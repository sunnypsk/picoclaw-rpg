package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/routing"
)

func TestNPCStateStore_SaveLoadRoundTrip(t *testing.T) {
	workspace := t.TempDir()
	store := NewNPCStateStore(workspace)

	state := defaultNPCState()
	state.Emotion = NPCEmotion{Name: "excited", Intensity: NPCEmotionIntensityHigh, Reason: "met a new traveler"}
	state.Location = NPCLocation{
		Area:       "harbor",
		Scene:      "boardwalk",
		Activity:   "walking",
		StartAt:    "2026-03-05 18:30",
		EndAt:      "2026-03-05 20:00",
		MoveReason: "evening exploration",
	}
	state.Relationships = map[string]NPCRelationship{
		"telegram:user1": {Affinity: NPCLevelHigh, Trust: NPCLevelMid, Familiarity: NPCLevelLow},
	}
	state.Habits = []string{"keeps notes", "greets politely"}

	if err := store.SaveState(state); err != nil {
		t.Fatalf("SaveState() error: %v", err)
	}

	loaded, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}

	if loaded.Emotion.Name != "excited" {
		t.Fatalf("Emotion.Name = %q, want %q", loaded.Emotion.Name, "excited")
	}
	if loaded.Emotion.Intensity != NPCEmotionIntensityHigh {
		t.Fatalf("Emotion.Intensity = %q, want %q", loaded.Emotion.Intensity, NPCEmotionIntensityHigh)
	}
	if loaded.Location.Area != "harbor" {
		t.Fatalf("Location.Area = %q, want %q", loaded.Location.Area, "harbor")
	}
	if loaded.Location.StartAt != "2026-03-05 18:30" {
		t.Fatalf("Location.StartAt = %q, want %q", loaded.Location.StartAt, "2026-03-05 18:30")
	}
	if loaded.Location.EndAt != "2026-03-05 20:00" {
		t.Fatalf("Location.EndAt = %q, want %q", loaded.Location.EndAt, "2026-03-05 20:00")
	}
	if _, ok := loaded.Relationships["telegram:user1"]; !ok {
		t.Fatalf("expected relationship key telegram:user1")
	}
	if rel := loaded.Relationships["telegram:user1"]; rel.Affinity != NPCLevelHigh || rel.Trust != NPCLevelMid || rel.Familiarity != NPCLevelLow {
		t.Fatalf("unexpected relationship levels: %+v", rel)
	}
}

