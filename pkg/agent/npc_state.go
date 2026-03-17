package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/fileutil"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
)

const (
	npcStateVersion      = 1
	npcStateFileName     = "STATE.md"
	npcMemoryBeginMarker = "<!-- NPC_MEMORY_BEGIN -->"
	npcMemoryEndMarker   = "<!-- NPC_MEMORY_END -->"

	maxNPCRecentEvents       = 30
	maxNPCMemoryNotes        = 50
	npcUpdaterEveryTurns     = 5
	npcUpdaterTimeout        = 5 * time.Minute
	npcStateUpdaterPromptTag = "NPC_STATE_UPDATER_V2"
	npcUpdaterPromptTag      = "NPC_STATE_MEMORY_UPDATER_V1"

	npcLocationTimeLayout              = "2006-01-02 15:04"
	npcHeartbeatIdleThreshold          = 2 * time.Hour
	npcHeartbeatMoveProbability        = 0.08
	npcHeartbeatMinDurationMinutes     = 35
	npcHeartbeatDurationRangeMinutes   = 40
	npcActivityRemoteChatSuffix        = " (multitasking, chatting remotely)"
	npcHeartbeatMoveReason             = "idle heartbeat walk"
	npcHeartbeatReturnMoveReasonPrefix = "finished "

	defaultNPCEmotionName = "calm"
)

type npcHeartbeatOuting struct {
	Area     string
	Scene    string
	Activity string
}

var npcHeartbeatOutings = []npcHeartbeatOuting{
	{Area: "park", Scene: "tree-lined trail", Activity: "out for a walk"},
	{Area: "harbor", Scene: "boardwalk", Activity: "taking a slow walk"},
	{Area: "town", Scene: "side streets", Activity: "exploring nearby"},
	{Area: "market", Scene: "quiet alley", Activity: "stretching legs"},
}

type NPCEmotionIntensity string

type NPCLevel string

const (
	NPCEmotionIntensityLow  NPCEmotionIntensity = "low"
	NPCEmotionIntensityMid  NPCEmotionIntensity = "mid"
	NPCEmotionIntensityHigh NPCEmotionIntensity = "high"

	NPCLevelLow  NPCLevel = "low"
	NPCLevelMid  NPCLevel = "mid"
	NPCLevelHigh NPCLevel = "high"
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
	StartAt    string `json:"start_at,omitempty"`
	EndAt      string `json:"end_at,omitempty"`
	MoveReason string `json:"move_reason,omitempty"`
}

func (l *NPCLocation) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*l = NPCLocation{}
		return nil
	}

	type rawNPCLocation struct {
		Area       string `json:"area,omitempty"`
		Scene      string `json:"scene,omitempty"`
		Activity   string `json:"activity,omitempty"`
		StartAt    string `json:"start_at,omitempty"`
		EndAt      string `json:"end_at,omitempty"`
		MovedAt    string `json:"moved_at,omitempty"`
		MoveReason string `json:"move_reason,omitempty"`
	}

	var raw rawNPCLocation
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	startAt := raw.StartAt
	if strings.TrimSpace(startAt) == "" {
		startAt = raw.MovedAt
	}

	*l = NPCLocation{
		Area:       raw.Area,
		Scene:      raw.Scene,
		Activity:   raw.Activity,
		StartAt:    startAt,
		EndAt:      raw.EndAt,
		MoveReason: raw.MoveReason,
	}

	return nil
}

type NPCRelationship struct {
	Affinity               NPCLevel `json:"affinity,omitempty"`
	Trust                  NPCLevel `json:"trust,omitempty"`
	Familiarity            NPCLevel `json:"familiarity,omitempty"`
	LastInteractionAt      string   `json:"last_interaction_at,omitempty"`
	LastChannel            string   `json:"last_channel,omitempty"`
	LastChatID             string   `json:"last_chat_id,omitempty"`
	LastPeerKind           string   `json:"last_peer_kind,omitempty"`
	LastSessionKey         string   `json:"last_session_key,omitempty"`
	LastUserMessageAt      string   `json:"last_user_message_at,omitempty"`
	LastAgentMessageAt     string   `json:"last_agent_message_at,omitempty"`
	LastProactiveAttemptAt string   `json:"last_proactive_attempt_at,omitempty"`
	LastProactiveSuccessAt string   `json:"last_proactive_success_at,omitempty"`
	Notes                  string   `json:"notes,omitempty"`
}

