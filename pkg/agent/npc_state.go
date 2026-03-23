package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
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
	npcStateVersion      = 2
	npcStateFileName     = "STATE.md"
	npcMemoryBeginMarker = "<!-- NPC_MEMORY_BEGIN -->"
	npcMemoryEndMarker   = "<!-- NPC_MEMORY_END -->"

	maxNPCRecentEvents       = 30
	maxNPCMemoryNotes        = 50
	npcUpdaterEveryTurns     = 5
	npcUpdaterTimeout        = 5 * time.Minute
	npcStateUpdaterPromptTag = "NPC_STATE_UPDATER_V3"
	npcUpdaterPromptTag      = "NPC_STATE_MEMORY_UPDATER_V2"

	npcLocationTimeLayout                   = "2006-01-02 15:04"
	npcHeartbeatDefaultIdleThresholdMinutes = 60
	npcHeartbeatDefaultMoveProbability      = 0.20
	npcHeartbeatDefaultMinDurationMinutes   = 35
	npcHeartbeatDefaultMaxDurationMinutes   = 75
	npcActivityRemoteChatSuffix             = " (multitasking, chatting remotely)"
	npcHeartbeatMoveReason                  = "idle heartbeat walk"
	npcHeartbeatReturnMoveReasonPrefix      = "finished "

	defaultNPCEmotionName = "calm"
)

const npcPersonRefPrefix = "person_"

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

type NPCPerson struct {
	DisplayName string `json:"display_name,omitempty"`
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
	People        map[string]NPCPerson       `json:"people,omitempty"`
	IdentifierMap map[string]string          `json:"identifier_map,omitempty"`
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
	state.UpdatedAt = stateTimestampString(time.Now())

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

	normalized = filterManagedMemoryNotesAgainstManualContent(normalized, existing)
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
		UpdatedAt: stateTimestampString(time.Now()),
		Emotion: NPCEmotion{
			Name:      defaultNPCEmotionName,
			Intensity: NPCEmotionIntensityMid,
		},
		Location: NPCLocation{
			Area:     "base",
			Scene:    "workspace",
			Activity: "observing",
		},
		People:        map[string]NPCPerson{},
		IdentifierMap: map[string]string{},
		Relationships: map[string]NPCRelationship{},
	})
}

func normalizeNPCState(state NPCState) NPCState {
	state.Version = npcStateVersion
	if strings.TrimSpace(state.UpdatedAt) == "" {
		state.UpdatedAt = stateTimestampString(time.Now())
	}
	state.UpdatedAt = normalizeStateTimestamp(state.UpdatedAt)

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
	state.Location.StartAt = normalizeLocationTimestamp(state.Location.StartAt)
	state.Location.EndAt = normalizeLocationTimestamp(state.Location.EndAt)
	state.Location.MoveReason = strings.TrimSpace(state.Location.MoveReason)

	state.People = normalizePeopleMap(state.People)
	state.IdentifierMap = normalizeIdentifierMap(state.IdentifierMap)
	if state.Relationships == nil {
		state.Relationships = make(map[string]NPCRelationship)
	}
	relationshipKeys := make([]string, 0, len(state.Relationships))
	for key := range state.Relationships {
		relationshipKeys = append(relationshipKeys, key)
	}
	sort.Strings(relationshipKeys)
	reservedRefs := reservedPersonRefs(state.People, state.IdentifierMap, state.Relationships)
	normalizedRelationships := make(map[string]NPCRelationship, len(state.Relationships))
	for _, key := range relationshipKeys {
		rel := state.Relationships[key]
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		if isPlaceholderRelationshipKey(normalizedKey) {
			continue
		}
		var personRef string
		if identifierKey := normalizeIdentifierKey(normalizedKey); identifierKey != "" {
			mappedRef := normalizePersonRef(state.IdentifierMap[identifierKey])
			if !isPersonRefKey(mappedRef) {
				mappedRef = nextAvailablePersonRefFromReserved("", reservedRefs)
			}
			state.IdentifierMap[identifierKey] = mappedRef
			personRef = mappedRef
		} else {
			personRef = normalizePersonRef(normalizedKey)
		}
		if !isPersonRefKey(personRef) {
			personRef = nextAvailablePersonRefFromReserved("", reservedRefs)
		}
		reservedRefs[personRef] = struct{}{}
		rel = normalizeNPCRelationship(rel)
		normalizedRelationships[personRef] = mergeNPCRelationship(normalizedRelationships[personRef], rel)
		if _, ok := state.People[personRef]; !ok {
			state.People[personRef] = NPCPerson{}
		}
	}
	state.Relationships = normalizedRelationships
	for identifierKey, personRef := range state.IdentifierMap {
		personRef = normalizePersonRef(personRef)
		if !isPersonRefKey(personRef) {
			delete(state.IdentifierMap, identifierKey)
			continue
		}
		state.IdentifierMap[identifierKey] = personRef
		if _, ok := state.People[personRef]; !ok {
			state.People[personRef] = NPCPerson{}
		}
	}
	for personRef, person := range state.People {
		person.DisplayName = normalizePersonDisplayName(person.DisplayName, personRef)
		state.People[personRef] = person
	}

	state.Habits = normalizeHabits(state.Habits)
	state.RecentEvents = normalizeRecentEvents(state.RecentEvents)

	return state
}

func isPlaceholderRelationshipKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "channel:user_id", "<channel:user_id>":
		return true
	default:
		return false
	}
}

func stateTimestampString(at time.Time) string {
	if at.IsZero() {
		at = time.Now()
	}
	return at.In(time.Local).Format(time.RFC3339)
}

func normalizeStateTimestamp(value string) string {
	if parsed, ok := parseStateTimestamp(value); ok {
		return stateTimestampString(parsed)
	}
	return stateTimestampString(time.Now())
}

func normalizeLocationTimestamp(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if parsed, ok := parseStateTimestamp(trimmed); ok {
		return stateTimestampString(parsed)
	}
	return trimmed
}

func normalizeNPCRelationship(rel NPCRelationship) NPCRelationship {
	rel.Affinity = normalizeNPCLevel(rel.Affinity, NPCLevelMid)
	rel.Trust = normalizeNPCLevel(rel.Trust, NPCLevelMid)
	rel.Familiarity = normalizeNPCLevel(rel.Familiarity, NPCLevelLow)
	rel.LastInteractionAt = normalizeOptionalStateTimestamp(rel.LastInteractionAt)
	rel.LastChannel = normalizeRelationshipChannel(rel.LastChannel)
	rel.LastChatID = strings.TrimSpace(rel.LastChatID)
	rel.LastPeerKind = normalizeRelationshipPeerKind(rel.LastPeerKind)
	rel.LastSessionKey = strings.TrimSpace(rel.LastSessionKey)
	rel.LastUserMessageAt = normalizeOptionalStateTimestamp(rel.LastUserMessageAt)
	rel.LastAgentMessageAt = normalizeOptionalStateTimestamp(rel.LastAgentMessageAt)
	rel.LastProactiveAttemptAt = normalizeOptionalStateTimestamp(rel.LastProactiveAttemptAt)
	rel.LastProactiveSuccessAt = normalizeOptionalStateTimestamp(rel.LastProactiveSuccessAt)
	rel.Notes = strings.TrimSpace(rel.Notes)
	return rel
}

func normalizeOptionalStateTimestamp(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if parsed, ok := parseStateTimestamp(trimmed); ok {
		return stateTimestampString(parsed)
	}
	return trimmed
}

func normalizePeopleMap(people map[string]NPCPerson) map[string]NPCPerson {
	if people == nil {
		return make(map[string]NPCPerson)
	}

	keys := make([]string, 0, len(people))
	reservedRefs := make(map[string]struct{}, len(people))
	for key := range people {
		keys = append(keys, key)
		if personRef := normalizePersonRef(key); isPersonRefKey(personRef) {
			reservedRefs[personRef] = struct{}{}
		}
	}
	sort.Strings(keys)

	normalized := make(map[string]NPCPerson, len(people))
	for _, key := range keys {
		person := people[key]
		personRef := normalizePersonRef(key)
		if !isPersonRefKey(personRef) {
			personRef = nextAvailablePersonRefFromReserved("", reservedRefs)
		}
		reservedRefs[personRef] = struct{}{}
		existing := normalized[personRef]
		if existing.DisplayName == "" {
			existing.DisplayName = strings.TrimSpace(person.DisplayName)
		}
		normalized[personRef] = existing
	}
	if normalized == nil {
		return make(map[string]NPCPerson)
	}
	return normalized
}

func normalizeIdentifierMap(identifierMap map[string]string) map[string]string {
	if identifierMap == nil {
		return make(map[string]string)
	}

	normalized := make(map[string]string, len(identifierMap))
	for key, personRef := range identifierMap {
		identifierKey := normalizeIdentifierKey(key)
		personRef = normalizePersonRef(personRef)
		if identifierKey == "" || !isPersonRefKey(personRef) {
			continue
		}
		normalized[identifierKey] = personRef
	}
	if normalized == nil {
		return make(map[string]string)
	}
	return normalized
}

