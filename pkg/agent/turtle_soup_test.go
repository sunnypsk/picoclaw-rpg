package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/gamemode/turtlesoup"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/routing"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type turtleSoupProvider struct {
	agentResponses    []providers.LLMResponse
	generatorResponse string
	judgeResponse     string
	agentCalls        [][]providers.Message
	generatorCalls    [][]providers.Message
	judgeCalls        [][]providers.Message
	judgeModels       []string
	toolNames         [][]string
}

func (p *turtleSoupProvider) Chat(
	_ context.Context,
	messages []providers.Message,
	toolDefs []providers.ToolDefinition,
	model string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	cloned := make([]providers.Message, len(messages))
	copy(cloned, messages)
	if len(messages) >= 2 && strings.Contains(messages[0].Content, "internal judge for a turtle soup") {
		p.judgeCalls = append(p.judgeCalls, cloned)
		p.judgeModels = append(p.judgeModels, model)
		response := p.judgeResponse
		if response == "" {
			response = `{"kind":"question","label":"cannot_answer"}`
		}
		return &providers.LLMResponse{Content: response, FinishReason: "stop"}, nil
	}
	if len(messages) >= 2 && strings.Contains(messages[0].Content, "create original turtle soup") {
		p.generatorCalls = append(p.generatorCalls, cloned)
		response := p.generatorResponse
		if response == "" {
			response = `{"surface":"surface text","solution":"hidden answer","hints":["first hint","second hint","third hint"]}`
		}
		return &providers.LLMResponse{Content: response, FinishReason: "stop"}, nil
	}

	names := make([]string, 0, len(toolDefs))
	for _, tool := range toolDefs {
		names = append(names, tool.Function.Name)
	}
	p.toolNames = append(p.toolNames, names)
	p.agentCalls = append(p.agentCalls, cloned)
	if len(p.agentResponses) == 0 {
		return &providers.LLMResponse{Content: "ok", FinishReason: "stop"}, nil
	}
	response := p.agentResponses[0]
	p.agentResponses = p.agentResponses[1:]
	return &response, nil
}

func (p *turtleSoupProvider) GetDefaultModel() string {
	return "mock-model"
}

func newTurtleSoupTestLoop(t *testing.T, provider *turtleSoupProvider) (*AgentLoop, *config.Config) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         workspace,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
		Session: config.SessionConfig{DMScope: "per-channel-peer"},
	}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	installTestTurtleSoup(t, al, root)
	return al, cfg
}

func installTestTurtleSoup(t *testing.T, al *AgentLoop, root string) {
	t.Helper()
	engine := turtlesoup.NewEngine(
		turtlesoup.NewStore(filepath.Join(root, "games", "turtle_soup")),
		[]turtlesoup.Puzzle{{
			ID:       "test",
			Surface:  "surface text",
			Solution: "hidden answer",
			Hints:    []string{"first hint"},
		}},
	)
	al.turtleSoup = engine
	for _, agentID := range al.registry.ListAgentIDs() {
		agent, ok := al.registry.GetAgent(agentID)
		if !ok {
			continue
		}
		agentRef := agent
		agent.Tools.Register(tools.NewTurtleSoupToolWithModelResolver(engine, agent.Provider, func() string {
			return agentRef.Model
		}))
	}
}

func turtleSoupToolResponse(action, message string) providers.LLMResponse {
	args := map[string]any{"action": action}
	if message != "" {
		args["message"] = message
	}
	return providers.LLMResponse{
		ToolCalls: []providers.ToolCall{{
			ID:        "call-turtle-soup",
			Name:      "turtle_soup",
			Arguments: args,
		}},
		FinishReason: "tool_calls",
	}
}

func TestTurtleSoupStartUsesAgentToolAndStoresVisibleHistoryOnly(t *testing.T) {
	provider := &turtleSoupProvider{
		agentResponses: []providers.LLMResponse{
			turtleSoupToolResponse("start", ""),
			{Content: "game started", FinishReason: "stop"},
		},
	}
	al, _ := newTurtleSoupTestLoop(t, provider)
	msg := bus.InboundMessage{
		Channel:  "telegram",
		ChatID:   "chat-1",
		SenderID: "user-1",
		Content:  "play 海龜湯",
		Peer:     bus.Peer{Kind: "direct", ID: "user-1"},
	}

	response, agent, err := al.processMessageCore(context.Background(), msg, false)
	if err != nil {
		t.Fatalf("processMessageCore(start) error = %v", err)
	}
	if response != "game started" {
		t.Fatalf("response = %q, want tool-driven final response", response)
	}
	if len(provider.agentCalls) != 2 {
		t.Fatalf("agent calls = %d, want 2", len(provider.agentCalls))
	}
	if !hasTool(provider.toolNames[0], "turtle_soup") {
		t.Fatalf("turtle_soup tool was not offered to agent: %v", provider.toolNames[0])
	}
	if len(provider.judgeCalls) != 0 {
		t.Fatalf("start should not call judge, got %d calls", len(provider.judgeCalls))
	}

	history := agent.Sessions.GetHistory("agent:main:telegram:direct:user-1")
	joined := joinHistory(history)
	if !strings.Contains(joined, "surface text") || !strings.Contains(joined, "TS-") {
		t.Fatalf("session should contain visible tool result with surface/code, got %s", joined)
	}
	if strings.Contains(joined, "hidden answer") {
		t.Fatalf("visible session history leaked hidden solution: %s", joined)
	}
}