type NPCRecentEvent struct {
	At      string `json:"at,omitempty"`
	Type    string `json:"type,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type NPCState struct {
	Version       int                        `json:"version,omitempty"`
	UpdatedAt     string                     `json:"updated_at,omitempty"`
	TrackedTurns  int                        `json:"tracked_turns,omitempty"`
	Emotion       NPCEmotion                 `json:"emotion,omitempty"`
	Location      NPCLocation                `json:"location,omitempty"`
	Relationships map[string]NPCRelationship `json:"relationships,omitempty"`
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

	return s.saveStateLocked(state)
}

func (s *NPCStateStore) UpdateState(update func(state *NPCState) (bool, error)) error {
	if update == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.loadStateLocked()
	if err != nil {
		return err
	}
	changed, err := update(&state)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	return s.saveStateLocked(state)
}

func (s *NPCStateStore) saveStateLocked(state NPCState) error {
	start := time.Now()
	state = normalizeNPCState(state)
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		logger.WarnCF("agent", "State file update failed", map[string]any{
			"workspace":   s.workspace,
			"path":        s.statePath,
			"status":      "error",
			"error":       err.Error(),
			"duration_ms": time.Since(start).Milliseconds(),
		})
		return err
	}

	md := "# NPC State\n\n```json\n" + string(data) + "\n```\n"
	if err := fileutil.WriteFileAtomic(s.statePath, []byte(md), 0o644); err != nil {
		logger.WarnCF("agent", "State file update failed", map[string]any{
			"workspace":   s.workspace,
			"path":        s.statePath,
			"status":      "error",
			"error":       err.Error(),
			"duration_ms": time.Since(start).Milliseconds(),
		})
		return err
	}

	logger.InfoCF("agent", "State file updated", map[string]any{
		"workspace":          s.workspace,
		"path":               s.statePath,
		"status":             "updated",
		"emotion":            state.Emotion.Name,
		"emotion_intensity":  state.Emotion.Intensity,
		"location_area":      state.Location.Area,
		"relationship_count": len(state.Relationships),
		"recent_event_count": len(state.RecentEvents),
		"duration_ms":        time.Since(start).Milliseconds(),
	})

	return nil
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

	start := time.Now()
	normalized := normalizeMemoryNotes(notes)

	var existing string
	if content, err := os.ReadFile(s.memoryPath); err == nil {
		existing = string(content)
	} else if !os.IsNotExist(err) {
		logger.WarnCF("agent", "Memory notes update failed", map[string]any{
			"workspace":   s.workspace,
			"path":        s.memoryPath,
			"status":      "error",
			"error":       err.Error(),
			"duration_ms": time.Since(start).Milliseconds(),
		})
		return err
	}

	updated := upsertManagedMemoryBlock(existing, normalized)
	if err := fileutil.WriteFileAtomic(s.memoryPath, []byte(updated), 0o600); err != nil {
		logger.WarnCF("agent", "Memory notes update failed", map[string]any{
			"workspace":   s.workspace,
			"path":        s.memoryPath,
			"status":      "error",
			"error":       err.Error(),
			"duration_ms": time.Since(start).Milliseconds(),
		})
		return err
	}

	logger.InfoCF("agent", "Memory notes updated", map[string]any{
		"workspace":   s.workspace,
		"path":        s.memoryPath,
		"status":      "updated",
		"notes_count": len(normalized),
		"duration_ms": time.Since(start).Milliseconds(),
	})

	return nil
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
	state.Location.StartAt = strings.TrimSpace(state.Location.StartAt)
	state.Location.EndAt = strings.TrimSpace(state.Location.EndAt)
	state.Location.MoveReason = strings.TrimSpace(state.Location.MoveReason)

	if state.Relationships == nil {
		state.Relationships = make(map[string]NPCRelationship)
	}
	normalizedRelationships := make(map[string]NPCRelationship, len(state.Relationships))
	for key, rel := range state.Relationships {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		rel.Affinity = normalizeNPCLevel(rel.Affinity, NPCLevelMid)
		rel.Trust = normalizeNPCLevel(rel.Trust, NPCLevelMid)
		rel.Familiarity = normalizeNPCLevel(rel.Familiarity, NPCLevelLow)
		rel.LastInteractionAt = strings.TrimSpace(rel.LastInteractionAt)
		rel.LastChannel = normalizeRelationshipChannel(rel.LastChannel)
		rel.LastChatID = strings.TrimSpace(rel.LastChatID)
		rel.LastPeerKind = normalizeRelationshipPeerKind(rel.LastPeerKind)
		rel.LastSessionKey = strings.TrimSpace(rel.LastSessionKey)
		rel.LastUserMessageAt = strings.TrimSpace(rel.LastUserMessageAt)
		rel.LastAgentMessageAt = strings.TrimSpace(rel.LastAgentMessageAt)
		rel.LastProactiveAttemptAt = strings.TrimSpace(rel.LastProactiveAttemptAt)
		rel.LastProactiveSuccessAt = strings.TrimSpace(rel.LastProactiveSuccessAt)
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

func normalizeNPCLevel(level NPCLevel, fallback NPCLevel) NPCLevel {
	normalized := strings.ToLower(strings.TrimSpace(string(level)))
	switch normalized {
	case string(NPCLevelLow):
		return NPCLevelLow
	case "medium", "middle", string(NPCLevelMid):
		return NPCLevelMid
	case string(NPCLevelHigh):
		return NPCLevelHigh
	default:
		return fallback
	}
}

func promoteNPCLevel(level NPCLevel) NPCLevel {
	switch normalizeNPCLevel(level, NPCLevelLow) {
	case NPCLevelLow:
		return NPCLevelMid
	case NPCLevelMid:
		return NPCLevelHigh
	default:
		return NPCLevelHigh
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

func (al *AgentLoop) maybeUpdateNPCStateAfterReply(
	ctx context.Context,
	agent *AgentInstance,
	msg bus.InboundMessage,
	routeMatchedBy string,
	sessionKey string,
	assistantReply string,
) {
	if al == nil || al.cfg == nil || agent == nil || agent.StateStore == nil {
		return
	}
	if !shouldTrackRelationshipMessage(msg) {
		return
	}
	if strings.TrimSpace(assistantReply) == "" {
		return
	}

	unlock := lockNPCUpdater(agent)
	defer unlock()

	state, err := agent.StateStore.LoadState()
	if err != nil {
		logger.WarnCF("agent", "Failed to load state before post-reply update", map[string]any{
			"agent_id": agent.ID,
			"channel":  msg.Channel,
			"sender":   msg.SenderID,
			"error":    err.Error(),
		})
		return
	}

	previousState := normalizeNPCState(state)
	trackedTurns := previousState.TrackedTurns + 1
	if shouldUpdateNPCManagedMemory(al.cfg, routeMatchedBy, trackedTurns) {
		if err := al.updateNPCStateAndMemory(ctx, agent, previousState, trackedTurns, msg, sessionKey, assistantReply); err != nil {
			logger.WarnCF("agent", "Failed to update NPC state/memory after reply",
				map[string]any{
					"agent_id": agent.ID,
					"channel":  msg.Channel,
					"sender":   msg.SenderID,
					"error":    err.Error(),
				})
		}
		return
	}

	if err := al.updateNPCStateOnly(ctx, agent, previousState, trackedTurns, msg, sessionKey, assistantReply); err != nil {
		logger.WarnCF("agent", "Failed to update NPC state after reply",
			map[string]any{
				"agent_id": agent.ID,
				"channel":  msg.Channel,
				"sender":   msg.SenderID,
				"error":    err.Error(),
			})
	}
}

func shouldUpdateNPCManagedMemory(cfg *config.Config, routeMatchedBy string, trackedTurns int) bool {
	if cfg == nil || trackedTurns <= 0 {
		return false
	}
	autoCfg := cfg.Agents.AutoProvision
	if !autoCfg.Enabled || !autoCfg.StrictOneToOne || routeMatchedBy != "auto-provision" {
		return false
	}
	return trackedTurns%npcUpdaterEveryTurns == 0
}

func (al *AgentLoop) updateNPCStateOnly(
	parentCtx context.Context,
	agent *AgentInstance,
	previousState NPCState,
	trackedTurns int,
	msg bus.InboundMessage,
	sessionKey string,
	assistantReply string,
) error {
	ctx, cancel := npcUpdaterContext(parentCtx)
	defer cancel()

	nextState, err := al.requestNPCStateOnlyUpdate(ctx, agent, previousState, msg, sessionKey, assistantReply)
	if err != nil {
		return err
	}

	return saveNPCReplyState(agent, nextState, trackedTurns, msg, sessionKey, time.Now())
}

func (al *AgentLoop) updateNPCStateAndMemory(
	parentCtx context.Context,
	agent *AgentInstance,
	previousState NPCState,
	trackedTurns int,
	msg bus.InboundMessage,
	sessionKey string,
	assistantReply string,
) error {
	memoryNotes, err := agent.StateStore.LoadMemoryNotes()
	if err != nil {
		return fmt.Errorf("load memory notes: %w", err)
	}

	ctx, cancel := npcUpdaterContext(parentCtx)
	defer cancel()

	update, err := al.requestNPCStateUpdate(ctx, agent, previousState, memoryNotes, msg, sessionKey, assistantReply)
	if err != nil {
		return err
	}

	if err := saveNPCReplyState(agent, update.State, trackedTurns, msg, sessionKey, time.Now()); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	notesToSave := update.MemoryNotes
	if len(notesToSave) == 0 {
		notesToSave = normalizeMemoryNotes(memoryNotes)
	}
	if err := agent.StateStore.SaveMemoryNotes(notesToSave); err != nil {
		return fmt.Errorf("save memory notes: %w", err)
	}

	return nil
}

func npcUpdaterContext(parentCtx context.Context) (context.Context, context.CancelFunc) {
	baseCtx := parentCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	return context.WithTimeout(baseCtx, npcUpdaterTimeout)
}

func saveNPCReplyState(
	agent *AgentInstance,
	nextState NPCState,
	trackedTurns int,
	msg bus.InboundMessage,
	sessionKey string,
	replyAt time.Time,
) error {
	if agent == nil || agent.StateStore == nil {
		return nil
	}

	nextState = normalizeNPCState(nextState)
	return agent.StateStore.UpdateState(func(state *NPCState) (bool, error) {
		merged := mergeNPCState(*state, nextState)
		merged.TrackedTurns = max(state.TrackedTurns, trackedTurns)
		applyReplyRelationshipMetadata(&merged, msg, sessionKey, replyAt)
		preserveActiveOutingDuringChat(state.Location, &merged.Location, replyAt)
		*state = merged
		return true, nil
	})
}

var npcUpdaterLocks sync.Map

func lockNPCUpdater(agent *AgentInstance) func() {
	if agent == nil {
		return func() {}
	}

	v, _ := npcUpdaterLocks.LoadOrStore(agent.ID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (al *AgentLoop) maybeApplyHeartbeatLocationPolicy(agent *AgentInstance) {
	if agent == nil || agent.StateStore == nil {
		return
	}

	state, err := agent.StateStore.LoadState()
	if err != nil {
		logger.WarnCF("agent", "Failed to load state for heartbeat location policy",
			map[string]any{"agent_id": agent.ID, "error": err.Error()})
		return
	}

	roll := rand.Float64()
	outingIndex := 0
	if len(npcHeartbeatOutings) > 0 {
		outingIndex = rand.IntN(len(npcHeartbeatOutings))
	}
	durationMinutes := npcHeartbeatMinDurationMinutes + rand.IntN(npcHeartbeatDurationRangeMinutes+1)

	nextState, changed := applyHeartbeatLocationPolicy(state, time.Now(), roll, outingIndex, durationMinutes)
	if !changed {
		return
	}

	if err := agent.StateStore.SaveState(nextState); err != nil {
		logger.WarnCF("agent", "Failed to save heartbeat location policy update",
			map[string]any{"agent_id": agent.ID, "error": err.Error()})
	}
}

func buildNPCStateUpdatePayload(
	state NPCState,
	memoryNotes []string,
	msg bus.InboundMessage,
	sessionKey string,
	assistantReply string,
) ([]byte, string, error) {
	relationshipKey := buildRelationshipKey(msg.Channel, msg.SenderID)
	payload := map[string]any{
		"previous_state": state,
		"interaction": map[string]any{
			"timestamp":        time.Now().UTC().Format(time.RFC3339),
			"channel":          msg.Channel,
			"chat_id":          msg.ChatID,
			"sender_id":        msg.SenderID,
			"peer_kind":        msg.Peer.Kind,
			"peer_id":          msg.Peer.ID,
			"relationship_key": relationshipKey,
			"session_key":      strings.TrimSpace(sessionKey),
			"user_message":     msg.Content,
			"assistant_reply":  assistantReply,
		},
	}
	if memoryNotes != nil {
		payload["existing_memory_notes"] = memoryNotes
	}

	inputJSON, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, "", err
	}
	return inputJSON, relationshipKey, nil
}

func (al *AgentLoop) requestNPCStateOnlyUpdate(
	ctx context.Context,
	agent *AgentInstance,
	state NPCState,
	msg bus.InboundMessage,
	sessionKey string,
	assistantReply string,
) (NPCState, error) {
	if agent == nil || agent.Provider == nil {
		return NPCState{}, fmt.Errorf("agent provider is nil")
	}

	inputJSON, relationshipKey, err := buildNPCStateUpdatePayload(state, nil, msg, sessionKey, assistantReply)
	if err != nil {
		return NPCState{}, err
	}

	systemPrompt := fmt.Sprintf(`%s
You update internal roleplay state for one dedicated NPC agent.
Return JSON only, no markdown, no explanations.

Output shape:
{
  "version": 1,
  "updated_at": "RFC3339 timestamp",
  "emotion": {"name": "string", "intensity": "low|mid|high", "reason": "string"},
  "location": {"area": "string", "scene": "string", "activity": "string", "start_at": "local datetime text (e.g. 2026-03-05 22:00)", "end_at": "local datetime text (e.g. 2026-03-05 23:30)", "move_reason": "string"},
  "relationships": {
    "<channel:user_id>": {"affinity": "low|mid|high", "trust": "low|mid|high", "familiarity": "low|mid|high", "last_interaction_at": "RFC3339 timestamp", "last_channel": "string", "last_chat_id": "string", "last_peer_kind": "string", "last_session_key": "string", "last_user_message_at": "RFC3339 timestamp", "last_agent_message_at": "RFC3339 timestamp", "last_proactive_attempt_at": "RFC3339 timestamp", "last_proactive_success_at": "RFC3339 timestamp", "notes": "string"}
  },
  "habits": ["string"],
  "recent_events": [{"at": "RFC3339", "type": "string", "summary": "string"}]
}

Rules:
- Keep continuity from previous state unless interaction indicates change.
- emotion.name must be one of: %s.
- emotion.intensity must be one of: low, mid, high.
- Intensity behavior guide: low=subtle cues and mostly neutral language; mid=clear but balanced emotional expression; high=strong and direct expression matching context.
- Emotion transition rule: emotion.name may change only when previous_state.emotion.intensity is low.
- If previous_state.emotion.intensity is mid or high, keep emotion.name the same as previous_state.emotion.name.
- Example: previous_state angry/high cannot become calm in one update; lower intensity first.
- location tracks off-chat activity and whereabouts; use start_at/end_at as local datetime text when available.
- If previous_state.location indicates an active outing window (between start_at and end_at), keep the outing location/activity and add a multitasking cue for chatting remotely.
- Preserve relationship contact/session/timestamp fields unless the current interaction updates them.
- Ensure relationship key %q exists and is updated.
- Return a valid JSON object only.`, npcStateUpdaterPromptTag, strings.Join(npcAllowedEmotionNames, ", "), relationshipKey)

	response, err := agent.Provider.Chat(
		ctx,
		[]providers.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(inputJSON)},
		},
		nil,
		agent.Model,
		map[string]any{
			"max_tokens":       1200,
			"temperature":      0.2,
			"prompt_cache_key": agent.ID + ":npc-state-only",
		},
	)
	if err != nil {
		return NPCState{}, err
	}

	raw := extractJSONObjectFromContent(response.Content)
	if strings.TrimSpace(raw) == "" {
		return NPCState{}, fmt.Errorf("empty JSON state update response")
	}

	var nextState NPCState
	if err := json.Unmarshal([]byte(raw), &nextState); err != nil {
		var wrapped npcStateUpdateResult
		if err2 := json.Unmarshal([]byte(raw), &wrapped); err2 != nil {
			return NPCState{}, err
		}
		nextState = wrapped.State
	}

	return normalizeNPCState(nextState), nil
}

func (al *AgentLoop) requestNPCStateUpdate(
	ctx context.Context,
	agent *AgentInstance,
	state NPCState,
	memoryNotes []string,
	msg bus.InboundMessage,
	sessionKey string,
	assistantReply string,
) (*npcStateUpdateResult, error) {
	if agent.Provider == nil {
		return nil, fmt.Errorf("agent provider is nil")
	}

	inputJSON, relationshipKey, err := buildNPCStateUpdatePayload(state, memoryNotes, msg, sessionKey, assistantReply)
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
    "location": {"area": "string", "scene": "string", "activity": "string", "start_at": "local datetime text (e.g. 2026-03-05 22:00)", "end_at": "local datetime text (e.g. 2026-03-05 23:30)", "move_reason": "string"},
    "relationships": {
      "<channel:user_id>": {"affinity": "low|mid|high", "trust": "low|mid|high", "familiarity": "low|mid|high", "last_interaction_at": "RFC3339 timestamp", "last_channel": "string", "last_chat_id": "string", "last_peer_kind": "string", "last_session_key": "string", "last_user_message_at": "RFC3339 timestamp", "last_agent_message_at": "RFC3339 timestamp", "last_proactive_attempt_at": "RFC3339 timestamp", "last_proactive_success_at": "RFC3339 timestamp", "notes": "string"}
    },
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
- Emotion transition rule: emotion.name may change only when previous_state.emotion.intensity is low.
- If previous_state.emotion.intensity is mid or high, keep emotion.name the same as previous_state.emotion.name.
- Example: previous_state angry/high cannot become calm in one update; lower intensity first.
- location tracks off-chat activity and whereabouts; use start_at/end_at as local datetime text when available.
- If previous_state.location indicates an active outing window (between start_at and end_at), keep the outing location/activity and add a multitasking cue for chatting remotely.
- Preserve relationship contact/session/timestamp fields unless the current interaction updates them.
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
			"prompt_cache_key": agent.ID + ":npc-state-memory",
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
	if !shouldTrackRelationshipMessage(msg) {
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
			Affinity:    NPCLevelMid,
			Trust:       NPCLevelMid,
			Familiarity: NPCLevelLow,
		}
	} else {
		rel.Familiarity = promoteNPCLevel(rel.Familiarity)
	}
	rel.LastChannel = normalizeRelationshipChannel(msg.Channel)
	rel.LastChatID = strings.TrimSpace(msg.ChatID)
	rel.LastPeerKind = normalizeRelationshipPeerKind(msg.Peer.Kind)
	rel.LastUserMessageAt = now
	rel.LastInteractionAt = now
	state.Relationships[relationshipKey] = rel
}

func applyReplyRelationshipMetadata(
	state *NPCState,
	msg bus.InboundMessage,
	sessionKey string,
	replyAt time.Time,
) {
	if state == nil {
		return
	}
	if !shouldTrackRelationshipMessage(msg) {
		return
	}
	if state.Relationships == nil {
		state.Relationships = make(map[string]NPCRelationship)
	}
	relationshipKey := buildRelationshipKey(msg.Channel, msg.SenderID)
	if relationshipKey == "" {
		return
	}

	timestamp := replyAt.UTC().Format(time.RFC3339)
	rel, ok := state.Relationships[relationshipKey]
	if !ok {
		rel = NPCRelationship{
			Affinity:    NPCLevelMid,
			Trust:       NPCLevelMid,
			Familiarity: NPCLevelLow,
		}
	} else {
		rel.Familiarity = promoteNPCLevel(rel.Familiarity)
	}
	rel.LastChannel = normalizeRelationshipChannel(msg.Channel)
	rel.LastChatID = strings.TrimSpace(msg.ChatID)
	rel.LastPeerKind = normalizeRelationshipPeerKind(msg.Peer.Kind)
	rel.LastSessionKey = strings.TrimSpace(sessionKey)
	rel.LastUserMessageAt = timestamp
	rel.LastAgentMessageAt = timestamp
	rel.LastInteractionAt = timestamp
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

func mergeNPCState(latest NPCState, next NPCState) NPCState {
	latest = normalizeNPCState(latest)
	next = normalizeNPCState(next)

	merged := next
	merged.TrackedTurns = max(latest.TrackedTurns, next.TrackedTurns)
	merged.Relationships = make(map[string]NPCRelationship, len(latest.Relationships)+len(next.Relationships))

	for key, rel := range latest.Relationships {
		merged.Relationships[key] = rel
	}
	for key, rel := range next.Relationships {
		merged.Relationships[key] = mergeNPCRelationship(merged.Relationships[key], rel)
	}

	return normalizeNPCState(merged)
}

func mergeNPCRelationship(latest NPCRelationship, next NPCRelationship) NPCRelationship {
	merged := next
	merged.LastChannel = preferLatestNonEmptyString(latest.LastChannel, next.LastChannel)
	merged.LastChatID = preferLatestNonEmptyString(latest.LastChatID, next.LastChatID)
	merged.LastPeerKind = preferLatestNonEmptyString(latest.LastPeerKind, next.LastPeerKind)
	merged.LastSessionKey = preferLatestNonEmptyString(latest.LastSessionKey, next.LastSessionKey)
	merged.LastInteractionAt = laterRFC3339String(latest.LastInteractionAt, next.LastInteractionAt)
	merged.LastUserMessageAt = laterRFC3339String(latest.LastUserMessageAt, next.LastUserMessageAt)
	merged.LastAgentMessageAt = laterRFC3339String(latest.LastAgentMessageAt, next.LastAgentMessageAt)
	merged.LastProactiveAttemptAt = laterRFC3339String(latest.LastProactiveAttemptAt, next.LastProactiveAttemptAt)
	merged.LastProactiveSuccessAt = laterRFC3339String(latest.LastProactiveSuccessAt, next.LastProactiveSuccessAt)
	return merged
}

func preferLatestNonEmptyString(latest string, next string) string {
	latest = strings.TrimSpace(latest)
	if latest != "" {
		return latest
	}
	return strings.TrimSpace(next)
}

func laterRFC3339String(left string, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}

	leftAt, leftErr := time.Parse(time.RFC3339, left)
	rightAt, rightErr := time.Parse(time.RFC3339, right)
	switch {
	case leftErr == nil && rightErr == nil:
		if rightAt.After(leftAt) {
			return right
		}
		return left
	case leftErr == nil:
		return left
	case rightErr == nil:
		return right
	case right > left:
		return right
	default:
		return left
	}
}

func preserveActiveOutingDuringChat(previous NPCLocation, next *NPCLocation, now time.Time) {
	if next == nil || !isActiveOutingWindow(previous, now) {
		return
	}

	activity := strings.TrimSpace(previous.Activity)
	if activity == "" {
		activity = strings.TrimSpace(next.Activity)
	}
	if activity == "" {
		activity = "out and about"
	}

	next.Area = strings.TrimSpace(previous.Area)
	next.Scene = strings.TrimSpace(previous.Scene)
	next.Activity = ensureRemoteChatSuffix(activity)
	next.StartAt = strings.TrimSpace(previous.StartAt)
	next.EndAt = strings.TrimSpace(previous.EndAt)

	if strings.TrimSpace(next.MoveReason) == "" {
		if previousReason := strings.TrimSpace(previous.MoveReason); previousReason != "" {
			next.MoveReason = previousReason
		} else {
			next.MoveReason = "continuing outing while chatting"
		}
	}
}

func ensureRemoteChatSuffix(activity string) string {
	trimmed := strings.TrimSpace(activity)
	if trimmed == "" {
		trimmed = "out and about"
	}

	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "chatting remotely") || strings.Contains(lower, "multitasking") {
		return trimmed
	}

	return trimmed + npcActivityRemoteChatSuffix
}

func parseLocationLocalTime(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}

	parsed, err := time.ParseInLocation(npcLocationTimeLayout, trimmed, time.Local)
	if err != nil {
		return time.Time{}, false
	}

	return parsed, true
}

func isActiveOutingWindow(location NPCLocation, now time.Time) bool {
	nowLocal := now.In(time.Local)
	startAt, hasStart := parseLocationLocalTime(location.StartAt)
	endAt, hasEnd := parseLocationLocalTime(location.EndAt)

	if !hasStart && !hasEnd {
		return false
	}
	if hasStart && nowLocal.Before(startAt) {
		return false
	}
	if hasEnd && !nowLocal.Before(endAt) {
		return false
	}

	return true
}

func parseRFC3339(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}

	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, false
	}

	return parsed, true
}

func latestInteractionAt(state NPCState) (time.Time, bool) {
	var latest time.Time
	hasLatest := false

	for _, rel := range state.Relationships {
		parsed, ok := parseRFC3339(rel.LastInteractionAt)
		if !ok {
			continue
		}
		if !hasLatest || parsed.After(latest) {
			latest = parsed
			hasLatest = true
		}
	}

	for _, event := range state.RecentEvents {
		eventType := strings.ToLower(strings.TrimSpace(event.Type))
		if eventType != "" && eventType != "chat" {
			continue
		}
		parsed, ok := parseRFC3339(event.At)
		if !ok {
			continue
		}
		if !hasLatest || parsed.After(latest) {
			latest = parsed
			hasLatest = true
		}
	}

	if !hasLatest {
		if parsed, ok := parseRFC3339(state.UpdatedAt); ok {
			latest = parsed
			hasLatest = true
		}
	}

	return latest, hasLatest
}

func appendLocationEvent(state *NPCState, at time.Time, summary string) {
	if state == nil {
		return
	}
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		return
	}

	state.RecentEvents = append(state.RecentEvents, NPCRecentEvent{
		At:      at.UTC().Format(time.RFC3339),
		Type:    "location",
		Summary: trimmed,
	})
	state.RecentEvents = normalizeRecentEvents(state.RecentEvents)
}

func applyHeartbeatLocationPolicy(
	state NPCState,
	now time.Time,
	roll float64,
	outingIndex int,
	durationMinutes int,
) (NPCState, bool) {
	next := normalizeNPCState(state)
	nowLocal := now.In(time.Local)

	if endAt, hasEnd := parseLocationLocalTime(next.Location.EndAt); hasEnd && !nowLocal.Before(endAt) {
		previousActivity := strings.TrimSpace(next.Location.Activity)
		moveReason := "returned from outing"
		if previousActivity != "" && strings.ToLower(previousActivity) != "observing" {
			moveReason = npcHeartbeatReturnMoveReasonPrefix + previousActivity
		}

		next.Location = NPCLocation{
			Area:       "base",
			Scene:      "workspace",
			Activity:   "observing",
			MoveReason: moveReason,
		}
		appendLocationEvent(&next, now, moveReason)
		next.UpdatedAt = now.UTC().Format(time.RFC3339)
		return next, true
	}

	if isActiveOutingWindow(next.Location, nowLocal) {
		return next, false
	}

	lastInteraction, ok := latestInteractionAt(next)
	if !ok {
		return next, false
	}
	if now.UTC().Sub(lastInteraction) < npcHeartbeatIdleThreshold {
		return next, false
	}
	if roll >= npcHeartbeatMoveProbability {
		return next, false
	}
	if len(npcHeartbeatOutings) == 0 {
		return next, false
	}

	if outingIndex < 0 || outingIndex >= len(npcHeartbeatOutings) {
		outingIndex = 0
	}
	minDuration := npcHeartbeatMinDurationMinutes
	maxDuration := npcHeartbeatMinDurationMinutes + npcHeartbeatDurationRangeMinutes
	if durationMinutes < minDuration || durationMinutes > maxDuration {
		durationMinutes = minDuration
	}

	plan := npcHeartbeatOutings[outingIndex]
	startAt := nowLocal.Format(npcLocationTimeLayout)
	endAt := nowLocal.Add(time.Duration(durationMinutes) * time.Minute).Format(npcLocationTimeLayout)

	next.Location = NPCLocation{
		Area:       plan.Area,
		Scene:      plan.Scene,
		Activity:   plan.Activity,
		StartAt:    startAt,
		EndAt:      endAt,
		MoveReason: npcHeartbeatMoveReason,
	}
	appendLocationEvent(&next, now, fmt.Sprintf("went out: %s", plan.Activity))
	next.UpdatedAt = now.UTC().Format(time.RFC3339)

	return next, true
}
