package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/routing"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type scheduledReminderProvider struct {
	mode  string
	calls [][]providers.Message
}

func (p *scheduledReminderProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	p.calls = append(p.calls, append([]providers.Message(nil), messages...))

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

	if strings.TrimSpace(current) == "stretch" && !hasToolResult {
		switch p.mode {
		case "direct":
			return &providers.LLMResponse{Content: "time to stretch direct"}, nil
		default:
			return &providers.LLMResponse{
				ToolCalls: []providers.ToolCall{{
					ID:        "call-scheduled-reminder-1",
					Name:      "message",
					Arguments: map[string]any{"content": "time to stretch"},
				}},
			}, nil
		}
	}

	return &providers.LLMResponse{Content: "scheduled reminder complete"}, nil
}

func (p *scheduledReminderProvider) GetDefaultModel() string {
	return "mock-model"
}

func newScheduledReminderLoop(
	t *testing.T,
	provider providers.LLMProvider,
) (*AgentLoop, *AgentInstance, *bus.MessageBus) {
	t.Helper()

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
		Session: config.SessionConfig{
			DMScope: "per-channel-peer",
		},
	}

	msgBus := bus.NewMessageBus()
	al := NewAgentLoop(cfg, msgBus, provider)
	agent := al.registry.GetDefaultAgent()
	if agent == nil {
		t.Fatal("default agent is nil")
	}
	return al, agent, msgBus
}

func routedReminderSessionKey(agentID string) string {
	return strings.ToLower(routing.BuildAgentPeerSessionKey(routing.SessionKeyParams{
		AgentID: agentID,
		Channel: "telegram",
		Peer:    &routing.RoutePeer{Kind: "direct", ID: "user1"},
		DMScope: routing.DMScopePerChannelPeer,
	}))
}

func TestProcessScheduledReminder_DirectDeliveryMirrorsToRoutedSession(t *testing.T) {
	al, agent, msgBus := newScheduledReminderLoop(t, &mockProvider{})

	routedSessionKey := routedReminderSessionKey(agent.ID)
	agent.Sessions.GetOrCreate(routedSessionKey)
	agent.Sessions.AddMessage(routedSessionKey, "user", "remember the mug")

	_, err := al.ProcessScheduledReminder(context.Background(), tools.ScheduledReminderRequest{
		JobID:      "job-direct",
		Content:    "stretch now",
		Channel:    "telegram",
		ChatID:     "chat1",
		SessionKey: routedSessionKey,
		Deliver:    true,
	})
	if err != nil {
		t.Fatalf("ProcessScheduledReminder() error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, ok := msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected outbound reminder")
	}
	if msg.Content != "stretch now" {
		t.Fatalf("unexpected outbound content: %q", msg.Content)
	}

	history := agent.Sessions.GetHistory(routedSessionKey)
	if len(history) != 2 {
		t.Fatalf("expected routed history to contain prior context plus mirrored reminder, got %+v", history)
	}
	if history[1].Role != "assistant" || history[1].Content != "stretch now" {
		t.Fatalf("unexpected routed history tail: %+v", history[1])
	}
}

