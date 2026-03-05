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
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/routing"
)

func TestNPCStateStore_SaveLoadRoundTrip(t *testing.T) {
	workspace := t.TempDir()
	store := NewNPCStateStore(workspace)

	state := defaultNPCState()
	state.Emotion = NPCEmotion{Name: "excited", Intensity: NPCEmotionIntensityHigh, Reason: "met a new traveler"}
	state.Location = NPCLocation{Area: "harbor", Scene: "boardwalk", Activity: "walking"}
	state.Relationships = map[string]NPCRelationship{
		"telegram:user1": {Affinity: NPCLevelHigh, Trust: NPCLevelMid, Familiarity: NPCLevelLow},
	}
	state.Vitals = NPCVitals{Energy: 80, Stress: 30, Motivation: 84}
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
				Vitals:       NPCVitals{Energy: 72, Stress: 24, Motivation: 81},
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
