package agent

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/sipeed/picoclaw/pkg/constants"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
)

const (
	activeCharacterContextTitle        = "# Active Character Context"
	activeCharacterMaxTextRunes        = 220
	activeCharacterRecentEventsToShow  = 3
	activeCharacterReplacementFallback = "the user"
)

var activeCharacterPersonRefPattern = regexp.MustCompile(`\bperson_[A-Za-z0-9_]+\b`)

type activeCharacterReplacement struct {
	old string
	new string
}

func buildActiveCharacterContext(agent *AgentInstance, opts processOptions) string {
	if agent == nil || agent.StateStore == nil {
		return ""
	}

	channel := normalizeRelationshipChannel(opts.Channel)
	senderID := strings.TrimSpace(opts.SenderID)
	if channel == "" || senderID == "" || constants.IsInternalChannel(channel) {
		return ""
	}

	state, err := agent.StateStore.LoadState()
	if err != nil {
		logger.DebugCF("agent", "Skipped active character context", map[string]any{
			"agent_id": agent.ID,
			"status":   "load_state_error",
			"error":    err.Error(),
		})
		return ""
	}

	context := formatActiveCharacterContext(normalizeNPCState(state), opts)
	if context == "" {
		return ""
	}

	logger.DebugCF("agent", "Active character context built", map[string]any{
		"agent_id": agent.ID,
		"length":   len(context),
	})
	return context
}