func TestNPCStateStore_LoadState_LegacyNumericIntensity(t *testing.T) {
	workspace := t.TempDir()
	store := NewNPCStateStore(workspace)

	legacy := "# NPC State\n\n```json\n{\n  \"version\": 1,\n  \"emotion\": {\n    \"name\": \"calm\",\n    \"intensity\": 80,\n    \"reason\": \"legacy format\"\n  }\n}\n```\n"
	if err := os.WriteFile(store.StatePath(), []byte(legacy), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	loaded, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	if loaded.Emotion.Intensity != NPCEmotionIntensityHigh {
		t.Fatalf("Emotion.Intensity = %q, want %q", loaded.Emotion.Intensity, NPCEmotionIntensityHigh)
	}
}

func TestNPCStateStore_LoadState_LegacyMovedAtMapsToStartAt(t *testing.T) {
	workspace := t.TempDir()
	store := NewNPCStateStore(workspace)

	legacy := "# NPC State\n\n```json\n{\n  \"version\": 1,\n  \"location\": {\n    \"area\": \"town\",\n    \"scene\": \"gate\",\n    \"activity\": \"traveling\",\n    \"moved_at\": \"2026-03-05 18:30\",\n    \"move_reason\": \"daily stroll\"\n  }\n}\n```\n"
	if err := os.WriteFile(store.StatePath(), []byte(legacy), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	loaded, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}

	if loaded.Location.StartAt != "2026-03-05 18:30" {
		t.Fatalf("Location.StartAt = %q, want %q", loaded.Location.StartAt, "2026-03-05 18:30")
	}
	if loaded.Location.MoveReason != "daily stroll" {
		t.Fatalf("Location.MoveReason = %q, want %q", loaded.Location.MoveReason, "daily stroll")
	}

	if err := store.SaveState(loaded); err != nil {
		t.Fatalf("SaveState() error: %v", err)
	}

	raw, err := os.ReadFile(store.StatePath())
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if strings.Contains(string(raw), "\"moved_at\"") {
		t.Fatalf("saved state should not contain legacy moved_at field: %s", string(raw))
	}
}

func TestPreserveActiveOutingDuringChat_PreservesLocationAndMarksRemote(t *testing.T) {
	now := time.Date(2026, 3, 5, 20, 0, 0, 0, time.Local)
	previous := NPCLocation{
		Area:       "park",
		Scene:      "riverside path",
		Activity:   "out for a walk",
		StartAt:    now.Add(-20 * time.Minute).Format(npcLocationTimeLayout),
		EndAt:      now.Add(40 * time.Minute).Format(npcLocationTimeLayout),
		MoveReason: "evening walk",
	}
	next := NPCLocation{Area: "base", Scene: "workspace", Activity: "observing"}

	preserveActiveOutingDuringChat(previous, &next, now)

	if next.Area != "park" || next.Scene != "riverside path" {
		t.Fatalf("expected outing location to be preserved, got area=%q scene=%q", next.Area, next.Scene)
	}
	if !strings.Contains(strings.ToLower(next.Activity), "chatting remotely") {
		t.Fatalf("expected remote chat marker in activity, got %q", next.Activity)
	}
	if next.StartAt != previous.StartAt || next.EndAt != previous.EndAt {
		t.Fatalf("expected start/end to be preserved, got start=%q end=%q", next.StartAt, next.EndAt)
	}
}

func TestApplyHeartbeatLocationPolicy_StartsOutingWhenIdle(t *testing.T) {
	now := time.Date(2026, 3, 5, 21, 0, 0, 0, time.Local)
	state := defaultNPCState()
	state.Relationships = map[string]NPCRelationship{
		"telegram:user1": {
			LastInteractionAt: now.Add(-3 * time.Hour).UTC().Format(time.RFC3339),
		},
	}

	next, changed := applyHeartbeatLocationPolicy(state, now, 0.01, 1, 50)
	if !changed {
		t.Fatalf("expected heartbeat policy to start an outing")
	}
	if next.Location.StartAt == "" || next.Location.EndAt == "" {
		t.Fatalf("expected outing start/end to be populated, got start=%q end=%q", next.Location.StartAt, next.Location.EndAt)
	}
	if next.Location.MoveReason != npcHeartbeatMoveReason {
		t.Fatalf("MoveReason = %q, want %q", next.Location.MoveReason, npcHeartbeatMoveReason)
	}
	if !isActiveOutingWindow(next.Location, now) {
		t.Fatalf("expected active outing window after heartbeat move: %+v", next.Location)
	}
}

func TestApplyHeartbeatLocationPolicy_NoOutingWhenRollHigh(t *testing.T) {
	now := time.Date(2026, 3, 5, 21, 0, 0, 0, time.Local)
	state := defaultNPCState()
	state.Relationships = map[string]NPCRelationship{
		"telegram:user1": {
			LastInteractionAt: now.Add(-3 * time.Hour).UTC().Format(time.RFC3339),
		},
	}

	next, changed := applyHeartbeatLocationPolicy(state, now, 0.9, 0, 45)
	if changed {
		t.Fatalf("expected no movement when random roll is high, got %+v", next.Location)
	}
}

func TestApplyHeartbeatLocationPolicy_ReturnsToBaseAfterOutingEnds(t *testing.T) {
	now := time.Date(2026, 3, 5, 22, 0, 0, 0, time.Local)
	state := defaultNPCState()
	state.Location = NPCLocation{
		Area:       "park",
		Scene:      "riverside path",
		Activity:   "out for a walk",
		StartAt:    now.Add(-2 * time.Hour).Format(npcLocationTimeLayout),
		EndAt:      now.Add(-5 * time.Minute).Format(npcLocationTimeLayout),
		MoveReason: npcHeartbeatMoveReason,
	}

	next, changed := applyHeartbeatLocationPolicy(state, now, 0.9, 0, 45)
	if !changed {
		t.Fatalf("expected heartbeat policy to return to base after outing end")
	}
	if next.Location.Area != "base" || next.Location.Scene != "workspace" || next.Location.Activity != "observing" {
		t.Fatalf("expected return to base/workspace/observing, got %+v", next.Location)
	}
	if next.Location.StartAt != "" || next.Location.EndAt != "" {
		t.Fatalf("expected outing times to be cleared after return, got start=%q end=%q", next.Location.StartAt, next.Location.EndAt)
	}
}

func TestNormalizeEmotionName_AllowsRequestedMoodNames(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "naughty", want: "naughty"},
		{input: "angry", want: "angry"},
		{input: "withdrawn", want: "withdrawn"},
		{input: "dont want to talk to ppl", want: defaultNPCEmotionName},
		{input: "don't want to talk to ppl", want: defaultNPCEmotionName},
		{input: "dont_want_to_talk_to_people", want: defaultNPCEmotionName},
		{input: "unknown-emotion", want: defaultNPCEmotionName},
	}

	for _, tc := range tests {
		if got := normalizeEmotionName(tc.input); got != tc.want {
			t.Fatalf("normalizeEmotionName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeNPCLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    NPCLevel
		fallback NPCLevel
		want     NPCLevel
	}{
		{name: "low", input: NPCLevelLow, fallback: NPCLevelMid, want: NPCLevelLow},
		{name: "middle alias", input: "middle", fallback: NPCLevelLow, want: NPCLevelMid},
		{name: "invalid uses fallback", input: "unknown", fallback: NPCLevelHigh, want: NPCLevelHigh},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeNPCLevel(tc.input, tc.fallback); got != tc.want {
				t.Fatalf("normalizeNPCLevel(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestManagedMemoryBlock_UpsertPreservesManualContent(t *testing.T) {
	content := "# Manual Notes\nKeep this.\n\nSome custom text."
	updated := upsertManagedMemoryBlock(content, []string{"likes RPG taverns", "prefers short replies"})

	if !strings.Contains(updated, "# Manual Notes") {
		t.Fatalf("manual content should be preserved: %q", updated)
	}
	if !strings.Contains(updated, npcMemoryBeginMarker) || !strings.Contains(updated, npcMemoryEndMarker) {
		t.Fatalf("managed markers missing: %q", updated)
	}

	notes := extractManagedMemoryNotes(updated)
	if len(notes) != 2 {
		t.Fatalf("notes len = %d, want 2", len(notes))
	}
}

func TestNPCStateStore_SaveMemoryNotes_DedupAndLimit(t *testing.T) {
	workspace := t.TempDir()
	store := NewNPCStateStore(workspace)

	notes := make([]string, 0, 64)
	for i := 0; i < 70; i++ {
		note := fmt.Sprintf("unique note %02d", i)
		notes = append(notes, note)
		if i%2 == 0 {
			notes = append(notes, note)
		}
	}

	if err := store.SaveMemoryNotes(notes); err != nil {
		t.Fatalf("SaveMemoryNotes() error: %v", err)
	}

	loaded, err := store.LoadMemoryNotes()
	if err != nil {
		t.Fatalf("LoadMemoryNotes() error: %v", err)
	}

	if len(loaded) > maxNPCMemoryNotes {
		t.Fatalf("notes len = %d, should be <= %d", len(loaded), maxNPCMemoryNotes)
	}
	if len(loaded) != maxNPCMemoryNotes {
		t.Fatalf("notes len = %d, want %d (capped)", len(loaded), maxNPCMemoryNotes)
	}
}

func TestNPCStateStore_SaveOperations_LogStatus(t *testing.T) {
	workspace := t.TempDir()

	initialLevel := logger.GetLevel()
	defer logger.SetLevel(initialLevel)

	logPath := filepath.Join(workspace, "npc-state.log")
	if err := logger.EnableFileLogging(logPath); err != nil {
		t.Fatalf("EnableFileLogging() error: %v", err)
	}
	defer logger.DisableFileLogging()

	logger.SetLevel(logger.INFO)

	store := NewNPCStateStore(workspace)
	state := defaultNPCState()
	state.Emotion = NPCEmotion{Name: "focused", Intensity: NPCEmotionIntensityMid, Reason: "updating logs"}

	if err := store.SaveState(state); err != nil {
		t.Fatalf("SaveState() error: %v", err)
	}
	if err := store.SaveMemoryNotes([]string{"prefers concise replies", "prefers concise replies"}); err != nil {
		t.Fatalf("SaveMemoryNotes() error: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	logText := string(raw)

	if !strings.Contains(logText, `"message":"State file updated"`) {
		t.Fatalf("expected state update log, got: %s", logText)
	}
	if !strings.Contains(logText, `"message":"Memory notes updated"`) {
		t.Fatalf("expected memory update log, got: %s", logText)
	}
	if !strings.Contains(logText, `"emotion":"focused"`) {
		t.Fatalf("expected emotion field in state log, got: %s", logText)
	}
	if !strings.Contains(logText, `"notes_count":1`) {
		t.Fatalf("expected deduplicated notes count in log, got: %s", logText)
	}
}

type npcStateTestProvider struct{}

func (m *npcStateTestProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	if len(messages) > 0 && messages[0].Role == "system" && strings.Contains(messages[0].Content, npcUpdaterPromptTag) {
		sender := senderIDFromUpdaterInput(messages)
		relationshipKey := "telegram:" + sender
		update := npcStateUpdateResult{
			State: NPCState{
				Version:   1,
				UpdatedAt: "2026-01-01T00:00:00Z",
				Emotion: NPCEmotion{
					Name:      "cheerful",
					Intensity: NPCEmotionIntensityMid,
					Reason:    "had a chat",
				},
				Location: NPCLocation{
					Area:     "market",
					Scene:    "main square",
					Activity: "wandering",
				},
				Relationships: map[string]NPCRelationship{
					relationshipKey: {
						Affinity:    NPCLevelMid,
						Trust:       NPCLevelMid,
						Familiarity: NPCLevelLow,
					},
				},
				Habits:       []string{"greets politely"},
				RecentEvents: []NPCRecentEvent{{At: "2026-01-01T00:00:00Z", Type: "chat", Summary: "talked with " + sender}},
			},
			MemoryNotes: []string{"prefers RPG style", "sender=" + sender},
		}
		data, _ := json.Marshal(update)
		return &providers.LLMResponse{Content: string(data), ToolCalls: []providers.ToolCall{}}, nil
	}

	return &providers.LLMResponse{Content: "ok", ToolCalls: []providers.ToolCall{}}, nil
}

func (m *npcStateTestProvider) GetDefaultModel() string {
	return "mock-model"
}

func senderIDFromUpdaterInput(messages []providers.Message) string {
	if len(messages) < 2 {
		return "unknown"
	}
	content := messages[1].Content
	marker := `"sender_id": "`
	idx := strings.Index(content, marker)
	if idx < 0 {
		return "unknown"
	}
	rest := content[idx+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return "unknown"
	}
	if rest[:end] == "" {
		return "unknown"
	}
	return rest[:end]
}

func waitForCondition(t *testing.T, timeout, interval time.Duration, check func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := "condition not met"

	for time.Now().Before(deadline) {
		ok, detail := check()
		if ok {
			return
		}
		if detail != "" {
			last = detail
		}
		time.Sleep(interval)
	}

	t.Fatalf("timeout waiting for condition (%s): %s", timeout, last)
}

func TestAgentLoop_StrictAutoProvision_UpdatesStateAndMemory(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	workspace := filepath.Join(tmpHome, "main-workspace")
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         workspace,
				Model:             "mock-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
			AutoProvision: config.AutoProvisionConfig{
				Enabled:        true,
				StrictOneToOne: true,
				ChatTypes:      []string{"direct", "group", "channel"},
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &npcStateTestProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	msg := bus.InboundMessage{
		Channel:  "telegram",
		SenderID: "user42",
		ChatID:   "chat42",
		Content:  "hello",
		Peer:     bus.Peer{Kind: "direct", ID: "user42"},
	}

	response, err := al.processMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("processMessage() error: %v", err)
	}
	if response != "ok" {
		t.Fatalf("response = %q, want %q", response, "ok")
	}

	route := al.registry.ResolveRoute(routing.RouteInput{
		Channel: "telegram",
		Peer:    &routing.RoutePeer{Kind: "direct", ID: "user42"},
	})
	if route.MatchedBy != "auto-provision" {
		t.Fatalf("MatchedBy = %q, want auto-provision", route.MatchedBy)
	}

	agent, ok := al.registry.GetAgent(route.AgentID)
	if !ok {
		t.Fatalf("expected auto-provisioned agent %q", route.AgentID)
	}

	waitForCondition(t, 4*time.Second, 40*time.Millisecond, func() (bool, string) {
		state, err := agent.StateStore.LoadState()
		if err != nil {
			return false, fmt.Sprintf("LoadState() error: %v", err)
		}

		if state.Emotion.Name != "cheerful" {
			return false, fmt.Sprintf("Emotion.Name = %q, want cheerful", state.Emotion.Name)
		}
		if _, ok := state.Relationships["telegram:user42"]; !ok {
			return false, "relationship telegram:user42 not updated yet"
		}

		notes, loadErr := agent.StateStore.LoadMemoryNotes()
		if loadErr != nil {
			return false, fmt.Sprintf("LoadMemoryNotes() error: %v", loadErr)
		}
		if len(notes) == 0 {
			return false, "managed memory notes not written yet"
		}

		memoryRaw, readErr := os.ReadFile(agent.StateStore.MemoryPath())
		if readErr != nil {
			return false, fmt.Sprintf("ReadFile(memory) error: %v", readErr)
		}
		if !strings.Contains(string(memoryRaw), npcMemoryBeginMarker) {
			return false, "memory file missing managed block"
		}

		return true, ""
	})
}

func TestAgentLoop_StrictAutoProvision_IsolatesStatePerPeer(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	workspace := filepath.Join(tmpHome, "main-workspace")
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         workspace,
				Model:             "mock-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
			AutoProvision: config.AutoProvisionConfig{
				Enabled:        true,
				StrictOneToOne: true,
				ChatTypes:      []string{"direct", "group", "channel"},
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &npcStateTestProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	messages := []bus.InboundMessage{
		{Channel: "telegram", SenderID: "u1", ChatID: "group-chat", Content: "hi", Peer: bus.Peer{Kind: "group", ID: "group-chat"}},
		{Channel: "telegram", SenderID: "u2", ChatID: "group-chat", Content: "hello", Peer: bus.Peer{Kind: "group", ID: "group-chat-2"}},
	}
	for _, msg := range messages {
		if _, err := al.processMessage(context.Background(), msg); err != nil {
			t.Fatalf("processMessage() error: %v", err)
		}
	}

	route1 := al.registry.ResolveRoute(routing.RouteInput{Channel: "telegram", Peer: &routing.RoutePeer{Kind: "group", ID: "group-chat"}})
	route2 := al.registry.ResolveRoute(routing.RouteInput{Channel: "telegram", Peer: &routing.RoutePeer{Kind: "group", ID: "group-chat-2"}})
	if route1.AgentID == route2.AgentID {
		t.Fatalf("expected isolated agent IDs, got same %q", route1.AgentID)
	}

	agent1, ok := al.registry.GetAgent(route1.AgentID)
	if !ok {
		t.Fatalf("missing agent for route1")
	}
	agent2, ok := al.registry.GetAgent(route2.AgentID)
	if !ok {
		t.Fatalf("missing agent for route2")
	}

	waitForCondition(t, 4*time.Second, 40*time.Millisecond, func() (bool, string) {
		notes1, err := agent1.StateStore.LoadMemoryNotes()
		if err != nil {
			return false, fmt.Sprintf("LoadMemoryNotes(agent1) error: %v", err)
		}
		notes2, err := agent2.StateStore.LoadMemoryNotes()
		if err != nil {
			return false, fmt.Sprintf("LoadMemoryNotes(agent2) error: %v", err)
		}
		if len(notes1) == 0 || len(notes2) == 0 {
			return false, "waiting for per-agent memory notes"
		}
		if strings.Join(notes1, "|") == strings.Join(notes2, "|") {
			return false, fmt.Sprintf("expected different per-agent memory notes, got same: %v", notes1)
		}
		return true, ""
	})
}
