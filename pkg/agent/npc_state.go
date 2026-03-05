package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/fileutil"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
)

const (
	npcStateVersion      = 1
	npcStateFileName     = "STATE.md"
	npcMemoryBeginMarker = "<!-- NPC_MEMORY_BEGIN -->"
	npcMemoryEndMarker   = "<!-- NPC_MEMORY_END -->"

	maxNPCRecentEvents  = 30
	maxNPCMemoryNotes   = 50
	npcUpdaterTimeout   = 25 * time.Second
	npcUpdaterPromptTag = "NPC_STATE_MEMORY_UPDATER_V1"

	defaultNPCEmotionName = "calm"
)

type NPCEmotionIntensity string

const (
	NPCEmotionIntensityLow  NPCEmotionIntensity = "low"
	NPCEmotionIntensityMid  NPCEmotionIntensity = "mid"
	NPCEmotionIntensityHigh NPCEmotionIntensity = "high"
)

var npcAllowedEmotionNames = []string{
	"calm",
	"cheerful",
	"excited",
	"playful",
	"focused",
	"curious",
	"concerned",
	"frustrated",
	"naughty",
	"angry",
	"withdrawn",
}

var npcAllowedEmotionNameSet = map[string]struct{}{
	"calm":       {},
	"cheerful":   {},
	"excited":    {},
	"playful":    {},
	"focused":    {},
	"curious":    {},
	"concerned":  {},
	"frustrated": {},
	"naughty":    {},
	"angry":      {},
	"withdrawn":  {},
}

func (i *NPCEmotionIntensity) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*i = ""
		return nil
	}

	if strings.HasPrefix(trimmed, `"`) {
		var level string
		if err := json.Unmarshal(data, &level); err != nil {
			return err
		}
		*i = normalizeEmotionIntensity(NPCEmotionIntensity(level))
		return nil
	}

	var numeric float64
	if err := json.Unmarshal(data, &numeric); err == nil {
		*i = intensityFromNumeric(numeric)
		return nil
	}

	return fmt.Errorf("invalid emotion intensity: %s", trimmed)
}

type NPCEmotion struct {
	Name      string              `json:"name,omitempty"`
	Intensity NPCEmotionIntensity `json:"intensity,omitempty"`
	Reason    string              `json:"reason,omitempty"`
}

type NPCLocation struct {
	Area       string `json:"area,omitempty"`
	Scene      string `json:"scene,omitempty"`
	Activity   string `json:"activity,omitempty"`
	MovedAt    string `json:"moved_at,omitempty"`
	MoveReason string `json:"move_reason,omitempty"`
}

type NPCRelationship struct {
	Affinity          int    `json:"affinity,omitempty"`
	Trust             int    `json:"trust,omitempty"`
	Familiarity       int    `json:"familiarity,omitempty"`
	LastInteractionAt string `json:"last_interaction_at,omitempty"`
	Notes             string `json:"notes,omitempty"`
}

type NPCVitals struct {
	Energy     int `json:"energy,omitempty"`
	Stress     int `json:"stress,omitempty"`
	Motivation int `json:"motivation,omitempty"`
}

