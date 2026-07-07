package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
)

func configureMaintenanceRoute(agent *AgentInstance, provider providers.LLMProvider) {
	agent.MaintenanceRoute = modelRoute{
		Name: "maintenance",
		Candidates: []modelRouteCandidate{{
			Alias:        "maintenance",
			ProviderName: "deepseek",
			Model:        "maintenance-model",
			Provider:     provider,
		}},
	}
}

func maintenanceRouteTestMessage() bus.InboundMessage {
	return bus.InboundMessage{
		Channel:  "telegram",
		SenderID: "user1",
		ChatID:   "chat1",
		Content:  "hello there",
		Peer:     bus.Peer{Kind: "direct", ID: "user1"},
	}
}

func TestNewAgentInstance_ConfiguresMaintenanceRouteFromModelList(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:            t.TempDir(),
				ModelName:            "text",
				MaintenanceModelName: "maintenance",
				MaxTokens:            4096,
				MaxToolIterations:    1,
			},
		},
		ModelList: []config.ModelConfig{
			{
				ModelName: "text",
				Model:     "openai/text-model",
				APIBase:   "https://text.example.test/v1",
			},
			{
				ModelName:       "maintenance",
				Model:           "deepseek/maintenance-model",
				APIBase:         "https://maintenance.example.test/v1",
				ReasoningEffort: "low",
			},
		},
	}

	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, &mockProvider{})
	primary := agent.MaintenanceRoute.primary()
	if primary.ProviderName != "deepseek" || primary.Model != "maintenance-model" {
		t.Fatalf("maintenance route = %s/%s, want deepseek/maintenance-model",
			primary.ProviderName, primary.Model)
	}
	if primary.Provider == nil {
		t.Fatal("maintenance route provider should be configured")
	}
	if primary.Reasoning["effort"] != "low" {
		t.Fatalf("maintenance reasoning effort = %v, want low", primary.Reasoning["effort"])
	}
}

func TestNewAgentInstance_DefaultMaintenanceRouteUsesTextRoute(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         t.TempDir(),
				ModelName:         "text",
				MaxTokens:         4096,
				MaxToolIterations: 1,
			},
		},
		ModelList: []config.ModelConfig{
			{
				ModelName: "text",
				Model:     "openai/text-model",
				APIBase:   "https://text.example.test/v1",
			},
		},
	}

	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, &mockProvider{})
	if !agent.MaintenanceRoute.configured() {
		t.Fatal("maintenance route should default to the text route")
	}
	if agent.MaintenanceRoute.primary().Model != agent.TextRoute.primary().Model {
		t.Fatalf("maintenance model = %q, want text model %q",
			agent.MaintenanceRoute.primary().Model, agent.TextRoute.primary().Model)
	}
}

func TestRetrySummaryCall_UsesMaintenanceRouteWhenConfigured(t *testing.T) {
	textProvider := &routeCaptureProvider{name: "text"}
	maintenanceProvider := &routeCaptureProvider{
		name:     "maintenance",
		response: &providers.LLMResponse{Content: "summary from maintenance", FinishReason: "stop"},
	}
	agent := newRouteTestAgent(textProvider, nil)
	configureMaintenanceRoute(agent, maintenanceProvider)

	got, err := newRouteTestLoop().retrySummaryCall(context.Background(), agent, "summarize this", 1)
	if err != nil {
		t.Fatalf("retrySummaryCall() error = %v", err)
	}
	if got != "summary from maintenance" {
		t.Fatalf("summary = %q, want maintenance response", got)
	}
	if len(maintenanceProvider.calls) != 1 {
		t.Fatalf("maintenance calls = %d, want 1", len(maintenanceProvider.calls))
	}
	if maintenanceProvider.calls[0].model != "maintenance-model" {
		t.Fatalf("maintenance model = %q, want maintenance-model", maintenanceProvider.calls[0].model)
	}
	if len(textProvider.calls) != 0 {
		t.Fatalf("text calls = %d, want 0", len(textProvider.calls))
	}
}

func TestRetrySummaryCall_UsesTextRouteWhenMaintenanceRouteUnset(t *testing.T) {
	textProvider := &routeCaptureProvider{
		name:     "text",
		response: &providers.LLMResponse{Content: "summary from text", FinishReason: "stop"},
	}
	agent := newRouteTestAgent(textProvider, nil)
	agent.MaintenanceRoute = modelRoute{Name: "maintenance"}

	got, err := newRouteTestLoop().retrySummaryCall(context.Background(), agent, "summarize this", 1)
	if err != nil {
		t.Fatalf("retrySummaryCall() error = %v", err)
	}
	if got != "summary from text" {
		t.Fatalf("summary = %q, want text response", got)
	}
	if len(textProvider.calls) != 1 {
		t.Fatalf("text calls = %d, want 1", len(textProvider.calls))
	}
	if textProvider.calls[0].model != "text-model" {
		t.Fatalf("text model = %q, want text-model", textProvider.calls[0].model)
	}
}

