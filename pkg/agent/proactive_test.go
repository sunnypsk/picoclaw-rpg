package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type proactiveTestProvider struct{}

func (p *proactiveTestProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	hasToolResult := false
	for _, msg := range messages {
		if msg.Role == "tool" {
			hasToolResult = true
			break
		}
	}
	current := ""
	if len(messages) > 0 {
		current = messages[len(messages)-1].Content
	}
	if strings.Contains(current, "# Proactive Outreach Check") && !hasToolResult {
		return &providers.LLMResponse{
			ToolCalls: []providers.ToolCall{{
				ID:        "call-proactive-1",
				Name:      "message",
				Arguments: map[string]any{"content": "hey there"},
			}},
		}, nil
	}
	return &providers.LLMResponse{Content: proactiveNoopToken}, nil
}

func (p *proactiveTestProvider) GetDefaultModel() string {
	return "mock-model"
}

func TestPrepareRelationshipTargetAndRecordOutboundMessage(t *testing.T) {
	al, _, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()

	agent := al.registry.GetDefaultAgent()
	msg := bus.InboundMessage{
		Channel:  "telegram",
		SenderID: "user1",
		ChatID:   "chat1",
		Peer:     bus.Peer{Kind: "direct", ID: "user1"},
	}

	if err := prepareRelationshipTarget(agent, msg); err != nil {
		t.Fatalf("prepareRelationshipTarget() error: %v", err)
	}
	if err := recordNPCOutboundMessage(agent, "telegram", "chat1"); err != nil {
		t.Fatalf("recordNPCOutboundMessage() error: %v", err)
	}

	state, err := agent.StateStore.LoadState()
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	rel := state.Relationships["telegram:user1"]
	if rel.LastChannel != "telegram" || rel.LastChatID != "chat1" {
		t.Fatalf("unexpected relationship target: %+v", rel)
	}
	if rel.LastUserMessageAt == "" {
		t.Fatal("expected LastUserMessageAt to be recorded")
	}
	if rel.LastAgentMessageAt == "" {
		t.Fatal("expected LastAgentMessageAt to be recorded")
	}
}

func TestEffectiveProactiveTolerance_StrongerRelationshipLowersTolerance(t *testing.T) {
	cfg := normalizeHeartbeatProactiveConfig(config.HeartbeatProactiveConfig{
		BaseToleranceMinutes:    240,
		MinToleranceMinutes:     60,
		RelationshipStepMinutes: 30,
	})
	weak := NPCRelationship{Affinity: NPCLevelLow, Trust: NPCLevelLow, Familiarity: NPCLevelLow}
	strong := NPCRelationship{Affinity: NPCLevelHigh, Trust: NPCLevelHigh, Familiarity: NPCLevelHigh}

	weakTolerance := effectiveProactiveTolerance(cfg, weak)
	strongTolerance := effectiveProactiveTolerance(cfg, strong)

	if strongTolerance >= weakTolerance {
		t.Fatalf("expected strong relationship tolerance %s to be lower than weak %s", strongTolerance, weakTolerance)
	}
}

func TestEvaluateProactiveOpportunity_RampsAndCooldown(t *testing.T) {
	now := time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC)
	cfg := normalizeHeartbeatProactiveConfig(config.HeartbeatProactiveConfig{
		BaseToleranceMinutes:        240,
		MinToleranceMinutes:         60,
		RelationshipStepMinutes:     30,
		InitialProbability:          0.2,
		ProbabilityRampPerHeartbeat: 0.1,
		MaxProbability:              0.5,
		CooldownMinutes:             360,
	})
	rel := NPCRelationship{
		LastChannel:       "telegram",
		LastChatID:        "chat1",
		LastUserMessageAt: now.Add(-5 * time.Hour).Format(time.RFC3339),
	}

	eval := evaluateProactiveOpportunity(rel, cfg, 30*time.Minute, now, 0.39)
	if !eval.Ready || !eval.Triggered {
		t.Fatalf("expected proactive evaluation to trigger, got %+v", eval)
	}
	if eval.Probability != 0.4 {
		t.Fatalf("Probability = %.2f, want 0.40", eval.Probability)
	}

	rel.LastProactiveSuccessAt = now.Add(-1 * time.Hour).Format(time.RFC3339)
	eval = evaluateProactiveOpportunity(rel, cfg, 30*time.Minute, now, 0.0)
	if eval.Ready || eval.Triggered {
		t.Fatalf("expected cooldown to suppress proactive outreach, got %+v", eval)
	}
}

func TestRunProactiveHeartbeat_SendsMessageAndRecordsSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
		Heartbeat: config.HeartbeatConfig{
			Enabled:  true,
			Interval: 5,
			Proactive: config.HeartbeatProactiveConfig{
				Enabled:                     true,
				BaseToleranceMinutes:        240,
				MinToleranceMinutes:         60,
				RelationshipStepMinutes:     30,
				InitialProbability:          1,
				ProbabilityRampPerHeartbeat: 0,
				MaxProbability:              1,
				CooldownMinutes:             360,
			},
		},
	}
	msgBus := bus.NewMessageBus()
	al := NewAgentLoop(cfg, msgBus, &proactiveTestProvider{})
	agent := al.registry.GetDefaultAgent()

	state := defaultNPCState()
	state.Relationships = map[string]NPCRelationship{
		"telegram:user1": {
			Affinity:          NPCLevelHigh,
			Trust:             NPCLevelHigh,
			Familiarity:       NPCLevelHigh,
			LastChannel:       "telegram",
			LastChatID:        "chat1",
			LastPeerKind:      "direct",
			LastUserMessageAt: time.Now().Add(-6 * time.Hour).UTC().Format(time.RFC3339),
		},
	}
	if err := agent.StateStore.SaveState(state); err != nil {
		t.Fatalf("SaveState() error: %v", err)
	}

	al.RunProactiveHeartbeat(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, ok := msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected proactive outbound message")
	}
	if msg.Channel != "telegram" || msg.ChatID != "chat1" {
		t.Fatalf("unexpected outbound target: %+v", msg)
	}
	if !strings.Contains(msg.Content, "hey there") {
		t.Fatalf("unexpected outbound content: %q", msg.Content)
	}

	updated, err := agent.StateStore.LoadState()
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	rel := updated.Relationships["telegram:user1"]
	if rel.LastProactiveSuccessAt == "" {
		t.Fatal("expected LastProactiveSuccessAt to be recorded")
	}
	if rel.LastAgentMessageAt == "" {
		t.Fatal("expected LastAgentMessageAt to be recorded")
	}
}