type NPCRecentEvent struct {
	At      string `json:"at,omitempty"`
	Type    string `json:"type,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type NPCState struct {
	Version       int                        `json:"version,omitempty"`
	UpdatedAt     string                     `json:"updated_at,omitempty"`
	Emotion       NPCEmotion                 `json:"emotion,omitempty"`
	Location      NPCLocation                `json:"location,omitempty"`
	Relationships map[string]NPCRelationship `json:"relationships,omitempty"`
	Vitals        NPCVitals                  `json:"vitals,omitempty"`
	Habits        []string                   `json:"habits,omitempty"`
	RecentEvents  []NPCRecentEvent           `json:"recent_events,omitempty"`
}

type npcStateUpdateResult struct {
	State       NPCState `json:"state"`
	MemoryNotes []string `json:"memory_notes"`
}

// NPCStateStore persists per-agent roleplay state and a managed memory block.
type NPCStateStore struct {
	workspace  string
	statePath  string
	memoryPath string
	mu         sync.Mutex
}

func NewNPCStateStore(workspace string) *NPCStateStore {
	return &NPCStateStore{
		workspace:  workspace,
		statePath:  filepath.Join(workspace, npcStateFileName),
		memoryPath: filepath.Join(workspace, "memory", "MEMORY.md"),
	}
}

func (s *NPCStateStore) StatePath() string {
	return s.statePath
}

func (s *NPCStateStore) MemoryPath() string {
	return s.memoryPath
}

func (s *NPCStateStore) LoadState() (NPCState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadStateLocked()
}

func (s *NPCStateStore) loadStateLocked() (NPCState, error) {
	content, err := os.ReadFile(s.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultNPCState(), nil
		}
		return NPCState{}, err
	}

	rawJSON := extractJSONObjectFromContent(string(content))
	if strings.TrimSpace(rawJSON) == "" {
		return defaultNPCState(), nil
	}

	var state NPCState
	if err := json.Unmarshal([]byte(rawJSON), &state); err != nil {
		return defaultNPCState(), nil
	}

	return normalizeNPCState(state), nil
}

func (s *NPCStateStore) SaveState(state NPCState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state = normalizeNPCState(state)
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	md := "# NPC State\n\n```json\n" + string(data) + "\n```\n"
	return fileutil.WriteFileAtomic(s.statePath, []byte(md), 0o644)
}

func (s *NPCStateStore) LoadMemoryNotes() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := os.ReadFile(s.memoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	return extractManagedMemoryNotes(string(content)), nil
}

func (s *NPCStateStore) SaveMemoryNotes(notes []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := normalizeMemoryNotes(notes)

	var existing string
	if content, err := os.ReadFile(s.memoryPath); err == nil {
		existing = string(content)
	} else if !os.IsNotExist(err) {
		return err
	}

	updated := upsertManagedMemoryBlock(existing, normalized)
	return fileutil.WriteFileAtomic(s.memoryPath, []byte(updated), 0o600)
}

func defaultNPCState() NPCState {
	return normalizeNPCState(NPCState{
		Version:   npcStateVersion,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Emotion: NPCEmotion{
			Name:      defaultNPCEmotionName,
			Intensity: NPCEmotionIntensityMid,
		},
		Location: NPCLocation{
			Area:     "base",
			Scene:    "workspace",
			Activity: "observing",
		},
		Relationships: map[string]NPCRelationship{},
		Vitals: NPCVitals{
			Energy:     70,
			Stress:     20,
			Motivation: 70,
		},
	})
}

func normalizeNPCState(state NPCState) NPCState {
	if state.Version == 0 {
		state.Version = npcStateVersion
	}
	if strings.TrimSpace(state.UpdatedAt) == "" {
		state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	state.Emotion.Name = normalizeEmotionName(state.Emotion.Name)
	state.Emotion.Intensity = normalizeEmotionIntensity(state.Emotion.Intensity)
	state.Emotion.Reason = strings.TrimSpace(state.Emotion.Reason)

	state.Location.Area = strings.TrimSpace(state.Location.Area)
	if state.Location.Area == "" {
		state.Location.Area = "base"
	}
	state.Location.Scene = strings.TrimSpace(state.Location.Scene)
	if state.Location.Scene == "" {
		state.Location.Scene = "workspace"
	}
	state.Location.Activity = strings.TrimSpace(state.Location.Activity)
	if state.Location.Activity == "" {
		state.Location.Activity = "observing"
	}
	state.Location.MoveReason = strings.TrimSpace(state.Location.MoveReason)

	state.Vitals.Energy = clamp(state.Vitals.Energy, 0, 100)
	state.Vitals.Stress = clamp(state.Vitals.Stress, 0, 100)
	state.Vitals.Motivation = clamp(state.Vitals.Motivation, 0, 100)

	if state.Relationships == nil {
		state.Relationships = make(map[string]NPCRelationship)
	}
	normalizedRelationships := make(map[string]NPCRelationship, len(state.Relationships))
	for key, rel := range state.Relationships {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		rel.Affinity = clamp(rel.Affinity, 0, 100)
		rel.Trust = clamp(rel.Trust, 0, 100)
		rel.Familiarity = clamp(rel.Familiarity, 0, 100)
		rel.LastInteractionAt = strings.TrimSpace(rel.LastInteractionAt)
		rel.Notes = strings.TrimSpace(rel.Notes)
		normalizedRelationships[normalizedKey] = rel
	}
	state.Relationships = normalizedRelationships

	state.Habits = normalizeHabits(state.Habits)
	state.RecentEvents = normalizeRecentEvents(state.RecentEvents)

	return state
}

func normalizeHabits(habits []string) []string {
	if len(habits) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(habits))
	result := make([]string, 0, len(habits))
	for _, h := range habits {
		normalized := strings.TrimSpace(h)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, normalized)
		if len(result) >= 24 {
			break
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeRecentEvents(events []NPCRecentEvent) []NPCRecentEvent {
	if len(events) == 0 {
		return nil
	}
	trimmed := make([]NPCRecentEvent, 0, len(events))
	for _, e := range events {
		e.Summary = strings.TrimSpace(e.Summary)
		if e.Summary == "" {
			continue
		}
		e.Type = strings.TrimSpace(e.Type)
		if e.Type == "" {
			e.Type = "chat"
		}
		e.At = strings.TrimSpace(e.At)
		if e.At == "" {
			e.At = time.Now().UTC().Format(time.RFC3339)
		}
		trimmed = append(trimmed, e)
	}
	if len(trimmed) > maxNPCRecentEvents {
		trimmed = trimmed[len(trimmed)-maxNPCRecentEvents:]
	}
	if len(trimmed) == 0 {
		return nil
	}
	return trimmed
}

func normalizeMemoryNotes(notes []string) []string {
	if len(notes) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(notes))
	result := make([]string, 0, len(notes))
	for _, note := range notes {
		normalized := normalizeMemoryNote(note)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, normalized)
		if len(result) >= maxNPCMemoryNotes {
			break
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeMemoryNote(note string) string {
	n := strings.TrimSpace(note)
	n = strings.TrimPrefix(n, "- ")
	n = strings.TrimPrefix(n, "*")
	n = strings.TrimSpace(n)
	return n
}

func normalizeEmotionName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.Join(strings.Fields(normalized), " ")
	if _, ok := npcAllowedEmotionNameSet[normalized]; ok {
		return normalized
	}
	return defaultNPCEmotionName
}

func normalizeEmotionIntensity(level NPCEmotionIntensity) NPCEmotionIntensity {
	normalized := strings.ToLower(strings.TrimSpace(string(level)))
	switch normalized {
	case string(NPCEmotionIntensityLow):
		return NPCEmotionIntensityLow
	case "medium", "middle", string(NPCEmotionIntensityMid):
		return NPCEmotionIntensityMid
	case string(NPCEmotionIntensityHigh):
		return NPCEmotionIntensityHigh
	default:
		return NPCEmotionIntensityMid
	}
}

func intensityFromNumeric(value float64) NPCEmotionIntensity {
	switch {
	case value <= 33:
		return NPCEmotionIntensityLow
	case value <= 66:
		return NPCEmotionIntensityMid
	default:
		return NPCEmotionIntensityHigh
	}
}

func buildRelationshipKey(channel, senderID string) string {
	ch := strings.ToLower(strings.TrimSpace(channel))
	sender := strings.ToLower(strings.TrimSpace(senderID))
	if ch == "" || sender == "" {
		return ""
	}
	return ch + ":" + sender
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func extractJSONObjectFromContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}

	lower := strings.ToLower(trimmed)
	if idx := strings.Index(lower, "```json"); idx >= 0 {
		rest := trimmed[idx+len("```json"):]
		rest = strings.TrimLeft(rest, " \t\r\n")
		if end := strings.Index(rest, "```"); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
		return strings.TrimSpace(rest)
	}

	if strings.HasPrefix(trimmed, "```") {
		rest := trimmed[3:]
		if nl := strings.Index(rest, "\n"); nl >= 0 {
			rest = rest[nl+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			candidate := strings.TrimSpace(rest[:end])
			if strings.HasPrefix(candidate, "{") {
				return candidate
			}
		}
	}

	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		return trimmed
	}

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return strings.TrimSpace(trimmed[start : end+1])
	}

	return ""
}