func normalizePersonDisplayName(name string, personRef string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed != "" {
		return trimmed
	}

	label := strings.TrimPrefix(strings.TrimSpace(personRef), npcPersonRefPrefix)
	label = strings.Trim(label, "_")
	if label == "" || label == "person" {
		return "Contact"
	}
	label = strings.ReplaceAll(label, "_", " ")
	if strings.HasPrefix(label, "person ") {
		suffix := strings.TrimSpace(strings.TrimPrefix(label, "person "))
		if suffix == "" {
			return "Contact"
		}
		if isDigitsOnly(strings.ReplaceAll(suffix, " ", "")) {
			return "Contact " + strings.ReplaceAll(suffix, " ", "")
		}
		label = suffix
	}
	parts := strings.Fields(label)
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func isDigitsOnly(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func normalizePersonRef(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return ""
	}

	trimmed = strings.ReplaceAll(trimmed, "-", "_")
	trimmed = strings.ReplaceAll(trimmed, " ", "_")
	var b strings.Builder
	lastUnderscore := false
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	result := strings.Trim(b.String(), "_")
	if result == "" {
		return ""
	}
	if !strings.HasPrefix(result, npcPersonRefPrefix) {
		result = npcPersonRefPrefix + result
	}
	return result
}

func isPersonRefKey(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, npcPersonRefPrefix)
}

func nextAvailablePersonRef(label string, people map[string]NPCPerson, relationships map[string]NPCRelationship) string {
	base := normalizePersonRef(label)
	if !isPersonRefKey(base) {
		base = npcPersonRefPrefix + "contact"
	}
	if !personRefExists(base, people, relationships) {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s_%d", base, i)
		if !personRefExists(candidate, people, relationships) {
			return candidate
		}
	}
}

func nextAvailablePersonRefFromReserved(label string, reserved map[string]struct{}) string {
	base := normalizePersonRef(label)
	if !isPersonRefKey(base) {
		base = npcPersonRefPrefix + "contact"
	}
	if _, exists := reserved[base]; !exists {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s_%d", base, i)
		if _, exists := reserved[candidate]; !exists {
			return candidate
		}
	}
}

func reservedPersonRefs(
	people map[string]NPCPerson,
	identifierMap map[string]string,
	relationships map[string]NPCRelationship,
) map[string]struct{} {
	reserved := make(map[string]struct{}, len(people)+len(identifierMap)+len(relationships))
	for personRef := range people {
		personRef = normalizePersonRef(personRef)
		if isPersonRefKey(personRef) {
			reserved[personRef] = struct{}{}
		}
	}
	for _, personRef := range identifierMap {
		personRef = normalizePersonRef(personRef)
		if isPersonRefKey(personRef) {
			reserved[personRef] = struct{}{}
		}
	}
	for key := range relationships {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" || isPlaceholderRelationshipKey(normalizedKey) {
			continue
		}
		if identifierKey := normalizeIdentifierKey(normalizedKey); identifierKey != "" {
			personRef := normalizePersonRef(identifierMap[identifierKey])
			if isPersonRefKey(personRef) {
				reserved[personRef] = struct{}{}
			}
			continue
		}
		personRef := normalizePersonRef(normalizedKey)
		if isPersonRefKey(personRef) {
			reserved[personRef] = struct{}{}
		}
	}
	return reserved
}