func TestTurtleSoupActiveGameDoesNotCaptureUnrelatedMessage(t *testing.T) {
	provider := &turtleSoupProvider{
		agentResponses: []providers.LLMResponse{{Content: "normal answer", FinishReason: "stop"}},
	}
	al, _ := newTurtleSoupTestLoop(t, provider)
	sessionKey := "agent:main:telegram:direct:user-1"
	if _, err := al.turtleSoup.Start(sessionKey); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	response, _, err := al.processMessageCore(context.Background(), bus.InboundMessage{
		Channel:  "telegram",
		ChatID:   "chat-1",
		SenderID: "user-1",
		Content:  "what is the weather?",
		Peer:     bus.Peer{Kind: "direct", ID: "user-1"},
	}, false)
	if err != nil {
		t.Fatalf("processMessageCore(unrelated) error = %v", err)
	}
	if response != "normal answer" {
		t.Fatalf("unrelated response = %q, want normal answer", response)
	}
	if len(provider.judgeCalls) != 0 {
		t.Fatalf("unrelated message should not be captured by turtle soup judge, got %d judge calls", len(provider.judgeCalls))
	}
}

func TestTurtleSoupToolTurnUsesJudge(t *testing.T) {
	provider := &turtleSoupProvider{
		agentResponses: []providers.LLMResponse{
			turtleSoupToolResponse("turn", "is it about food?"),
			{Content: "是", FinishReason: "stop"},
		},
		judgeResponse: `{"kind":"question","label":"yes"}`,
	}
	al, _ := newTurtleSoupTestLoop(t, provider)
	sessionKey := "agent:main:telegram:direct:user-1"
	if _, err := al.turtleSoup.Start(sessionKey); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	agent := al.registry.GetDefaultAgent()
	if agent == nil {
		t.Fatal("expected default agent")
	}
	agent.Model = "new-model"

	response, _, err := al.processMessageCore(context.Background(), bus.InboundMessage{
		Channel:  "telegram",
		ChatID:   "chat-1",
		SenderID: "user-1",
		Content:  "is it about food?",
		Peer:     bus.Peer{Kind: "direct", ID: "user-1"},
	}, false)
	if err != nil {
		t.Fatalf("processMessageCore(turn) error = %v", err)
	}
	if response != "是" {
		t.Fatalf("turn response = %q, want 是", response)
	}
	if len(provider.judgeCalls) != 1 {
		t.Fatalf("judge calls = %d, want 1", len(provider.judgeCalls))
	}
	if len(provider.judgeModels) != 1 || provider.judgeModels[0] != "new-model" {
		t.Fatalf("judge models = %v, want [new-model]", provider.judgeModels)
	}
	payload := provider.judgeCalls[0][1].Content
	if !strings.Contains(payload, "hidden_solution") || !strings.Contains(payload, "hidden answer") {
		t.Fatalf("judge payload should include hidden solution: %s", payload)
	}
	if !strings.Contains(payload, "is it about food?") {
		t.Fatalf("judge payload should include player message: %s", payload)
	}
}

func TestTurtleSoupToolRegisteredForAutoProvisionedAgent(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         t.TempDir(),
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
			AutoProvision: config.AutoProvisionConfig{
				Enabled: true,
			},
		},
	}
	provider := &turtleSoupProvider{
		agentResponses: []providers.LLMResponse{{Content: "ok", FinishReason: "stop"}},
	}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	route := al.registry.ResolveRoute(routing.RouteInput{
		Channel: "telegram",
		Peer:    &routing.RoutePeer{Kind: "direct", ID: "user42"},
	})
	if route.MatchedBy != "auto-provision" {
		t.Fatalf("MatchedBy = %q, want auto-provision", route.MatchedBy)
	}

	_, _, err := al.processMessageCore(context.Background(), bus.InboundMessage{
		Channel:  "telegram",
		ChatID:   "chat42",
		SenderID: "user42",
		Content:  "hello",
		Peer:     bus.Peer{Kind: "direct", ID: "user42"},
	}, false)
	if err != nil {
		t.Fatalf("processMessageCore(auto-provision) error = %v", err)
	}
	agent, ok := al.registry.GetAgent(route.AgentID)
	if !ok {
		t.Fatalf("expected auto-provisioned agent %q", route.AgentID)
	}
	if _, ok := agent.Tools.Get("turtle_soup"); !ok {
		t.Fatalf("expected turtle_soup tool to be registered on auto-provisioned agent")
	}
}

func hasTool(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func joinHistory(history []providers.Message) string {
	var b strings.Builder
	for _, msg := range history {
		b.WriteString(msg.Content)
		b.WriteByte('\n')
	}
	return b.String()
}