func extractManagedMemoryNotes(content string) []string {
	start := strings.Index(content, npcMemoryBeginMarker)
	end := strings.Index(content, npcMemoryEndMarker)
	if start < 0 || end <= start {
		return nil
	}

	segment := content[start+len(npcMemoryBeginMarker) : end]
	lines := strings.Split(segment, "\n")
	notes := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		note := normalizeMemoryNote(line)
		if note == "" || note == "(none yet)" {
			continue
		}
		notes = append(notes, note)
	}
	return normalizeMemoryNotes(notes)
}

func renderManagedMemoryBlock(notes []string) string {
	notes = normalizeMemoryNotes(notes)

	var sb strings.Builder
	sb.WriteString(npcMemoryBeginMarker)
	sb.WriteString("\n## NPC Memory Notes\n")
	sb.WriteString("<!-- auto-generated by npc state updater -->\n\n")
	if len(notes) == 0 {
		sb.WriteString("- (none yet)\n")
	} else {
		for _, note := range notes {
			sb.WriteString("- ")
			sb.WriteString(note)
			sb.WriteString("\n")
		}
	}
	sb.WriteString(npcMemoryEndMarker)
	sb.WriteString("\n")
	return sb.String()
}

func upsertManagedMemoryBlock(existing string, notes []string) string {
	block := renderManagedMemoryBlock(notes)
	start := strings.Index(existing, npcMemoryBeginMarker)
	end := strings.Index(existing, npcMemoryEndMarker)
	if start >= 0 && end > start {
		replaceEnd := end + len(npcMemoryEndMarker)
		if replaceEnd < len(existing) && existing[replaceEnd] == '\r' {
			replaceEnd++
		}
		if replaceEnd < len(existing) && existing[replaceEnd] == '\n' {
			replaceEnd++
		}
		return existing[:start] + block + existing[replaceEnd:]
	}

	existing = strings.TrimRight(existing, "\r\n")
	if existing == "" {
		return block
	}
	return existing + "\n\n" + block
}

