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
)

type proactiveCaptureProvider struct {
	mode          string
	toolContent   string
	directContent string
	calls         [][]providers.Message
}

func (p *proactiveCaptureProvider) Chat(
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

	if strings.Contains(current, "# Proactive Outreach Check") && !hasToolResult {
		switch p.mode {
		case "direct":
			content := p.directContent
			if strings.TrimSpace(content) == "" {
				content = "hey there direct"
			}
			return &providers.LLMResponse{Content: content}, nil
		default:
			content := p.toolContent
			if strings.TrimSpace(content) == "" {
				content = "hey there"
			}
			return &providers.LLMResponse{
				ToolCalls: []providers.ToolCall{{
					ID:        "call-proactive-1",
					Name:      "message",
					Arguments: map[string]any{"content": content},
				}},
			}, nil
		}
	}

	return &providers.LLMResponse{Content: proactiveNoopToken}, nil
}

func (p *proactiveCaptureProvider) GetDefaultModel() string {
	return "mock-model"
}

func newProactiveHeartbeatLoop(
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
	al := NewAgentLoop(cfg, msgBus, provider)
	agent := al.registry.GetDefaultAgent()
	if agent == nil {
		t.Fatal("default agent is nil")
	}
	return al, agent, msgBus
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
	sessionKey := "agent:main:telegram:direct:user1"

	if err := prepareRelationshipTarget(agent, msg, sessionKey); err != nil {
		t.Fatalf("prepareRelationshipTarget() error: %v", err)
	}
	if err := recordNPCOutboundMessage(agent, "telegram", "chat1"); err != nil {
		t.Fatalf("recordNPCOutboundMessage() error: %v", err)
	}

	state, err := agent.StateStore.LoadState()
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	_, rel := requireRelationshipForIdentifier(t, state, "telegram", "user1")
	if rel.LastChannel != "telegram" || rel.LastChatID != "chat1" {
		t.Fatalf("unexpected relationship target: %+v", rel)
	}
	if rel.LastSessionKey != sessionKey {
		t.Fatalf("LastSessionKey = %q, want %q", rel.LastSessionKey, sessionKey)
	}
	if rel.LastUserMessageAt == "" {
		t.Fatal("expected LastUserMessageAt to be recorded")
	}
	if rel.LastAgentMessageAt == "" {
		t.Fatal("expected LastAgentMessageAt to be recorded")
	}
}