func personRefExists(ref string, people map[string]NPCPerson, relationships map[string]NPCRelationship) bool {
	if _, ok := people[ref]; ok {
		return true
	}
	if relationships == nil {
		return false
	}
	_, ok := relationships[ref]
	return ok
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
			e.At = stateTimestampString(time.Now())
		} else {
			e.At = normalizeOptionalStateTimestamp(e.At)
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

func filterStateOwnedMemoryNotes(notes []string, state NPCState) []string {
	if len(notes) == 0 {
		return nil
	}

	transient := make(map[string]struct{})
	addTransient := func(value string) {
		normalized := strings.ToLower(normalizeMemoryNote(value))
		if normalized == "" {
			return
		}
		transient[normalized] = struct{}{}
	}

	addTransient(state.Emotion.Reason)
	addTransient(state.Location.Activity)
	addTransient(state.Location.MoveReason)
	for _, event := range state.RecentEvents {
		addTransient(event.Summary)
	}

	filtered := make([]string, 0, len(notes))
	for _, note := range normalizeMemoryNotes(notes) {
		key := strings.ToLower(normalizeMemoryNote(note))
		if key == "" {
			continue
		}
		if _, exists := transient[key]; exists {
			continue
		}
		filtered = append(filtered, note)
	}

	return normalizeMemoryNotes(filtered)
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
	return buildIdentifierKey(channel, senderID)
}

func buildIdentifierKey(channel, senderID string) string {
	ch := strings.ToLower(strings.TrimSpace(channel))
	sender := strings.ToLower(strings.TrimSpace(senderID))
	if ch == "" || sender == "" {
		return ""
	}
	return ch + ":" + sender
}

func normalizeIdentifierKey(key string) string {
	parts := strings.SplitN(strings.ToLower(strings.TrimSpace(key)), ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return buildIdentifierKey(parts[0], parts[1])
}

func bestKnownSenderDisplayName(msg bus.InboundMessage) string {
	candidates := []string{
		msg.Sender.DisplayName,
		msg.Sender.Username,
		msg.Metadata["sender_name"],
		msg.Metadata["display_name"],
		msg.Metadata["user_name"],
		msg.Metadata["username"],
		msg.Metadata["nickname"],
	}
	rawSenderID := strings.ToLower(strings.TrimSpace(msg.SenderID))
	rawPlatformID := strings.ToLower(strings.TrimSpace(msg.Sender.PlatformID))
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if lower == rawSenderID || lower == rawPlatformID {
			continue
		}
		return trimmed
	}
	return ""
}

func ensurePersonRef(state *NPCState, msg bus.InboundMessage) string {
	if state == nil {
		return ""
	}
	return ensurePersonRefForIdentifier(state, msg.Channel, msg.SenderID, bestKnownSenderDisplayName(msg))
}

func ensurePersonRefForIdentifier(state *NPCState, channel, senderID, displayName string) string {
	if state == nil {
		return ""
	}
	if state.People == nil {
		state.People = make(map[string]NPCPerson)
	}
	if state.IdentifierMap == nil {
		state.IdentifierMap = make(map[string]string)
	}

	identifierKey := buildIdentifierKey(channel, senderID)
	if identifierKey == "" {
		return ""
	}

	personRef := normalizePersonRef(state.IdentifierMap[identifierKey])
	if !isPersonRefKey(personRef) {
		personRef = nextAvailablePersonRef(displayName, state.People, state.Relationships)
		state.IdentifierMap[identifierKey] = personRef
	}

	person := state.People[personRef]
	if trimmed := strings.TrimSpace(displayName); trimmed != "" {
		person.DisplayName = trimmed
	}
	person.DisplayName = normalizePersonDisplayName(person.DisplayName, personRef)
	state.People[personRef] = person

	return personRef
}

func displayNameForPerson(state NPCState, personRef string) string {
	personRef = normalizePersonRef(personRef)
	if person, ok := state.People[personRef]; ok {
		if display := strings.TrimSpace(person.DisplayName); display != "" {
			return display
		}
	}
	return normalizePersonDisplayName("", personRef)
}

func senderIDForPerson(state NPCState, channel, personRef string) string {
	targetRef := normalizePersonRef(personRef)
	targetChannel := normalizeRelationshipChannel(channel)
	for identifierKey, mappedRef := range state.IdentifierMap {
		if normalizePersonRef(mappedRef) != targetRef {
			continue
		}
		parts := strings.SplitN(identifierKey, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if targetChannel != "" && parts[0] != targetChannel {
			continue
		}
		return strings.TrimSpace(parts[1])
	}
	return ""
}

func personRefForIdentifier(state NPCState, channel, senderID string) string {
	return normalizePersonRef(state.IdentifierMap[buildIdentifierKey(channel, senderID)])
}

func stateForPrompt(state NPCState) NPCState {
	sanitized := normalizeNPCState(state)
	sanitized.IdentifierMap = nil
	return sanitized
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

func filterManagedMemoryNotesAgainstManualContent(notes []string, existing string) []string {
	if len(notes) == 0 {
		return nil
	}
	manualLines := extractComparableMemoryLines(removeManagedMemoryBlock(existing))
	if len(manualLines) == 0 {
		return normalizeMemoryNotes(notes)
	}

	filtered := make([]string, 0, len(notes))
	for _, note := range normalizeMemoryNotes(notes) {
		key := strings.ToLower(normalizeMemoryNote(note))
		if key == "" {
			continue
		}
		if _, exists := manualLines[key]; exists {
			continue
		}
		filtered = append(filtered, note)
	}
	return normalizeMemoryNotes(filtered)
}

func removeManagedMemoryBlock(content string) string {
	start := strings.Index(content, npcMemoryBeginMarker)
	end := strings.Index(content, npcMemoryEndMarker)
	if start < 0 || end <= start {
		return content
	}

	replaceEnd := end + len(npcMemoryEndMarker)
	if replaceEnd < len(content) && content[replaceEnd] == '\r' {
		replaceEnd++
	}
	if replaceEnd < len(content) && content[replaceEnd] == '\n' {
		replaceEnd++
	}
	return content[:start] + content[replaceEnd:]
}

func extractComparableMemoryLines(content string) map[string]struct{} {
	lines := strings.Split(content, "\n")
	result := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		normalized := normalizeMemoryNote(line)
		if normalized == "" {
			continue
		}
		result[strings.ToLower(normalized)] = struct{}{}
	}
	return result
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
	notesToSave = filterStateOwnedMemoryNotes(notesToSave, update.State)
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

	locationCfg := defaultHeartbeatLocationConfig()
	if al != nil && al.cfg != nil {
		locationCfg = normalizeHeartbeatLocationConfig(al.cfg.Heartbeat.Location)
	}

	roll := rand.Float64()
	outingIndex := 0
	if len(npcHeartbeatOutings) > 0 {
		outingIndex = rand.IntN(len(npcHeartbeatOutings))
	}
	durationMinutes := locationCfg.MinDurationMinutes
	if locationCfg.MaxDurationMinutes > locationCfg.MinDurationMinutes {
		durationMinutes += rand.IntN(locationCfg.MaxDurationMinutes - locationCfg.MinDurationMinutes + 1)
	}

	nextState, changed := applyHeartbeatLocationPolicy(state, locationCfg, time.Now(), roll, outingIndex, durationMinutes)
	if !changed {
		return
	}

	if err := agent.StateStore.SaveState(nextState); err != nil {
		logger.WarnCF("agent", "Failed to save heartbeat location policy update",
			map[string]any{"agent_id": agent.ID, "error": err.Error()})
	}
}

func defaultHeartbeatLocationConfig() config.HeartbeatLocationConfig {
	return config.HeartbeatLocationConfig{
		Enabled:              true,
		IdleThresholdMinutes: npcHeartbeatDefaultIdleThresholdMinutes,
		OutingProbability:    npcHeartbeatDefaultMoveProbability,
		MinDurationMinutes:   npcHeartbeatDefaultMinDurationMinutes,
		MaxDurationMinutes:   npcHeartbeatDefaultMaxDurationMinutes,
	}
}

func normalizeHeartbeatLocationConfig(cfg config.HeartbeatLocationConfig) config.HeartbeatLocationConfig {
	if cfg == (config.HeartbeatLocationConfig{}) {
		return defaultHeartbeatLocationConfig()
	}

	if cfg.IdleThresholdMinutes <= 0 {
		cfg.IdleThresholdMinutes = npcHeartbeatDefaultIdleThresholdMinutes
	}
	switch {
	case cfg.OutingProbability < 0:
		cfg.OutingProbability = 0
	case cfg.OutingProbability > 1:
		cfg.OutingProbability = 1
	}
	if cfg.MinDurationMinutes <= 0 {
		cfg.MinDurationMinutes = npcHeartbeatDefaultMinDurationMinutes
	}
	if cfg.MaxDurationMinutes <= 0 {
		cfg.MaxDurationMinutes = npcHeartbeatDefaultMaxDurationMinutes
	}
	if cfg.MaxDurationMinutes < cfg.MinDurationMinutes {
		cfg.MaxDurationMinutes = cfg.MinDurationMinutes
	}

	return cfg
}

func buildNPCStateUpdatePayload(
	state NPCState,
	memoryNotes []string,
	msg bus.InboundMessage,
	sessionKey string,
	assistantReply string,
) ([]byte, string, error) {
	stateForPayload := normalizeNPCState(state)
	personRef := ensurePersonRef(&stateForPayload, msg)
	identifierKey := buildIdentifierKey(msg.Channel, msg.SenderID)
	payload := map[string]any{
		"previous_state": stateForPrompt(stateForPayload),
		"interaction": map[string]any{
			"timestamp":           stateTimestampString(time.Now()),
			"channel":             msg.Channel,
			"chat_id":             msg.ChatID,
			"sender_id":           msg.SenderID,
			"peer_kind":           msg.Peer.Kind,
			"peer_id":             msg.Peer.ID,
			"identifier_key":      identifierKey,
			"person_ref":          personRef,
			"sender_display_name": displayNameForPerson(stateForPayload, personRef),
			"session_key":         strings.TrimSpace(sessionKey),
			"user_message":        msg.Content,
			"assistant_reply":     assistantReply,
		},
	}
	if memoryNotes != nil {
		payload["existing_memory_notes"] = memoryNotes
	}

	inputJSON, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, "", err
	}
	return inputJSON, personRef, nil
}

func npcStateUpdateRules(personRef string) string {
	return fmt.Sprintf(`- Keep continuity from previous state unless interaction indicates change.
- emotion.name must be one of: %s.
- emotion.intensity must be one of: low, mid, high.
- Intensity behavior guide: low=subtle cues and mostly neutral language; mid=clear but balanced emotional expression; high=strong and direct expression matching context.
- Emotion transition rule: emotion.name may change only when previous_state.emotion.intensity is low.
- If previous_state.emotion.intensity is mid or high, keep emotion.name the same as previous_state.emotion.name.
- Example: previous_state angry/high cannot become calm in one update; lower intensity first.
- location tracks off-chat activity and whereabouts; use start_at/end_at as RFC3339 timestamps with timezone offset.
- If previous_state.location indicates an active outing window (between start_at and end_at), keep the outing location/activity and add a multitasking cue for chatting remotely.
- Do not invent spontaneous location moves during replied-turn updates; heartbeat/autonomous policies handle unprompted outings. Only change location when the interaction clearly implies movement or when continuing an active outing.
- previous_state.people stores stable person refs and human-readable display names. Keep person refs stable.
- Ensure person ref %q exists in both people and relationships.
- Use display names or neutral contact labels in notes and summaries. Never use raw channel:user_id text in habits, relationship notes, recent event summaries, or memory notes.
- Existing notes may mention 助手 in third person. Interpret that as referring to this agent unless it is clearly quoted user speech or contrasted with another assistant.
- Write new emotion.reason, relationship notes, recent event summaries, and memory_notes from the agent's own perspective. Do not refer to self as 助手 in new text.
- Preserve relationship contact/session/timestamp fields unless the current interaction updates them.
- affinity = emotional warmth and liking toward the user.
- trust = willingness to rely on the user, believe them, or be vulnerable with them.
- familiarity = shared history, routine, and mutual knowing built over repeated interactions.
- Update affinity, trust, and familiarity conservatively. Usually keep the previous level unless the interaction provides clear evidence for a change.
- Change each of affinity, trust, and familiarity by at most one step per interaction.
- Increase affinity after clear warmth, kindness, fun rapport, or support; decrease it after repeated coldness, insults, manipulation, or unwanted pressure.
- Increase trust after reliability, honesty, discretion, respect, or supportive behavior; decrease it after deception, broken expectations, coercion, or disrespect.
- Increase familiarity mainly through repeated interactions, callbacks, routines, or shared history, not from one emotional spike alone.
- Do not decrease affinity, trust, or familiarity because of silence, delayed replies, or neutral small talk alone.
- Keep transient logistics, trip-progress beats, and very recent chat moments in state recent_events instead of memory_notes.
- memory_notes should only keep durable preferences, identity mappings, stable interaction rules, and long-lived plans that matter across sessions.`, strings.Join(npcAllowedEmotionNames, ", "), personRef)
}

func buildNPCStateOnlySystemPrompt(personRef string) string {
	return fmt.Sprintf(`%s
You update internal roleplay state for one dedicated NPC agent.
Return JSON only, no markdown, no explanations.

Output shape:
{
  "version": 2,
  "updated_at": "RFC3339 timestamp",
  "emotion": {"name": "string", "intensity": "low|mid|high", "reason": "string"},
  "location": {"area": "string", "scene": "string", "activity": "string", "start_at": "RFC3339 timestamp with timezone offset", "end_at": "RFC3339 timestamp with timezone offset", "move_reason": "string"},
  "people": {
    %q: {"display_name": "string"}
  },
  "relationships": {
    %q: {"affinity": "low|mid|high", "trust": "low|mid|high", "familiarity": "low|mid|high", "last_interaction_at": "RFC3339 timestamp", "last_channel": "string", "last_chat_id": "string", "last_peer_kind": "string", "last_session_key": "string", "last_user_message_at": "RFC3339 timestamp", "last_agent_message_at": "RFC3339 timestamp", "last_proactive_attempt_at": "RFC3339 timestamp", "last_proactive_success_at": "RFC3339 timestamp", "notes": "string"}
  },
  "habits": ["string"],
  "recent_events": [{"at": "RFC3339", "type": "string", "summary": "string"}]
}

Rules:
%s
- Return a valid JSON object only.`, npcStateUpdaterPromptTag, personRef, personRef, npcStateUpdateRules(personRef))
}

func buildNPCStateAndMemorySystemPrompt(personRef string) string {
	return fmt.Sprintf(`%s
You update internal roleplay state and long-term memory notes for one dedicated NPC agent.
Return JSON only, no markdown, no explanations.

Output shape:
{
  "state": {
    "version": 2,
    "updated_at": "RFC3339 timestamp",
    "emotion": {"name": "string", "intensity": "low|mid|high", "reason": "string"},
    "location": {"area": "string", "scene": "string", "activity": "string", "start_at": "RFC3339 timestamp with timezone offset", "end_at": "RFC3339 timestamp with timezone offset", "move_reason": "string"},
    "people": {
      %q: {"display_name": "string"}
    },
    "relationships": {
      %q: {"affinity": "low|mid|high", "trust": "low|mid|high", "familiarity": "low|mid|high", "last_interaction_at": "RFC3339 timestamp", "last_channel": "string", "last_chat_id": "string", "last_peer_kind": "string", "last_session_key": "string", "last_user_message_at": "RFC3339 timestamp", "last_agent_message_at": "RFC3339 timestamp", "last_proactive_attempt_at": "RFC3339 timestamp", "last_proactive_success_at": "RFC3339 timestamp", "notes": "string"}
    },
    "habits": ["string"],
    "recent_events": [{"at": "RFC3339", "type": "string", "summary": "string"}]
  },
  "memory_notes": ["string"]
}

Rules:
%s
- Keep memory_notes concise, deduplicated, and <= %d.
- Merge/edit existing notes when possible instead of blind append.
- Return valid JSON object only.`, npcUpdaterPromptTag, personRef, personRef, npcStateUpdateRules(personRef), maxNPCMemoryNotes)
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

	systemPrompt := buildNPCStateOnlySystemPrompt(relationshipKey)

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

	systemPrompt := buildNPCStateAndMemorySystemPrompt(relationshipKey)

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
	*state = normalizeNPCState(*state)
	if state.Relationships == nil {
		state.Relationships = make(map[string]NPCRelationship)
	}
	personRef := ensurePersonRef(state, msg)
	if personRef == "" {
		return
	}

	now := stateTimestampString(time.Now())
	rel, ok := state.Relationships[personRef]
	if !ok {
		rel = NPCRelationship{
			Affinity:    NPCLevelMid,
			Trust:       NPCLevelMid,
			Familiarity: NPCLevelLow,
		}
	}
	rel.LastChannel = normalizeRelationshipChannel(msg.Channel)
	rel.LastChatID = strings.TrimSpace(msg.ChatID)
	rel.LastPeerKind = normalizeRelationshipPeerKind(msg.Peer.Kind)
	rel.LastUserMessageAt = now
	rel.LastInteractionAt = now
	state.Relationships[personRef] = rel
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
	*state = normalizeNPCState(*state)
	if state.Relationships == nil {
		state.Relationships = make(map[string]NPCRelationship)
	}
	personRef := ensurePersonRef(state, msg)
	if personRef == "" {
		return
	}

	timestamp := stateTimestampString(replyAt)
	rel, ok := state.Relationships[personRef]
	if !ok {
		rel = NPCRelationship{
			Affinity:    NPCLevelMid,
			Trust:       NPCLevelMid,
			Familiarity: NPCLevelLow,
		}
	}
	rel.LastChannel = normalizeRelationshipChannel(msg.Channel)
	rel.LastChatID = strings.TrimSpace(msg.ChatID)
	rel.LastPeerKind = normalizeRelationshipPeerKind(msg.Peer.Kind)
	rel.LastSessionKey = strings.TrimSpace(sessionKey)
	rel.LastUserMessageAt = timestamp
	rel.LastAgentMessageAt = timestamp
	rel.LastInteractionAt = timestamp
	state.Relationships[personRef] = rel
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
		At:      stateTimestampString(time.Now()),
		Type:    "chat",
		Summary: summary,
	}
	state.RecentEvents = append(state.RecentEvents, event)
	state.RecentEvents = normalizeRecentEvents(state.RecentEvents)
	state.UpdatedAt = stateTimestampString(time.Now())
}