func (al *AgentLoop) maybeUpdateNPCStateAndMemory(
	ctx context.Context,
	agent *AgentInstance,
	msg bus.InboundMessage,
	routeMatchedBy string,
	assistantReply string,
) {
	if al == nil || al.cfg == nil || agent == nil || agent.StateStore == nil {
		return
	}

	autoCfg := al.cfg.Agents.AutoProvision
	if !autoCfg.Enabled || !autoCfg.StrictOneToOne || routeMatchedBy != "auto-provision" {
		return
	}

	if err := al.updateNPCStateAndMemory(ctx, agent, msg, assistantReply); err != nil {
		logger.WarnCF("agent", "Failed to update NPC state/memory",
			map[string]any{
				"agent_id": agent.ID,
				"channel":  msg.Channel,
				"sender":   msg.SenderID,
				"error":    err.Error(),
			})
	}
}

func (al *AgentLoop) updateNPCStateAndMemory(
	parentCtx context.Context,
	agent *AgentInstance,
	msg bus.InboundMessage,
	assistantReply string,
) error {
	baseCtx := parentCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	ctx, cancel := context.WithTimeout(baseCtx, npcUpdaterTimeout)
	defer cancel()

	state, err := agent.StateStore.LoadState()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	memoryNotes, err := agent.StateStore.LoadMemoryNotes()
	if err != nil {
		return fmt.Errorf("load memory notes: %w", err)
	}

	fallbackState := state
	applyMinimalTurnUpdate(&fallbackState, msg, assistantReply)
	fallbackNotes := normalizeMemoryNotes(memoryNotes)

	update, err := al.requestNPCStateUpdate(ctx, agent, fallbackState, fallbackNotes, msg, assistantReply)
	if err != nil {
		if saveErr := agent.StateStore.SaveState(fallbackState); saveErr != nil {
			return fmt.Errorf("save fallback state after update failure (%v): %w", err, saveErr)
		}
		if saveErr := agent.StateStore.SaveMemoryNotes(fallbackNotes); saveErr != nil {
			return fmt.Errorf("save fallback memory after update failure (%v): %w", err, saveErr)
		}
		return nil
	}

	nextState := normalizeNPCState(update.State)
	ensureRelationshipPresent(&nextState, msg)
	if err := agent.StateStore.SaveState(nextState); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	notesToSave := update.MemoryNotes
	if len(notesToSave) == 0 {
		notesToSave = fallbackNotes
	}
	if err := agent.StateStore.SaveMemoryNotes(notesToSave); err != nil {
		return fmt.Errorf("save memory notes: %w", err)
	}

	return nil
}