func TestAutoRecallKeywordExtraction_UsesMaintenanceRouteAndBudget(t *testing.T) {
	textProvider := &routeCaptureProvider{name: "text"}
	maintenanceProvider := &routeCaptureProvider{
		name:     "maintenance",
		response: &providers.LLMResponse{Content: `{"keywords":["hello"]}`, FinishReason: "stop"},
	}
	agent := newRouteTestAgent(textProvider, nil)
	configureMaintenanceRoute(agent, maintenanceProvider)

	got := (&AgentLoop{}).extractAutoRecallQuery(context.Background(), agent, "hello")
	if got != "hello" {
		t.Fatalf("keyword query = %q, want hello", got)
	}
	if len(maintenanceProvider.calls) != 1 {
		t.Fatalf("maintenance calls = %d, want 1", len(maintenanceProvider.calls))
	}
	call := maintenanceProvider.calls[0]
	if call.model != "maintenance-model" {
		t.Fatalf("maintenance model = %q, want maintenance-model", call.model)
	}
	if call.options["max_tokens"] != autoRecallKeywordExtractionMaxTokens {
		t.Fatalf("max_tokens = %v, want %d", call.options["max_tokens"], autoRecallKeywordExtractionMaxTokens)
	}
	if len(textProvider.calls) != 0 {
		t.Fatalf("text calls = %d, want 0", len(textProvider.calls))
	}
}

func TestNPCMaintenanceCalls_UseMaintenanceRouteAndBudgets(t *testing.T) {
	textProvider := &routeCaptureProvider{name: "text"}
	maintenanceProvider := &routeCaptureProvider{
		name: "maintenance",
		responses: []*providers.LLMResponse{
			{Content: scriptedNPCStateOnlyUpdate(), FinishReason: "stop"},
			{Content: scriptedNPCStateMemoryUpdate("durable note"), FinishReason: "stop"},
			{Content: scriptedNPCStateMemoryUpdate("repair note"), FinishReason: "stop"},
		},
	}
	agent := newRouteTestAgent(textProvider, nil)
	configureMaintenanceRoute(agent, maintenanceProvider)
	al := newRouteTestLoop()
	msg := maintenanceRouteTestMessage()
	state := defaultNPCState()

	if _, err := al.requestNPCStateOnlyUpdate(context.Background(), agent, state, msg, "session", "reply"); err != nil {
		t.Fatalf("requestNPCStateOnlyUpdate() error = %v", err)
	}
	if _, err := al.requestNPCStateUpdate(context.Background(), agent, state, nil, msg, "session", "reply"); err != nil {
		t.Fatalf("requestNPCStateUpdate() error = %v", err)
	}
	if _, err := al.requestNPCStateUpdateRepair(
		context.Background(), agent, state, nil, msg, "session", "reply",
	); err != nil {
		t.Fatalf("requestNPCStateUpdateRepair() error = %v", err)
	}

	if len(maintenanceProvider.calls) != 3 {
		t.Fatalf("maintenance calls = %d, want 3", len(maintenanceProvider.calls))
	}
	wantTokens := []int{npcStateOnlyMaxTokens, npcStateMemoryMaxTokens, npcStateMemoryMaxTokens}
	for i, call := range maintenanceProvider.calls {
		if call.model != "maintenance-model" {
			t.Fatalf("call %d model = %q, want maintenance-model", i, call.model)
		}
		if call.options["max_tokens"] != wantTokens[i] {
			t.Fatalf("call %d max_tokens = %v, want %d", i, call.options["max_tokens"], wantTokens[i])
		}
	}
	if len(textProvider.calls) != 0 {
		t.Fatalf("text calls = %d, want 0", len(textProvider.calls))
	}
}

func TestNPCMaintenanceMalformedJSONLogsResponseMetadata(t *testing.T) {
	workspace := t.TempDir()
	logPath := filepath.Join(workspace, "agent.log")

	initialLevel := logger.GetLevel()
	defer logger.SetLevel(initialLevel)
	if err := logger.EnableFileLogging(logPath); err != nil {
		t.Fatalf("EnableFileLogging() error = %v", err)
	}
	defer logger.DisableFileLogging()
	logger.SetLevel(logger.INFO)

	maintenanceProvider := &routeCaptureProvider{
		name: "maintenance",
		responses: []*providers.LLMResponse{
			{
				Content:      `{"state":`,
				FinishReason: "length",
				Usage: &providers.UsageInfo{
					PromptTokens:     11,
					CompletionTokens: 22,
					TotalTokens:      33,
				},
			},
			{Content: scriptedNPCStateMemoryUpdate("repair note"), FinishReason: "stop"},
		},
	}
	agent := newRouteTestAgent(&routeCaptureProvider{name: "text"}, nil)
	agent.StateStore = NewNPCStateStore(workspace)
	configureMaintenanceRoute(agent, maintenanceProvider)

	err := newRouteTestLoop().updateNPCStateAndMemory(
		context.Background(),
		agent,
		defaultNPCState(),
		1,
		maintenanceRouteTestMessage(),
		"session",
		"reply",
	)
	if err != nil {
		t.Fatalf("updateNPCStateAndMemory() error = %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	logText := string(raw)
	for _, want := range []string{
		`"message":"NPC state/memory updater retrying"`,
		`"finish_reason":"length"`,
		`"prompt_tokens":11`,
		`"completion_tokens":22`,
		`"total_tokens":33`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("log missing %s, got: %s", want, logText)
		}
	}
}