func mergeNPCState(latest NPCState, next NPCState) NPCState {
	latest = normalizeNPCState(latest)
	next = normalizeNPCState(next)

	merged := next
	merged.TrackedTurns = max(latest.TrackedTurns, next.TrackedTurns)
	merged.People = make(map[string]NPCPerson, len(latest.People)+len(next.People))
	for key, person := range latest.People {
		merged.People[key] = person
	}
	for key, person := range next.People {
		merged.People[key] = mergeNPCPerson(merged.People[key], person, key)
	}
	merged.IdentifierMap = make(map[string]string, len(latest.IdentifierMap)+len(next.IdentifierMap))
	for key, personRef := range latest.IdentifierMap {
		merged.IdentifierMap[key] = personRef
	}
	for key, personRef := range next.IdentifierMap {
		if normalized := normalizePersonRef(personRef); isPersonRefKey(normalized) {
			merged.IdentifierMap[key] = normalized
		}
	}
	merged.Relationships = make(map[string]NPCRelationship, len(latest.Relationships)+len(next.Relationships))

	for key, rel := range latest.Relationships {
		merged.Relationships[key] = rel
	}
	for key, rel := range next.Relationships {
		merged.Relationships[key] = mergeNPCRelationship(merged.Relationships[key], rel)
	}

	return normalizeNPCState(merged)
}