func TestProcessScheduledReminder_DirectDeliveryWithoutSessionKeyDoesNotMirror(t *testing.T) {
	al, agent, msgBus := newScheduledReminderLoop(t, &mockProvider{})

	routedSessionKey := routedReminderSessionKey(agent.ID)
	agent.Sessions.GetOrCreate(routedSessionKey)
	agent.Sessions.AddMessage(routedSessionKey, "user", "previous context")

	_, err := al.ProcessScheduledReminder(context.Background(), tools.ScheduledReminderRequest{
		JobID:   "job-no-session",
		Content: "stretch now",
		Channel: "telegram",
		ChatID:  "chat1",
		Deliver: true,
	})
	if err != nil {
		t.Fatalf("ProcessScheduledReminder() error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, ok := msgBus.SubscribeOutbound(ctx); !ok {
		t.Fatal("expected outbound reminder")
	}

	history := agent.Sessions.GetHistory(routedSessionKey)
	if len(history) != 1 {
		t.Fatalf("expected routed history to remain unchanged without session key, got %+v", history)
	}
}

func TestProcessScheduledReminder_UsesRoutedHistoryAndMirrorsMessageToolOutput(t *testing.T) {
	provider := &scheduledReminderProvider{mode: "tool"}
	al, agent, msgBus := newScheduledReminderLoop(t, provider)

	routedSessionKey := routedReminderSessionKey(agent.ID)
	agent.Sessions.GetOrCreate(routedSessionKey)
	agent.Sessions.AddMessage(routedSessionKey, "user", "remember the mug")
	agent.Sessions.AddMessage(routedSessionKey, "assistant", "I remember the mug")
	agent.Sessions.SetSummary(routedSessionKey, "User likes gentle reminders.")

	_, err := al.ProcessScheduledReminder(context.Background(), tools.ScheduledReminderRequest{
		JobID:      "job-tool",
		Content:    "stretch",
		Channel:    "telegram",
		ChatID:     "chat1",
		SessionKey: routedSessionKey,
		Deliver:    false,
	})
	if err != nil {
		t.Fatalf("ProcessScheduledReminder() error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, ok := msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected outbound reminder")
	}
	if msg.Content != "time to stretch" {
		t.Fatalf("unexpected outbound content: %q", msg.Content)
	}

	if len(provider.calls) == 0 {
		t.Fatal("expected provider to receive routed reminder context")
	}
	firstCall := provider.calls[0]
	foundHistoryUser := false
	foundHistoryAssistant := false
	for _, message := range firstCall {
		if message.Role == "user" && message.Content == "remember the mug" {
			foundHistoryUser = true
		}
		if message.Role == "assistant" && message.Content == "I remember the mug" {
			foundHistoryAssistant = true
		}
	}
	if !foundHistoryUser || !foundHistoryAssistant {
		t.Fatalf("scheduled reminder call missing routed history: %+v", firstCall)
	}
	if got := firstCall[len(firstCall)-1].Content; got != "stretch" {
		t.Fatalf("expected raw reminder content as last input, got %q", got)
	}

	routedHistory := agent.Sessions.GetHistory(routedSessionKey)
	if len(routedHistory) != 3 {
		t.Fatalf("expected routed history to keep prior context plus mirrored reminder, got %+v", routedHistory)
	}
	if routedHistory[len(routedHistory)-1].Role != "assistant" || routedHistory[len(routedHistory)-1].Content != "time to stretch" {
		t.Fatalf("unexpected routed history tail: %+v", routedHistory[len(routedHistory)-1])
	}
	for _, historyMsg := range routedHistory {
		if historyMsg.Content == "stretch" {
			t.Fatalf("scheduled reminder trigger leaked into routed history: %+v", routedHistory)
		}
	}

	if got := agent.Sessions.GetHistory(scheduledReminderSessionKey(agent.ID, "job-tool")); len(got) != 0 {
		t.Fatalf("expected scheduled reminder session to stay ephemeral, got %+v", got)
	}
}

func TestProcessScheduledReminder_DirectResponseMirrorsToRoutedSession(t *testing.T) {
	provider := &scheduledReminderProvider{mode: "direct"}
	al, agent, msgBus := newScheduledReminderLoop(t, provider)

	routedSessionKey := routedReminderSessionKey(agent.ID)
	agent.Sessions.GetOrCreate(routedSessionKey)
	agent.Sessions.AddMessage(routedSessionKey, "user", "remember the mug")

	_, err := al.ProcessScheduledReminder(context.Background(), tools.ScheduledReminderRequest{
		JobID:      "job-direct-response",
		Content:    "stretch",
		Channel:    "telegram",
		ChatID:     "chat1",
		SessionKey: routedSessionKey,
		Deliver:    false,
	})
	if err != nil {
		t.Fatalf("ProcessScheduledReminder() error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, ok := msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected outbound reminder")
	}
	if msg.Content != "time to stretch direct" {
		t.Fatalf("unexpected outbound content: %q", msg.Content)
	}

	routedHistory := agent.Sessions.GetHistory(routedSessionKey)
	if len(routedHistory) != 2 {
		t.Fatalf("expected routed history to contain prior context plus mirrored direct response, got %+v", routedHistory)
	}
	if routedHistory[1].Role != "assistant" || routedHistory[1].Content != "time to stretch direct" {
		t.Fatalf("unexpected routed history tail: %+v", routedHistory[1])
	}
}

func TestProcessScheduledReminder_UsesRotatedSessionKey(t *testing.T) {
	al, agent, msgBus := newScheduledReminderLoop(t, &mockProvider{})

	oldSessionKey := routedReminderSessionKey(agent.ID)
	newSessionKey := oldSessionKey + "-new"
	agent.Sessions.GetOrCreate(newSessionKey)
	al.sessionRotates.Store(al.sessionRotationKey(agent.ID, oldSessionKey), newSessionKey)

	_, err := al.ProcessScheduledReminder(context.Background(), tools.ScheduledReminderRequest{
		JobID:      "job-rotated",
		Content:    "stretch now",
		Channel:    "telegram",
		ChatID:     "chat1",
		SessionKey: oldSessionKey,
		Deliver:    true,
	})
	if err != nil {
		t.Fatalf("ProcessScheduledReminder() error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, ok := msgBus.SubscribeOutbound(ctx); !ok {
		t.Fatal("expected outbound reminder")
	}

	oldHistory := agent.Sessions.GetHistory(oldSessionKey)
	if len(oldHistory) != 0 {
		t.Fatalf("expected old session to stay untouched after rotation, got %+v", oldHistory)
	}
	newHistory := agent.Sessions.GetHistory(newSessionKey)
	if len(newHistory) != 1 {
		t.Fatalf("expected rotated session to receive mirrored reminder, got %+v", newHistory)
	}
	if newHistory[0].Role != "assistant" || newHistory[0].Content != "stretch now" {
		t.Fatalf("unexpected rotated session history: %+v", newHistory)
	}
}
