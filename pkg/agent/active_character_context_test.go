package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
)

func TestBuildActiveCharacterContextIncludesStateAndRelationship(t *testing.T) {
	workspace := setupWorkspace(t, nil)
	defer os.RemoveAll(workspace)

	store := NewNPCStateStore(workspace)
	state := defaultNPCState()
	state.Emotion = NPCEmotion{
		Name:      "playful",
		Intensity: NPCEmotionIntensityMid,
		Reason:    "I enjoyed the last joke.",
	}
	state.Location = NPCLocation{
		Area:     "harbor",
		Scene:    "boardwalk",
		Activity: "taking a slow walk",
	}
	state.People["person_sunny"] = NPCPerson{DisplayName: "Sunny"}
	state.IdentifierMap["telegram:user1"] = "person_sunny"
	state.Relationships["person_sunny"] = NPCRelationship{
		Affinity:    NPCLevelHigh,
		Trust:       NPCLevelMid,
		Familiarity: NPCLevelHigh,
		Notes:       "We keep a playful style in telegram:user1; person_sunny prefers direct practical answers.",
	}
	state.RecentEvents = []NPCRecentEvent{
		{At: "2026-05-11T20:12:00+08:00", Type: "location", Summary: "I stepped out for a quiet harbor walk."},
		{At: "2026-05-11T20:18:00+08:00", Type: "scene", Summary: "I noticed the evening lights along the water."},
		{At: "2026-05-11T20:22:00+08:00", Type: "chat", Summary: "Sunny asked about making personality more visible."},
	}
	if err := store.SaveState(state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	got := buildActiveCharacterContext(&AgentInstance{ID: "test", StateStore: store}, processOptions{
		Channel:  "telegram",
		ChatID:   "chat1",
		SenderID: "user1",
	})

	for _, want := range []string{
		"# Active Character Context",
		"Emotion: playful, mid intensity. Reason: I enjoyed the last joke.",
		"Location: harbor / boardwalk / taking a slow walk",
		"Current relationship with Sunny",
		"Affinity: high",
		"Trust: mid",
		"Familiarity: high",
		"Sunny prefers direct practical answers",
		"I noticed the evening lights along the water.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("active character context missing %q:\n%s", want, got)
		}
	}

	for _, forbidden := range []string{"telegram:user1", "person_sunny", "user1", "chat1"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("active character context leaked raw identifier %q:\n%s", forbidden, got)
		}
	}
}

func TestBuildActiveCharacterContextSkipsEmptyOrIneligibleState(t *testing.T) {
	workspace := setupWorkspace(t, nil)
	defer os.RemoveAll(workspace)

	store := NewNPCStateStore(workspace)
	if err := store.SaveState(defaultNPCState()); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	tests := []struct {
		name  string
		agent *AgentInstance
		opts  processOptions
	}{
		{
			name:  "default state",
			agent: &AgentInstance{ID: "test", StateStore: store},
			opts:  processOptions{Channel: "telegram", SenderID: "user1"},
		},
		{
			name:  "missing state store",
			agent: &AgentInstance{ID: "test"},
			opts:  processOptions{Channel: "telegram", SenderID: "user1"},
		},
		{
			name:  "empty sender",
			agent: &AgentInstance{ID: "test", StateStore: store},
			opts:  processOptions{Channel: "telegram"},
		},
		{
			name:  "internal channel",
			agent: &AgentInstance{ID: "test", StateStore: store},
			opts:  processOptions{Channel: "system", SenderID: "user1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildActiveCharacterContext(tt.agent, tt.opts); got != "" {
				t.Fatalf("buildActiveCharacterContext() = %q, want empty", got)
			}
		})
	}
}

func TestBuildActiveCharacterContextSkipsUnreadableState(t *testing.T) {
	workspace := setupWorkspace(t, nil)
	defer os.RemoveAll(workspace)

	store := &NPCStateStore{
		workspace: workspace,
		statePath: workspace,
	}
	got := buildActiveCharacterContext(&AgentInstance{ID: "test", StateStore: store}, processOptions{
		Channel:  "telegram",
		SenderID: "user1",
	})
	if got != "" {
		t.Fatalf("buildActiveCharacterContext() = %q, want empty", got)
	}
}

func TestInjectActiveCharacterContextAppendsSystemBlockOnly(t *testing.T) {
	messages := []providers.Message{
		{
			Role:    "system",
			Content: "base prompt",
			SystemParts: []providers.ContentBlock{
				{Type: "text", Text: "base prompt"},
			},
		},
		{Role: "user", Content: "hello"},
	}

	got := injectActiveCharacterContext(messages, "# Active Character Context\n\nprivate")
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if !strings.Contains(got[0].Content, "# Active Character Context") {
		t.Fatalf("system content missing active context: %q", got[0].Content)
	}
	if got[1].Content != "hello" {
		t.Fatalf("user message changed: %q", got[1].Content)
	}
	if len(got[0].SystemParts) != 2 {
		t.Fatalf("SystemParts len = %d, want 2", len(got[0].SystemParts))
	}
	if got[0].SystemParts[1].Text != "# Active Character Context\n\nprivate" {
		t.Fatalf("unexpected active context system part: %q", got[0].SystemParts[1].Text)
	}
}