func (al *AgentLoop) requestNPCStateUpdate(
	ctx context.Context,
	agent *AgentInstance,
	state NPCState,
	memoryNotes []string,
	msg bus.InboundMessage,
	assistantReply string,
) (*npcStateUpdateResult, error) {
	if agent.Provider == nil {
		return nil, fmt.Errorf("agent provider is nil")
	}

	relationshipKey := buildRelationshipKey(msg.Channel, msg.SenderID)
	payload := map[string]any{
		"previous_state":        state,
		"existing_memory_notes": memoryNotes,
		"interaction": map[string]any{
			"timestamp":        time.Now().UTC().Format(time.RFC3339),
			"channel":          msg.Channel,
			"chat_id":          msg.ChatID,
			"sender_id":        msg.SenderID,
			"peer_kind":        msg.Peer.Kind,
			"peer_id":          msg.Peer.ID,
			"relationship_key": relationshipKey,
			"user_message":     msg.Content,
			"assistant_reply":  assistantReply,
		},
	}

	inputJSON, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}

	systemPrompt := fmt.Sprintf(`%s
You update internal roleplay state and long-term memory notes for one dedicated NPC agent.
Return JSON only, no markdown, no explanations.

Output shape:
{
  "state": {
    "version": 1,
    "updated_at": "RFC3339 timestamp",
    "emotion": {"name": "string", "intensity": "low|mid|high", "reason": "string"},
    "location": {"area": "string", "scene": "string", "activity": "string", "moved_at": "RFC3339 timestamp", "move_reason": "string"},
    "relationships": {
      "<channel:user_id>": {"affinity": 0-100, "trust": 0-100, "familiarity": 0-100, "last_interaction_at": "RFC3339 timestamp", "notes": "string"}
    },
    "vitals": {"energy": 0-100, "stress": 0-100, "motivation": 0-100},
    "habits": ["string"],
    "recent_events": [{"at": "RFC3339", "type": "string", "summary": "string"}]
  },
  "memory_notes": ["string"]
}

Rules:
- Keep continuity from previous state unless interaction indicates change.
- emotion.name must be one of: %s.
- emotion.intensity must be one of: low, mid, high.
- Intensity behavior guide: low=subtle cues and mostly neutral language; mid=clear but balanced emotional expression; high=strong and direct expression matching context.
- Ensure relationship key %q exists and is updated.
- Keep memory_notes concise, deduplicated, and <= %d.
- Merge/edit existing notes when possible instead of blind append.
- Return valid JSON object only.`, npcUpdaterPromptTag, strings.Join(npcAllowedEmotionNames, ", "), relationshipKey, maxNPCMemoryNotes)

	response, err := agent.Provider.Chat(
		ctx,
		[]providers.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(inputJSON)},
		},
		nil,
		agent.Model,
		map[string]any{
			"max_tokens":       1400,
			"temperature":      0.2,
			"prompt_cache_key": agent.ID + ":npc-state",
		},
	)
	if err != nil {
		return nil, err
	}

	raw := extractJSONObjectFromContent(response.Content)
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("empty JSON update response")
	}

	var update npcStateUpdateResult
	if err := json.Unmarshal([]byte(raw), &update); err != nil {
		var stateOnly NPCState
		if err2 := json.Unmarshal([]byte(raw), &stateOnly); err2 != nil {
			return nil, err
		}
		update.State = stateOnly
	}

	update.State = normalizeNPCState(update.State)
	update.MemoryNotes = normalizeMemoryNotes(update.MemoryNotes)

	return &update, nil
}

func ensureRelationshipPresent(state *NPCState, msg bus.InboundMessage) {
	if state == nil {
		return
	}
	if state.Relationships == nil {
		state.Relationships = make(map[string]NPCRelationship)
	}
	relationshipKey := buildRelationshipKey(msg.Channel, msg.SenderID)
	if relationshipKey == "" {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	rel, ok := state.Relationships[relationshipKey]
	if !ok {
		rel = NPCRelationship{
			Affinity:    50,
			Trust:       50,
			Familiarity: 1,
		}
	} else {
		rel.Familiarity = clamp(rel.Familiarity+1, 0, 100)
	}
	rel.LastInteractionAt = now
	state.Relationships[relationshipKey] = rel
}

func applyMinimalTurnUpdate(state *NPCState, msg bus.InboundMessage, assistantReply string) {
	if state == nil {
		return
	}

	ensureRelationshipPresent(state, msg)

	summary := strings.TrimSpace(msg.Content)
	if summary == "" {
		summary = strings.TrimSpace(assistantReply)
	}
	if summary == "" {
		summary = "interaction"
	}
	if len(summary) > 180 {
		summary = summary[:180]
	}

	event := NPCRecentEvent{
		At:      time.Now().UTC().Format(time.RFC3339),
		Type:    "chat",
		Summary: summary,
	}
	state.RecentEvents = append(state.RecentEvents, event)
	state.RecentEvents = normalizeRecentEvents(state.RecentEvents)
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
}