func mergeNPCPerson(latest NPCPerson, next NPCPerson, personRef string) NPCPerson {
	merged := latest
	if display := strings.TrimSpace(next.DisplayName); display != "" {
		merged.DisplayName = display
	}
	merged.DisplayName = normalizePersonDisplayName(merged.DisplayName, personRef)
	return merged
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
	merged.Notes = preferNextNonEmptyString(latest.Notes, next.Notes)
	return merged
}

func preferLatestNonEmptyString(latest string, next string) string {
	latest = strings.TrimSpace(latest)
	if latest != "" {
		return latest
	}
	return strings.TrimSpace(next)
}

func preferNextNonEmptyString(latest string, next string) string {
	next = strings.TrimSpace(next)
	if next != "" {
		return next
	}
	return strings.TrimSpace(latest)
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

	leftAt, leftOK := parseStateTimestamp(left)
	rightAt, rightOK := parseStateTimestamp(right)
	switch {
	case leftOK && rightOK:
		if rightAt.After(leftAt) {
			return stateTimestampString(rightAt)
		}
		return stateTimestampString(leftAt)
	case leftOK:
		return stateTimestampString(leftAt)
	case rightOK:
		return stateTimestampString(rightAt)
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

func parseStateTimestamp(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}

	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed, true
	}

	parsed, err := time.ParseInLocation(npcLocationTimeLayout, trimmed, time.Local)
	if err != nil {
		return time.Time{}, false
	}

	return parsed, true
}