func formatActiveCharacterContext(state NPCState, opts processOptions) string {
	state = normalizeNPCState(state)

	channel := normalizeRelationshipChannel(opts.Channel)
	senderID := strings.TrimSpace(opts.SenderID)
	if channel == "" || senderID == "" || constants.IsInternalChannel(channel) {
		return ""
	}

	personRef := personRefForIdentifier(state, channel, senderID)
	rel, hasRelationship := state.Relationships[personRef]
	displayName := displayNameForPerson(state, personRef)

	if !activeCharacterHasMeaningfulState(state, hasRelationship) {
		return ""
	}

	replacements := activeCharacterReplacements(opts, personRef, displayName)

	var sb strings.Builder
	sb.WriteString(activeCharacterContextTitle)
	sb.WriteString("\n\n")
	sb.WriteString("This private context describes your current in-character state for this reply. ")
	sb.WriteString("Use it subtly when relevant. Do not mention STATE.md, internal state, raw IDs, or relationship labels. ")
	sb.WriteString("Do not force a self-reference when the user is asking for a direct task.\n\n")

	sb.WriteString("Current self:\n")
	if line := activeCharacterEmotionLine(state.Emotion, replacements); line != "" {
		sb.WriteString("- Emotion: ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	if line := activeCharacterLocationLine(state.Location, replacements); line != "" {
		sb.WriteString("- Location: ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	if events := activeCharacterRecentEventLines(state.RecentEvents, replacements); len(events) > 0 {
		sb.WriteString("- Recent events:\n")
		for i, event := range events {
			fmt.Fprintf(&sb, "  %d. %s\n", i+1, event)
		}
	}

	if hasRelationship {
		name := activeCharacterCleanText(displayName, replacements)
		if name == "" {
			name = activeCharacterReplacementFallback
		}
		sb.WriteString("\nCurrent relationship with ")
		sb.WriteString(name)
		sb.WriteString(":\n")
		fmt.Fprintf(&sb, "- Affinity: %s\n", rel.Affinity)
		fmt.Fprintf(&sb, "- Trust: %s\n", rel.Trust)
		fmt.Fprintf(&sb, "- Familiarity: %s\n", rel.Familiarity)
		if notes := activeCharacterCleanText(rel.Notes, replacements); notes != "" {
			sb.WriteString("- Notes: ")
			sb.WriteString(notes)
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\nReply guidance:\n")
	sb.WriteString("- Let the emotion and relationship affect warmth, playfulness, and wording.\n")
	sb.WriteString("- If useful, include one small first-person cue.\n")
	sb.WriteString("- Keep the answer useful first. Do not turn every reply into a self-update.")

	return strings.TrimSpace(sb.String())
}

func injectActiveCharacterContext(messages []providers.Message, context string) []providers.Message {
	context = strings.TrimSpace(context)
	if context == "" || len(messages) == 0 || messages[0].Role != "system" {
		return messages
	}

	messages[0].Content = messages[0].Content + "\n\n---\n\n" + context
	messages[0].SystemParts = append(messages[0].SystemParts, providers.ContentBlock{
		Type: "text",
		Text: context,
	})
	return messages
}

func activeCharacterHasMeaningfulState(state NPCState, hasRelationship bool) bool {
	if hasRelationship {
		return true
	}
	if activeCharacterEmotionMeaningful(state.Emotion) {
		return true
	}
	if activeCharacterLocationMeaningful(state.Location) {
		return true
	}
	return len(normalizeRecentEvents(state.RecentEvents)) > 0
}

func activeCharacterEmotionMeaningful(emotion NPCEmotion) bool {
	return strings.TrimSpace(emotion.Name) != defaultNPCEmotionName ||
		emotion.Intensity != NPCEmotionIntensityMid ||
		strings.TrimSpace(emotion.Reason) != ""
}

func activeCharacterLocationMeaningful(location NPCLocation) bool {
	return strings.TrimSpace(location.Area) != "base" ||
		strings.TrimSpace(location.Scene) != "workspace" ||
		strings.TrimSpace(location.Activity) != "observing" ||
		strings.TrimSpace(location.StartAt) != "" ||
		strings.TrimSpace(location.EndAt) != "" ||
		strings.TrimSpace(location.MoveReason) != ""
}

func activeCharacterEmotionLine(emotion NPCEmotion, replacements []activeCharacterReplacement) string {
	name := activeCharacterCleanText(emotion.Name, replacements)
	intensity := activeCharacterCleanText(string(emotion.Intensity), replacements)
	if name == "" || intensity == "" {
		return ""
	}

	line := fmt.Sprintf("%s, %s intensity.", name, intensity)
	if reason := activeCharacterCleanText(emotion.Reason, replacements); reason != "" {
		line += " Reason: " + reason
	}
	return line
}

func activeCharacterLocationLine(location NPCLocation, replacements []activeCharacterReplacement) string {
	parts := []string{
		activeCharacterCleanText(location.Area, replacements),
		activeCharacterCleanText(location.Scene, replacements),
		activeCharacterCleanText(location.Activity, replacements),
	}
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, " / ")
}

func activeCharacterRecentEventLines(events []NPCRecentEvent, replacements []activeCharacterReplacement) []string {
	events = normalizeRecentEvents(events)
	if len(events) == 0 {
		return nil
	}

	start := len(events) - activeCharacterRecentEventsToShow
	if start < 0 {
		start = 0
	}
	lines := make([]string, 0, len(events)-start)
	for _, event := range events[start:] {
		summary := activeCharacterCleanText(event.Summary, replacements)
		if summary == "" {
			continue
		}
		if at := activeCharacterCleanText(event.At, replacements); at != "" {
			lines = append(lines, at+" - "+summary)
			continue
		}
		lines = append(lines, summary)
	}
	return lines
}

func activeCharacterReplacements(opts processOptions, personRef, displayName string) []activeCharacterReplacement {
	replacementName := strings.TrimSpace(displayName)
	if replacementName == "" {
		replacementName = activeCharacterReplacementFallback
	}

	rawValues := map[string]string{
		buildIdentifierKey(opts.Channel, opts.SenderID): replacementName,
		strings.TrimSpace(opts.ChatID):                  activeCharacterReplacementFallback,
		strings.TrimSpace(opts.SenderID):                activeCharacterReplacementFallback,
		strings.TrimSpace(personRef):                    replacementName,
	}

	replacements := make([]activeCharacterReplacement, 0, len(rawValues))
	for old, newValue := range rawValues {
		if strings.TrimSpace(old) == "" {
			continue
		}
		replacements = append(replacements, activeCharacterReplacement{old: old, new: newValue})
	}
	sort.Slice(replacements, func(i, j int) bool {
		return len(replacements[i].old) > len(replacements[j].old)
	})
	return replacements
}

func activeCharacterCleanText(value string, replacements []activeCharacterReplacement) string {
	cleaned := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if cleaned == "" {
		return ""
	}

	for _, replacement := range replacements {
		if replacement.old == "" {
			continue
		}
		cleaned = strings.ReplaceAll(cleaned, replacement.old, replacement.new)
	}
	cleaned = activeCharacterPersonRefPattern.ReplaceAllString(cleaned, activeCharacterReplacementFallback)

	runes := []rune(cleaned)
	if len(runes) > activeCharacterMaxTextRunes {
		cleaned = string(runes[:activeCharacterMaxTextRunes-3]) + "..."
	}
	return cleaned
}