func TestProactiveContextSessionKey_UsesRoutedPeerSession(t *testing.T) {
	cfg := &config.Config{Session: config.SessionConfig{DMScope: "per-channel-peer"}}
	state := defaultNPCState()
	state.IdentifierMap["telegram:user1"] = "person_user1"
	state.People["person_user1"] = NPCPerson{DisplayName: "User 1"}
	rel := NPCRelationship{
		LastChannel:  "telegram",
		LastChatID:   "chat1",
		LastPeerKind: "direct",
	}

	got := proactiveContextSessionKey(cfg, state, "main", "person_user1", rel)
	want := strings.ToLower(routing.BuildAgentPeerSessionKey(routing.SessionKeyParams{
		AgentID: "main",
		Channel: "telegram",
		Peer:    &routing.RoutePeer{Kind: "direct", ID: "user1"},
		DMScope: routing.DMScopePerChannelPeer,
	}))
	if got != want {
		t.Fatalf("proactiveContextSessionKey() = %q, want %q", got, want)
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

	rel.LastProactiveSuccessAt = ""
	rel.LastProactiveAttemptAt = now.Add(-10 * time.Minute).Format(time.RFC3339)
	eval = evaluateProactiveOpportunity(rel, cfg, 30*time.Minute, now, 0.0)
	if eval.Ready || eval.Triggered {
		t.Fatalf("expected recent attempt to suppress proactive outreach, got %+v", eval)
	}
}

func TestRunProactiveHeartbeat_UsesRoutedHistoryAndMirrorsMessageToolOutput(t *testing.T) {
	provider := &proactiveCaptureProvider{mode: "tool"}
	al, agent, msgBus := newProactiveHeartbeatLoop(t, provider)

	routedSessionKey := strings.ToLower(routing.BuildAgentPeerSessionKey(routing.SessionKeyParams{
		AgentID: agent.ID,
		Channel: "telegram",
		Peer:    &routing.RoutePeer{Kind: "direct", ID: "user1"},
		DMScope: routing.DMScopePerChannelPeer,
	}))
	agent.Sessions.GetOrCreate(routedSessionKey)
	agent.Sessions.AddMessage(routedSessionKey, "user", "remember the green mug")
	agent.Sessions.AddMessage(routedSessionKey, "assistant", "I remember the green mug")
	agent.Sessions.SetSummary(routedSessionKey, "User likes contextual follow-ups.")

	state := defaultNPCState()
	state.Relationships = map[string]NPCRelationship{
		"telegram:user1": {
			Affinity:          NPCLevelHigh,
			Trust:             NPCLevelHigh,
			Familiarity:       NPCLevelHigh,
			LastChannel:       "telegram",
			LastChatID:        "chat1",
			LastPeerKind:      "direct",
			LastSessionKey:    routedSessionKey,
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
	if msg.Content != "hey there" {
		t.Fatalf("unexpected outbound content: %q", msg.Content)
	}

	if len(provider.calls) == 0 {
		t.Fatal("expected provider to receive proactive context")
	}
	firstCall := provider.calls[0]
	if len(firstCall) < 4 {
		t.Fatalf("expected system + routed history + proactive prompt, got %d messages", len(firstCall))
	}
	if !strings.Contains(firstCall[0].Content, "User likes contextual follow-ups.") {
		t.Fatalf("system prompt missing routed session summary:\n%s", firstCall[0].Content)
	}
	foundHistoryUser := false
	foundHistoryAssistant := false
	for _, message := range firstCall {
		if message.Role == "user" && message.Content == "remember the green mug" {
			foundHistoryUser = true
		}
		if message.Role == "assistant" && message.Content == "I remember the green mug" {
			foundHistoryAssistant = true
		}
	}
	if !foundHistoryUser || !foundHistoryAssistant {
		t.Fatalf("proactive call did not include routed history: %+v", firstCall)
	}
	if !strings.Contains(firstCall[len(firstCall)-1].Content, "# Proactive Outreach Check") {
		t.Fatalf("last proactive input missing proactive prompt: %q", firstCall[len(firstCall)-1].Content)
	}
	if !strings.Contains(firstCall[len(firstCall)-1].Content, "Current time:") {
		t.Fatalf("proactive prompt missing current time: %q", firstCall[len(firstCall)-1].Content)
	}
	if !strings.Contains(firstCall[len(firstCall)-1].Content, "Last user message at:") {
		t.Fatalf("proactive prompt missing user timestamp: %q", firstCall[len(firstCall)-1].Content)
	}
	if !strings.Contains(firstCall[len(firstCall)-1].Content, "Latest user turn preview: remember the green mug") {
		t.Fatalf("proactive prompt missing latest user preview: %q", firstCall[len(firstCall)-1].Content)
	}
	if !strings.Contains(firstCall[len(firstCall)-1].Content, "Latest assistant turn preview: I remember the green mug") {
		t.Fatalf("proactive prompt missing latest assistant preview: %q", firstCall[len(firstCall)-1].Content)
	}

	routedHistory := agent.Sessions.GetHistory(routedSessionKey)
	if len(routedHistory) != 3 {
		t.Fatalf("expected routed session to keep prior history plus proactive message, got %+v", routedHistory)
	}
	if routedHistory[len(routedHistory)-1].Role != "assistant" || routedHistory[len(routedHistory)-1].Content != "hey there" {
		t.Fatalf("unexpected mirrored routed history tail: %+v", routedHistory[len(routedHistory)-1])
	}
	for _, historyMsg := range routedHistory {
		if strings.Contains(historyMsg.Content, "# Proactive Outreach Check") {
			t.Fatalf("synthetic proactive prompt leaked into routed history: %+v", routedHistory)
		}
	}

	savedState, err := agent.StateStore.LoadState()
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	personRef := personRefForIdentifier(savedState, "telegram", "user1")
	if got := agent.Sessions.GetHistory(proactiveSessionKey(agent.ID, personRef)); len(got) != 0 {
		t.Fatalf("expected proactive internal session to stay ephemeral, got %+v", got)
	}

	_, rel := requireRelationshipForIdentifier(t, savedState, "telegram", "user1")
	if rel.LastProactiveSuccessAt == "" {
		t.Fatal("expected LastProactiveSuccessAt to be recorded")
	}
	if rel.LastAgentMessageAt == "" {
		t.Fatal("expected LastAgentMessageAt to be recorded")
	}
}

func TestRunProactiveHeartbeat_MirrorsDirectResponseToRoutedSession(t *testing.T) {
	provider := &proactiveCaptureProvider{mode: "direct"}
	al, agent, msgBus := newProactiveHeartbeatLoop(t, provider)

	routedSessionKey := strings.ToLower(routing.BuildAgentPeerSessionKey(routing.SessionKeyParams{
		AgentID: agent.ID,
		Channel: "telegram",
		Peer:    &routing.RoutePeer{Kind: "direct", ID: "user1"},
		DMScope: routing.DMScopePerChannelPeer,
	}))
	agent.Sessions.GetOrCreate(routedSessionKey)
	agent.Sessions.AddMessage(routedSessionKey, "user", "earlier context")

	state := defaultNPCState()
	state.Relationships = map[string]NPCRelationship{
		"telegram:user1": {
			Affinity:          NPCLevelHigh,
			Trust:             NPCLevelHigh,
			Familiarity:       NPCLevelHigh,
			LastChannel:       "telegram",
			LastChatID:        "chat1",
			LastPeerKind:      "direct",
			LastSessionKey:    routedSessionKey,
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
	if msg.Content != "hey there direct" {
		t.Fatalf("unexpected direct proactive content: %q", msg.Content)
	}

	routedHistory := agent.Sessions.GetHistory(routedSessionKey)
	if len(routedHistory) != 2 {
		t.Fatalf("expected prior history plus mirrored direct proactive message, got %+v", routedHistory)
	}
	if routedHistory[1].Role != "assistant" || routedHistory[1].Content != "hey there direct" {
		t.Fatalf("unexpected routed history after direct proactive reply: %+v", routedHistory)
	}
	for _, historyMsg := range routedHistory {
		if strings.Contains(historyMsg.Content, "# Proactive Outreach Check") {
			t.Fatalf("synthetic proactive prompt leaked into routed history: %+v", routedHistory)
		}
	}
}

func TestBuildProactivePrompt_IncludesAbsoluteTimesAndRecentTurnContext(t *testing.T) {
	now := time.Date(2026, 3, 20, 12, 31, 3, 0, time.FixedZone("HKT", 8*60*60))
	rel := NPCRelationship{
		LastChannel:            "whatsapp_native",
		LastChatID:             "130184887930990@lid",
		LastPeerKind:           "direct",
		LastUserMessageAt:      "2026-03-20T04:00:00Z",
		LastAgentMessageAt:     "2026-03-20T04:10:00Z",
		LastProactiveSuccessAt: "2026-03-20T03:00:00Z",
	}
	eval := proactiveEvaluation{
		Silence:     3*time.Hour + 15*time.Minute,
		Tolerance:   2 * time.Hour,
		Probability: 0.45,
	}
	snapshot := proactiveSessionSnapshot{
		LatestRole:      "user",
		LatestUser:      "我隔離位個同事係咁咳",
		LatestAssistant: "早兩日你都講過佢勁煩",
	}

	prompt := buildProactivePrompt("person_sunny", rel, eval, now, snapshot)

	if !strings.Contains(prompt, "2026-03-20T12:31:03+08:00") {
		t.Fatalf("prompt missing absolute current timestamp: %q", prompt)
	}
	if !strings.Contains(prompt, "Last user message at: 2026-03-20T04:00:00Z") {
		t.Fatalf("prompt missing last user timestamp: %q", prompt)
	}
	if !strings.Contains(prompt, "Latest user turn preview: 我隔離位個同事係咁咳") {
		t.Fatalf("prompt missing latest user preview: %q", prompt)
	}
	if !strings.Contains(prompt, "Recalled memory can be stale.") {
		t.Fatalf("prompt missing stale memory guidance: %q", prompt)
	}
}

func TestProactiveAutoRecallQueryFromHistory_RequiresUserLatest(t *testing.T) {
	history := []providers.Message{
		{Role: "user", Content: "earlier complaint"},
		{Role: "assistant", Content: "latest proactive follow-up"},
	}
	if got := proactiveAutoRecallQueryFromHistory(history); got != "" {
		t.Fatalf("proactiveAutoRecallQueryFromHistory() = %q, want empty", got)
	}

	history = append(history, providers.Message{Role: "user", Content: "new update from user"})
	if got := proactiveAutoRecallQueryFromHistory(history); got != "new update from user" {
		t.Fatalf("proactiveAutoRecallQueryFromHistory() = %q, want latest user turn", got)
	}
}

func TestRunProactiveHeartbeat_DedupesSameTargetAcrossRelationships(t *testing.T) {
	provider := &proactiveCaptureProvider{mode: "tool"}
	al, agent, msgBus := newProactiveHeartbeatLoop(t, provider)

	routedSessionKey := strings.ToLower(routing.BuildAgentPeerSessionKey(routing.SessionKeyParams{
		AgentID: agent.ID,
		Channel: "whatsapp_native",
		Peer:    &routing.RoutePeer{Kind: "direct", ID: "130184887930990:59@lid"},
		DMScope: routing.DMScopePerChannelPeer,
	}))
	agent.Sessions.GetOrCreate(routedSessionKey)
	agent.Sessions.AddMessage(routedSessionKey, "user", "latest unresolved thing")

	state := defaultNPCState()
	state.Relationships = map[string]NPCRelationship{
		"whatsapp_native:user-a": {
			Affinity:          NPCLevelHigh,
			Trust:             NPCLevelHigh,
			Familiarity:       NPCLevelHigh,
			LastChannel:       "whatsapp_native",
			LastChatID:        "130184887930990@lid",
			LastPeerKind:      "direct",
			LastSessionKey:    routedSessionKey,
			LastUserMessageAt: time.Now().Add(-6 * time.Hour).UTC().Format(time.RFC3339),
		},
		"whatsapp_native:user-b": {
			Affinity:          NPCLevelHigh,
			Trust:             NPCLevelHigh,
			Familiarity:       NPCLevelHigh,
			LastChannel:       "whatsapp_native",
			LastChatID:        "130184887930990@lid",
			LastPeerKind:      "direct",
			LastSessionKey:    routedSessionKey,
			LastUserMessageAt: time.Now().Add(-6 * time.Hour).UTC().Format(time.RFC3339),
		},
	}
	if err := agent.StateStore.SaveState(state); err != nil {
		t.Fatalf("SaveState() error: %v", err)
	}

	al.RunProactiveHeartbeat(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, ok := msgBus.SubscribeOutbound(ctx); !ok {
		t.Fatal("expected one proactive outbound message")
	}

	ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if msg, ok := msgBus.SubscribeOutbound(ctx); ok {
		t.Fatalf("expected only one proactive outbound message, got %+v", msg)
	}
	proactivePromptCalls := 0
	for _, call := range provider.calls {
		if len(call) == 0 {
			continue
		}
		if strings.Contains(call[len(call)-1].Content, "# Proactive Outreach Check") {
			proactivePromptCalls++
		}
	}
	if proactivePromptCalls != 1 {
		t.Fatalf("proactive prompt call count = %d, want 1", proactivePromptCalls)
	}
}

func TestRunProactiveHeartbeat_SuppressesDuplicateContentWhenAssistantLatest(t *testing.T) {
	provider := &proactiveCaptureProvider{mode: "direct", directContent: "hey there"}
	al, agent, msgBus := newProactiveHeartbeatLoop(t, provider)

	routedSessionKey := strings.ToLower(routing.BuildAgentPeerSessionKey(routing.SessionKeyParams{
		AgentID: agent.ID,
		Channel: "telegram",
		Peer:    &routing.RoutePeer{Kind: "direct", ID: "user1"},
		DMScope: routing.DMScopePerChannelPeer,
	}))
	agent.Sessions.GetOrCreate(routedSessionKey)
	agent.Sessions.AddMessage(routedSessionKey, "user", "earlier context")
	agent.Sessions.AddMessage(routedSessionKey, "assistant", "hey there")

	state := defaultNPCState()
	state.Relationships = map[string]NPCRelationship{
		"telegram:user1": {
			Affinity:          NPCLevelHigh,
			Trust:             NPCLevelHigh,
			Familiarity:       NPCLevelHigh,
			LastChannel:       "telegram",
			LastChatID:        "chat1",
			LastPeerKind:      "direct",
			LastSessionKey:    routedSessionKey,
			LastUserMessageAt: time.Now().Add(-6 * time.Hour).UTC().Format(time.RFC3339),
		},
	}
	if err := agent.StateStore.SaveState(state); err != nil {
		t.Fatalf("SaveState() error: %v", err)
	}

	al.RunProactiveHeartbeat(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if msg, ok := msgBus.SubscribeOutbound(ctx); ok {
		t.Fatalf("expected duplicate proactive content to be suppressed, got %+v", msg)
	}

	routedHistory := agent.Sessions.GetHistory(routedSessionKey)
	if len(routedHistory) != 2 {
		t.Fatalf("expected routed history to stay unchanged, got %+v", routedHistory)
	}
}