func isActiveOutingWindow(location NPCLocation, now time.Time) bool {
	startAt, hasStart := parseStateTimestamp(location.StartAt)
	endAt, hasEnd := parseStateTimestamp(location.EndAt)

	if !hasStart && !hasEnd {
		return false
	}
	if hasStart && now.Before(startAt) {
		return false
	}
	if hasEnd && !now.Before(endAt) {
		return false
	}

	return true
}

func parseRFC3339(value string) (time.Time, bool) {
	return parseStateTimestamp(value)
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
		At:      stateTimestampString(at),
		Type:    "location",
		Summary: trimmed,
	})
	state.RecentEvents = normalizeRecentEvents(state.RecentEvents)
}

func applyHeartbeatLocationPolicy(
	state NPCState,
	locationCfg config.HeartbeatLocationConfig,
	now time.Time,
	roll float64,
	outingIndex int,
	durationMinutes int,
) (NPCState, bool) {
	next := normalizeNPCState(state)
	locationCfg = normalizeHeartbeatLocationConfig(locationCfg)

	if endAt, hasEnd := parseStateTimestamp(next.Location.EndAt); hasEnd && !now.Before(endAt) {
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
		next.UpdatedAt = stateTimestampString(now)
		return next, true
	}

	if isActiveOutingWindow(next.Location, now) {
		return next, false
	}
	if !locationCfg.Enabled {
		return next, false
	}

	lastInteraction, ok := latestInteractionAt(next)
	if !ok {
		return next, false
	}
	idleThreshold := time.Duration(locationCfg.IdleThresholdMinutes) * time.Minute
	if now.Sub(lastInteraction) < idleThreshold {
		return next, false
	}
	if roll >= locationCfg.OutingProbability {
		return next, false
	}
	if len(npcHeartbeatOutings) == 0 {
		return next, false
	}

	if outingIndex < 0 || outingIndex >= len(npcHeartbeatOutings) {
		outingIndex = 0
	}
	minDuration := locationCfg.MinDurationMinutes
	maxDuration := locationCfg.MaxDurationMinutes
	if durationMinutes < minDuration || durationMinutes > maxDuration {
		durationMinutes = minDuration
	}

	plan := npcHeartbeatOutings[outingIndex]
	startAt := stateTimestampString(now)
	endAt := stateTimestampString(now.Add(time.Duration(durationMinutes) * time.Minute))

	next.Location = NPCLocation{
		Area:       plan.Area,
		Scene:      plan.Scene,
		Activity:   plan.Activity,
		StartAt:    startAt,
		EndAt:      endAt,
		MoveReason: npcHeartbeatMoveReason,
	}
	appendLocationEvent(&next, now, fmt.Sprintf("went out: %s", plan.Activity))
	next.UpdatedAt = stateTimestampString(now)

	return next, true
}
