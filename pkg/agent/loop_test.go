package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/routing"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type fakeChannel struct{ id string }

func (f *fakeChannel) Name() string                                            { return "fake" }
func (f *fakeChannel) Start(ctx context.Context) error                         { return nil }
func (f *fakeChannel) Stop(ctx context.Context) error                          { return nil }
func (f *fakeChannel) Send(ctx context.Context, msg bus.OutboundMessage) error { return nil }
func (f *fakeChannel) IsRunning() bool                                         { return true }
func (f *fakeChannel) IsAllowed(string) bool                                   { return true }
func (f *fakeChannel) IsAllowedSender(sender bus.SenderInfo) bool              { return true }
func (f *fakeChannel) ReasoningChannelID() string                              { return f.id }

func newTestAgentLoop(
	t *testing.T,
) (al *AgentLoop, cfg *config.Config, msgBus *bus.MessageBus, provider *mockProvider, cleanup func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	cfg = &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}
	msgBus = bus.NewMessageBus()
	provider = &mockProvider{}
	al = NewAgentLoop(cfg, msgBus, provider)
	return al, cfg, msgBus, provider, func() { os.RemoveAll(tmpDir) }
}

func TestRecordLastChannel(t *testing.T) {
	al, cfg, msgBus, provider, cleanup := newTestAgentLoop(t)
	defer cleanup()

	testChannel := "test-channel"
	if err := al.RecordLastChannel(testChannel); err != nil {
		t.Fatalf("RecordLastChannel failed: %v", err)
	}
	if got := al.state.GetLastChannel(); got != testChannel {
		t.Errorf("Expected channel '%s', got '%s'", testChannel, got)
	}
	al2 := NewAgentLoop(cfg, msgBus, provider)
	if got := al2.state.GetLastChannel(); got != testChannel {
		t.Errorf("Expected persistent channel '%s', got '%s'", testChannel, got)
	}
}

func TestRecordLastChatID(t *testing.T) {
	al, cfg, msgBus, provider, cleanup := newTestAgentLoop(t)
	defer cleanup()

	testChatID := "test-chat-id-123"
	if err := al.RecordLastChatID(testChatID); err != nil {
		t.Fatalf("RecordLastChatID failed: %v", err)
	}
	if got := al.state.GetLastChatID(); got != testChatID {
		t.Errorf("Expected chat ID '%s', got '%s'", testChatID, got)
	}
	al2 := NewAgentLoop(cfg, msgBus, provider)
	if got := al2.state.GetLastChatID(); got != testChatID {
		t.Errorf("Expected persistent chat ID '%s', got '%s'", testChatID, got)
	}
}

func TestNewAgentLoop_StateInitialized(t *testing.T) {
	// Create temp workspace
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test config
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	// Create agent loop
	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	// Verify state manager is initialized
	if al.state == nil {
		t.Error("Expected state manager to be initialized")
	}

	// Verify state directory was created
	stateDir := filepath.Join(tmpDir, "state")
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		t.Error("Expected state directory to exist")
	}
}

// TestToolRegistry_ToolRegistration verifies tools can be registered and retrieved
func TestToolRegistry_ToolRegistration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	// Register a custom tool
	customTool := &mockCustomTool{}
	al.RegisterTool(customTool)

	// Verify tool is registered by checking it doesn't panic on GetStartupInfo
	// (actual tool retrieval is tested in tools package tests)
	info := al.GetStartupInfo()
	toolsInfo := info["tools"].(map[string]any)
	toolsList := toolsInfo["names"].([]string)

	// Check that our custom tool name is in the list
	found := slices.Contains(toolsList, "mock_custom")
	if !found {
		t.Error("Expected custom tool to be registered")
	}
}

func TestToolRegistry_RegistersReactToolForWhatsAppNative(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
		Channels: config.ChannelsConfig{
			WhatsApp: config.WhatsAppConfig{
				Enabled:   true,
				UseNative: true,
			},
		},
	}

	al := NewAgentLoop(cfg, bus.NewMessageBus(), &mockProvider{})
	agent := al.registry.GetDefaultAgent()
	if agent == nil {
		t.Fatal("expected default agent")
	}
	if _, ok := agent.Tools.Get("react"); !ok {
		t.Fatal("expected react tool to be registered for WhatsApp native")
	}
	if _, ok := agent.Tools.Get("send_file"); !ok {
		t.Fatal("expected send_file tool to be registered for WhatsApp native")
	}
}

// TestToolContext_Updates verifies tool context helpers work correctly.
func TestToolContext_Updates(t *testing.T) {
	ctx := tools.WithToolExecutionContext(
		context.Background(),
		"telegram",
		"chat-42",
		"msg-99",
		"user-11",
		"agent:main:telegram:direct:user-11",
		[]string{"media://img-1", "media://img-2"},
	)

	if got := tools.ToolChannel(ctx); got != "telegram" {
		t.Errorf("expected channel 'telegram', got %q", got)
	}
	if got := tools.ToolChatID(ctx); got != "chat-42" {
		t.Errorf("expected chatID 'chat-42', got %q", got)
	}
	if got := tools.ToolMessageID(ctx); got != "msg-99" {
		t.Errorf("expected messageID 'msg-99', got %q", got)
	}
	if got := tools.ToolMediaRefs(ctx); !slices.Equal(got, []string{"media://img-1", "media://img-2"}) {
		t.Errorf("expected media refs to round-trip, got %v", got)
	}
	if got := tools.ToolSenderID(ctx); got != "user-11" {
		t.Errorf("expected senderID 'user-11', got %q", got)
	}
	if got := tools.ToolSessionKey(ctx); got != "agent:main:telegram:direct:user-11" {
		t.Errorf("expected sessionKey 'agent:main:telegram:direct:user-11', got %q", got)
	}
	if got := tools.ToolChannel(context.Background()); got != "" {
		t.Errorf("expected empty channel from bare context, got %q", got)
	}
	if got := tools.ToolChatID(context.Background()); got != "" {
		t.Errorf("expected empty chatID from bare context, got %q", got)
	}
	if got := tools.ToolMessageID(context.Background()); got != "" {
		t.Errorf("expected empty messageID from bare context, got %q", got)
	}
	if got := tools.ToolMediaRefs(context.Background()); got != nil {
		t.Errorf("expected empty media refs from bare context, got %v", got)
	}
	if got := tools.ToolSenderID(context.Background()); got != "" {
		t.Errorf("expected empty senderID from bare context, got %q", got)
	}
	if got := tools.ToolSessionKey(context.Background()); got != "" {
		t.Errorf("expected empty sessionKey from bare context, got %q", got)
	}
}

func TestSanitizeHistoryForProvider_DropsIncompleteAssistantToolTurn(t *testing.T) {
	history := []providers.Message{
		{Role: "user", Content: "draw a mall"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []providers.ToolCall{
				{ID: "call_1", Name: "generate_image"},
			},
		},
		{Role: "user", Content: "retry"},
	}

	got := sanitizeHistoryForProvider(history)
	want := []providers.Message{
		{Role: "user", Content: "draw a mall"},
		{Role: "user", Content: "retry"},
	}

	if !slices.EqualFunc(got, want, func(a, b providers.Message) bool {
		return a.Role == b.Role && a.Content == b.Content && len(a.ToolCalls) == len(b.ToolCalls) && a.ToolCallID == b.ToolCallID
	}) {
		t.Fatalf("sanitized history = %#v, want %#v", got, want)
	}
}

func TestSanitizeHistoryForProvider_PreservesCompleteAssistantToolTurn(t *testing.T) {
	history := []providers.Message{
		{Role: "user", Content: "draw a mall"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []providers.ToolCall{
				{ID: "call_1", Name: "generate_image"},
			},
		},
		{Role: "tool", Content: "generated", ToolCallID: "call_1"},
		{Role: "assistant", Content: "done"},
	}

	got := sanitizeHistoryForProvider(history)
	if len(got) != len(history) {
		t.Fatalf("expected complete tool-call turn to be preserved, got %#v", got)
	}
}

// TestToolRegistry_GetDefinitions verifies tool definitions can be retrieved
func TestToolRegistry_GetDefinitions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	// Register a test tool and verify it shows up in startup info
	testTool := &mockCustomTool{}
	al.RegisterTool(testTool)

	info := al.GetStartupInfo()
	toolsInfo := info["tools"].(map[string]any)
	toolsList := toolsInfo["names"].([]string)

	// Check that our custom tool name is in the list
	found := slices.Contains(toolsList, "mock_custom")
	if !found {
		t.Error("Expected custom tool to be registered")
	}
}

// TestAgentLoop_GetStartupInfo verifies startup info contains tools
func TestAgentLoop_GetStartupInfo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	info := al.GetStartupInfo()

	// Verify tools info exists
	toolsInfo, ok := info["tools"]
	if !ok {
		t.Fatal("Expected 'tools' key in startup info")
	}

	toolsMap, ok := toolsInfo.(map[string]any)
	if !ok {
		t.Fatal("Expected 'tools' to be a map")
	}

	count, ok := toolsMap["count"]
	if !ok {
		t.Fatal("Expected 'count' in tools info")
	}

	// Should have default tools registered
	if count.(int) == 0 {
		t.Error("Expected at least some tools to be registered")
	}
}

// TestAgentLoop_Stop verifies Stop() sets running to false
func TestAgentLoop_Stop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	// Note: running is only set to true when Run() is called
	// We can't test that without starting the event loop
	// Instead, verify the Stop method can be called safely
	al.Stop()

	// Verify running is false (initial state or after Stop)
	if al.running.Load() {
		t.Error("Expected agent to be stopped (or never started)")
	}
}

// Mock implementations for testing

type simpleMockProvider struct {
	response string
}

func (m *simpleMockProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{
		Content:   m.response,
		ToolCalls: []providers.ToolCall{},
	}, nil
}

func (m *simpleMockProvider) GetDefaultModel() string {
	return "mock-model"
}

type execToolCallProvider struct {
	showOutput bool
	calls      int
}

func (p *execToolCallProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	if p.calls == 0 {
		p.calls++
		args := map[string]any{"command": "echo 'hello from exec'"}
		if p.showOutput {
			args["show_output"] = true
		}
		return &providers.LLMResponse{
			ToolCalls: []providers.ToolCall{
				{
					ID:        "call_exec_1",
					Name:      "exec",
					Arguments: args,
				},
			},
			FinishReason: "tool_calls",
		}, nil
	}

	return &providers.LLMResponse{
		Content:      "Final answer",
		ToolCalls:    []providers.ToolCall{},
		FinishReason: "stop",
	}, nil
}

func (p *execToolCallProvider) GetDefaultModel() string {
	return "mock-model"
}

type captureOptsProvider struct {
	response string
	lastOpts map[string]any
}

func (m *captureOptsProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	cloned := make(map[string]any, len(opts))
	for key, value := range opts {
		cloned[key] = value
	}
	m.lastOpts = cloned
	return &providers.LLMResponse{
		Content:   m.response,
		ToolCalls: []providers.ToolCall{},
	}, nil
}

func (m *captureOptsProvider) GetDefaultModel() string {
	return "mock-model"
}

type captureMessagesProvider struct {
	response string
	calls    [][]providers.Message
}

func (m *captureMessagesProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	cloned := make([]providers.Message, len(messages))
	copy(cloned, messages)
	m.calls = append(m.calls, cloned)
	return &providers.LLMResponse{
		Content:   m.response,
		ToolCalls: []providers.ToolCall{},
	}, nil
}

func (m *captureMessagesProvider) GetDefaultModel() string {
	return "mock-capture-model"
}

type fakeVoiceNoteTranscriber struct {
	transcript string
	err        error
	calls      []string
}

func (f *fakeVoiceNoteTranscriber) Transcribe(ctx context.Context, workspace, audioPath string) (string, error) {
	f.calls = append(f.calls, workspace+"|"+audioPath)
	if f.err != nil {
		return "", f.err
	}
	return f.transcript, nil
}

func extractPromptLineValue(content, prefix string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

type scriptedSummaryResult struct {
	content string
	err     error
}

type scriptedSummaryProvider struct {
	results  []scriptedSummaryResult
	calls    int
	lastOpts map[string]any
}

func (m *scriptedSummaryProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	cloned := make(map[string]any, len(opts))
	for key, value := range opts {
		cloned[key] = value
	}
	m.lastOpts = cloned

	m.calls++
	resultIndex := m.calls - 1
	if resultIndex >= len(m.results) {
		resultIndex = len(m.results) - 1
	}
	result := m.results[resultIndex]
	if result.err != nil {
		return nil, result.err
	}

	return &providers.LLMResponse{
		Content:   result.content,
		ToolCalls: []providers.ToolCall{},
	}, nil
}

func (m *scriptedSummaryProvider) GetDefaultModel() string {
	return "mock-scripted-model"
}

func TestSummarizeBatch_UsesConfiguredMaxTokens(t *testing.T) {
	tests := []struct {
		name          string
		maxTokens     int
		expectedLimit int
	}{
		{name: "configured max tokens", maxTokens: 4096, expectedLimit: 4096},
		{name: "fallback default", maxTokens: 0, expectedLimit: 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &captureOptsProvider{response: "summary"}
			agent := &AgentInstance{
				ID:        "test-agent",
				Model:     "mock-model",
				MaxTokens: tt.maxTokens,
				Provider:  provider,
			}

			al := &AgentLoop{}
			_, err := al.summarizeBatch(context.Background(), agent, []providers.Message{{
				Role:    "user",
				Content: "hello world",
			}}, "")
			if err != nil {
				t.Fatalf("summarizeBatch returned error: %v", err)
			}

			got, ok := provider.lastOpts["max_tokens"].(int)
			if !ok {
				t.Fatalf("expected int max_tokens option, got %#v", provider.lastOpts["max_tokens"])
			}
			if got != tt.expectedLimit {
				t.Fatalf("max_tokens = %d, want %d", got, tt.expectedLimit)
			}
		})
	}
}

func TestSummarizeBatch_RetriesTransientFailures(t *testing.T) {
	provider := &scriptedSummaryProvider{
		results: []scriptedSummaryResult{
			{err: errors.New("temporary provider failure")},
			{content: "retried summary"},
		},
	}
	agent := &AgentInstance{
		ID:        "test-agent",
		Model:     "mock-model",
		MaxTokens: 4096,
		Provider:  provider,
	}

	al := &AgentLoop{}
	summary, err := al.summarizeBatch(context.Background(), agent, []providers.Message{{
		Role:    "user",
		Content: "hello world",
	}}, "")
	if err != nil {
		t.Fatalf("summarizeBatch returned error: %v", err)
	}
	if summary != "retried summary" {
		t.Fatalf("summary = %q, want %q", summary, "retried summary")
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want %d", provider.calls, 2)
	}
	got, ok := provider.lastOpts["max_tokens"].(int)
	if !ok {
		t.Fatalf("expected int max_tokens option, got %#v", provider.lastOpts["max_tokens"])
	}
	if got != 4096 {
		t.Fatalf("max_tokens = %d, want %d", got, 4096)
	}
}

func TestSummarizeBatch_FallsBackAfterRetryFailures(t *testing.T) {
	provider := &scriptedSummaryProvider{
		results: []scriptedSummaryResult{{err: errors.New("provider unavailable")}},
	}
	agent := &AgentInstance{
		ID:       "test-agent",
		Model:    "mock-model",
		Provider: provider,
	}
	batch := []providers.Message{
		{Role: "user", Content: "hello there"},
		{Role: "assistant", Content: "general kenobi"},
	}

	al := &AgentLoop{}
	summary, err := al.summarizeBatch(context.Background(), agent, batch, "")
	if err != nil {
		t.Fatalf("summarizeBatch returned error: %v", err)
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls = %d, want %d", provider.calls, 3)
	}
	if !strings.Contains(summary, "Conversation summary:") {
		t.Fatalf("expected fallback summary prefix, got %q", summary)
	}
	if !strings.Contains(summary, "user: hello there") {
		t.Fatalf("expected user content in fallback summary, got %q", summary)
	}
	if !strings.Contains(summary, "assistant: general kenobi") {
		t.Fatalf("expected assistant content in fallback summary, got %q", summary)
	}
}

func TestSummarizeBatch_FallbackIsRuneSafe(t *testing.T) {
	provider := &scriptedSummaryProvider{
		results: []scriptedSummaryResult{{content: ""}},
	}
	agent := &AgentInstance{
		ID:       "test-agent",
		Model:    "mock-model",
		Provider: provider,
	}
	content := strings.Repeat("界", 205)

	al := &AgentLoop{}
	summary, err := al.summarizeBatch(context.Background(), agent, []providers.Message{{
		Role:    "user",
		Content: content,
	}}, "")
	if err != nil {
		t.Fatalf("summarizeBatch returned error: %v", err)
	}
	if !utf8.ValidString(summary) {
		t.Fatalf("fallback summary is not valid UTF-8: %q", summary)
	}
	if !strings.Contains(summary, strings.Repeat("界", 200)+"...") {
		t.Fatalf("expected rune-safe truncated content in fallback summary, got %q", summary)
	}
}

func TestFindNearestUserMessage(t *testing.T) {
	tests := []struct {
		name     string
		messages []providers.Message
		mid      int
		want     int
	}{
		{
			name: "prefers previous user message",
			messages: []providers.Message{
				{Role: "assistant"},
				{Role: "user"},
				{Role: "assistant"},
				{Role: "assistant"},
				{Role: "user"},
			},
			mid:  3,
			want: 1,
		},
		{
			name: "uses next user when needed",
			messages: []providers.Message{
				{Role: "assistant"},
				{Role: "assistant"},
				{Role: "user"},
			},
			mid:  1,
			want: 2,
		},
		{
			name: "returns original index without user message",
			messages: []providers.Message{
				{Role: "assistant"},
				{Role: "assistant"},
			},
			mid:  1,
			want: 1,
		},
	}

	al := &AgentLoop{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := al.findNearestUserMessage(tt.messages, tt.mid)
			if got != tt.want {
				t.Fatalf("findNearestUserMessage() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBestSenderLabel(t *testing.T) {
	tests := []struct {
		name string
		msg  bus.InboundMessage
		want string
	}{
		{
			name: "display name wins",
			msg: bus.InboundMessage{
				Sender: bus.SenderInfo{
					DisplayName: "Alice",
					Username:    "alice123",
					PlatformID:  "42",
				},
				Metadata: map[string]string{"sender_name": "Alias"},
			},
			want: "Alice",
		},
		{
			name: "username before metadata",
			msg: bus.InboundMessage{
				Sender:   bus.SenderInfo{Username: "alice123", PlatformID: "42"},
				Metadata: map[string]string{"sender_name": "Alias"},
			},
			want: "alice123",
		},
		{
			name: "metadata fallback",
			msg: bus.InboundMessage{
				Sender:   bus.SenderInfo{PlatformID: "42"},
				Metadata: map[string]string{"user_name": "WhatsApp Alice"},
			},
			want: "WhatsApp Alice",
		},
		{
			name: "platform id fallback",
			msg: bus.InboundMessage{
				Sender: bus.SenderInfo{PlatformID: "42"},
			},
			want: "42",
		},
		{
			name: "sender id fallback",
			msg: bus.InboundMessage{
				SenderID: "whatsapp:42",
			},
			want: "whatsapp:42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bestSenderLabel(tt.msg); got != tt.want {
				t.Fatalf("bestSenderLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolSenderID(t *testing.T) {
	tests := []struct {
		name string
		msg  bus.InboundMessage
		want string
	}{
		{
			name: "platform id preferred",
			msg: bus.InboundMessage{
				SenderID: "whatsapp:130184887930990:59@lid",
				Sender: bus.SenderInfo{
					Platform:   "whatsapp",
					PlatformID: "130184887930990:59@lid",
				},
			},
			want: "130184887930990:59@lid",
		},
		{
			name: "sender id fallback",
			msg: bus.InboundMessage{
				SenderID: "whatsapp:42",
			},
			want: "whatsapp:42",
		},
		{
			name: "empty when missing",
			msg:  bus.InboundMessage{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolSenderID(tt.msg); got != tt.want {
				t.Fatalf("toolSenderID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProcessMessage_GroupMessagePersistsAttributedHistory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &captureMessagesProvider{response: "ok"}
	al := NewAgentLoop(cfg, msgBus, provider)
	helper := testHelper{al: al}
	sessionKey := al.registry.ResolveRoute(routing.RouteInput{
		Channel: "whatsapp",
		Peer:    &routing.RoutePeer{Kind: "group", ID: "group-1"},
	}).SessionKey

	first := bus.InboundMessage{
		Channel:  "whatsapp",
		SenderID: "whatsapp:1",
		Sender: bus.SenderInfo{
			Platform:    "whatsapp",
			PlatformID:  "1",
			DisplayName: "Alice",
		},
		ChatID:  "group-1",
		Content: "hello there",
		Peer:    bus.Peer{Kind: "group", ID: "group-1"},
	}
	second := bus.InboundMessage{
		Channel:  "whatsapp",
		SenderID: "whatsapp:2",
		Sender: bus.SenderInfo{
			Platform:    "whatsapp",
			PlatformID:  "2",
			DisplayName: "Bob",
		},
		ChatID:  "group-1",
		Content: "hi again",
		Peer:    bus.Peer{Kind: "group", ID: "group-1"},
	}

	if got := helper.executeAndGetResponse(t, context.Background(), first); got != "ok" {
		t.Fatalf("first response = %q, want ok", got)
	}
	if got := helper.executeAndGetResponse(t, context.Background(), second); got != "ok" {
		t.Fatalf("second response = %q, want ok", got)
	}

	if len(provider.calls) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.calls))
	}

	firstCall := provider.calls[0]
	if got := firstCall[len(firstCall)-1].Content; got != "[From: Alice] hello there" {
		t.Fatalf("first call current user = %q, want %q", got, "[From: Alice] hello there")
	}

	secondCall := provider.calls[1]
	if len(secondCall) < 4 {
		t.Fatalf("second call message count = %d, want at least 4", len(secondCall))
	}
	if got := secondCall[1].Content; got != "[From: Alice] hello there" {
		t.Fatalf("second call history user = %q, want %q", got, "[From: Alice] hello there")
	}
	if got := secondCall[len(secondCall)-1].Content; got != "[From: Bob] hi again" {
		t.Fatalf("second call current user = %q, want %q", got, "[From: Bob] hi again")
	}

	history := al.registry.GetDefaultAgent().Sessions.GetHistory(sessionKey)
	if len(history) != 4 {
		t.Fatalf("history len = %d, want 4", len(history))
	}
	if got := history[0].Content; got != "[From: Alice] hello there" {
		t.Fatalf("history[0] = %q, want %q", got, "[From: Alice] hello there")
	}
	if got := history[2].Content; got != "[From: Bob] hi again" {
		t.Fatalf("history[2] = %q, want %q", got, "[From: Bob] hi again")
	}
}

func TestProcessMessage_DirectMessageDoesNotAddSenderAttribution(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &captureMessagesProvider{response: "ok"}
	al := NewAgentLoop(cfg, msgBus, provider)
	helper := testHelper{al: al}
	sessionKey := al.registry.ResolveRoute(routing.RouteInput{
		Channel: "telegram",
		Peer:    &routing.RoutePeer{Kind: "direct", ID: "42"},
	}).SessionKey

	msg := bus.InboundMessage{
		Channel:  "telegram",
		SenderID: "telegram:42",
		Sender: bus.SenderInfo{
			Platform:    "telegram",
			PlatformID:  "42",
			DisplayName: "Alice",
		},
		ChatID:  "42",
		Content: "hello",
		Peer:    bus.Peer{Kind: "direct", ID: "42"},
	}

	if got := helper.executeAndGetResponse(t, context.Background(), msg); got != "ok" {
		t.Fatalf("response = %q, want ok", got)
	}

	if len(provider.calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.calls))
	}
	call := provider.calls[0]
	if got := call[len(call)-1].Content; got != "hello" {
		t.Fatalf("current user = %q, want %q", got, "hello")
	}

	history := al.registry.GetDefaultAgent().Sessions.GetHistory(sessionKey)
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2", len(history))
	}
	if got := history[0].Content; got != "hello" {
		t.Fatalf("history[0] = %q, want %q", got, "hello")
	}
}

func TestProcessMessage_ReplyContextOnlyAugmentsLivePrompt(t *testing.T) {
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
	}

	msgBus := bus.NewMessageBus()
	provider := &captureMessagesProvider{response: "ok"}
	al := NewAgentLoop(cfg, msgBus, provider)
	helper := testHelper{al: al}
	sessionKey := al.registry.ResolveRoute(routing.RouteInput{
		Channel: "whatsapp_native",
		Peer:    &routing.RoutePeer{Kind: "direct", ID: "42"},
	}).SessionKey

	msg := bus.InboundMessage{
		Channel:   "whatsapp_native",
		SenderID:  "whatsapp:42",
		ChatID:    "42",
		MessageID: "wamid-7",
		Content:   "what do you mean?",
		Peer:      bus.Peer{Kind: "direct", ID: "42"},
		Metadata: map[string]string{
			"reply_to_message_id": "wamid-prev",
			"reply_to_text":       "Earlier assistant message",
		},
	}

	if got := helper.executeAndGetResponse(t, context.Background(), msg); got != "ok" {
		t.Fatalf("response = %q, want ok", got)
	}

	if len(provider.calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.calls))
	}
	livePrompt := provider.calls[0][len(provider.calls[0])-1].Content
	if !strings.Contains(livePrompt, "[Reply context: quoted message: Earlier assistant message]") {
		t.Fatalf("live prompt missing reply context: %q", livePrompt)
	}
	if !strings.Contains(livePrompt, "what do you mean?") {
		t.Fatalf("live prompt missing original message: %q", livePrompt)
	}

	history := al.registry.GetDefaultAgent().Sessions.GetHistory(sessionKey)
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2", len(history))
	}
	if got := history[0].Content; got != "what do you mean?" {
		t.Fatalf("history[0] = %q, want %q", got, "what do you mean?")
	}
}

// mockCustomTool is a simple mock tool for registration testing
type mockCustomTool struct{}

func (m *mockCustomTool) Name() string {
	return "mock_custom"
}

func (m *mockCustomTool) Description() string {
	return "Mock custom tool for testing"
}

func (m *mockCustomTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (m *mockCustomTool) Execute(ctx context.Context, args map[string]any) *tools.ToolResult {
	return tools.SilentResult("Custom tool executed")
}

type mockMediaResultTool struct{}

func (m *mockMediaResultTool) Name() string {
	return "mock_media_result"
}

func (m *mockMediaResultTool) Description() string {
	return "Mock media tool for testing"
}

func (m *mockMediaResultTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (m *mockMediaResultTool) Execute(ctx context.Context, args map[string]any) *tools.ToolResult {
	return tools.MediaResult("Media sent", []string{"media://test-ref"})
}

type singleToolCallProvider struct {
	toolName string
	args     map[string]any
	calls    int
}

func (p *singleToolCallProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	if p.calls == 0 {
		p.calls++
		args := map[string]any{}
		for key, value := range p.args {
			args[key] = value
		}
		return &providers.LLMResponse{
			ToolCalls: []providers.ToolCall{{
				ID:        "tool-call-1",
				Name:      p.toolName,
				Arguments: args,
			}},
			FinishReason: "tool_calls",
		}, nil
	}

	return &providers.LLMResponse{
		Content:      "done",
		FinishReason: "stop",
	}, nil
}

func (p *singleToolCallProvider) GetDefaultModel() string {
	return "mock-tool-model"
}

// testHelper executes a message and returns the response
type testHelper struct {
	al *AgentLoop
}

func (h testHelper) executeAndGetResponse(tb testing.TB, ctx context.Context, msg bus.InboundMessage) string {
	// Use a short timeout to avoid hanging
	timeoutCtx, cancel := context.WithTimeout(ctx, responseTimeout)
	defer cancel()

	response, err := h.al.processMessage(timeoutCtx, msg)
	if err != nil {
		tb.Fatalf("processMessage failed: %v", err)
	}
	return response
}

const responseTimeout = 3 * time.Second

func drainOutboundMessages(msgBus *bus.MessageBus, wait time.Duration) []bus.OutboundMessage {
	var msgs []bus.OutboundMessage
	for {
		ctx, cancel := context.WithTimeout(context.Background(), wait)
		msg, ok := msgBus.SubscribeOutbound(ctx)
		cancel()
		if !ok {
			break
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func drainOutboundMediaMessages(msgBus *bus.MessageBus, wait time.Duration) []bus.OutboundMediaMessage {
	var msgs []bus.OutboundMediaMessage
	for {
		ctx, cancel := context.WithTimeout(context.Background(), wait)
		msg, ok := msgBus.SubscribeOutboundMedia(ctx)
		cancel()
		if !ok {
			break
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

// TestToolResult_SilentToolDoesNotSendUserMessage verifies silent tools don't trigger outbound
func TestToolResult_SilentToolDoesNotSendUserMessage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &simpleMockProvider{response: "File operation complete"}
	al := NewAgentLoop(cfg, msgBus, provider)
	helper := testHelper{al: al}

	// ReadFileTool returns SilentResult, which should not send user message
	ctx := context.Background()
	msg := bus.InboundMessage{
		Channel:    "test",
		SenderID:   "user1",
		ChatID:     "chat1",
		Content:    "read test.txt",
		SessionKey: "test-session",
	}

	response := helper.executeAndGetResponse(t, ctx, msg)

	// Silent tool should return the LLM's response directly
	if response != "File operation complete" {
		t.Errorf("Expected 'File operation complete', got: %s", response)
	}
}

// TestToolResult_UserFacingToolDoesSendMessage verifies user-facing tools trigger outbound
func TestToolResult_UserFacingToolDoesSendMessage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &simpleMockProvider{response: "Command output: hello world"}
	al := NewAgentLoop(cfg, msgBus, provider)
	helper := testHelper{al: al}

	// ExecTool returns UserResult, which should send user message
	ctx := context.Background()
	msg := bus.InboundMessage{
		Channel:    "test",
		SenderID:   "user1",
		ChatID:     "chat1",
		Content:    "run hello",
		SessionKey: "test-session",
	}

	response := helper.executeAndGetResponse(t, ctx, msg)

	// User-facing tool should include the output in final response
	if response != "Command output: hello world" {
		t.Errorf("Expected 'Command output: hello world', got: %s", response)
	}
}

func TestProcessMessageAndSend_HiddenExecOutputOnlyPublishesFinalAnswer(t *testing.T) {
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
	}

	msgBus := bus.NewMessageBus()
	provider := &execToolCallProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	response, err := al.processMessageAndSend(context.Background(), bus.InboundMessage{
		Channel:    "test",
		SenderID:   "user1",
		ChatID:     "chat1",
		Content:    "check the weather",
		SessionKey: "test-session",
	})
	if err != nil {
		t.Fatalf("processMessageAndSend failed: %v", err)
	}
	if response != "Final answer" {
		t.Fatalf("response = %q, want %q", response, "Final answer")
	}

	outbound := drainOutboundMessages(msgBus, 50*time.Millisecond)
	if len(outbound) != 1 {
		t.Fatalf("expected 1 outbound message, got %d: %+v", len(outbound), outbound)
	}
	if outbound[0].Content != "Final answer" {
		t.Fatalf("expected final answer only, got %+v", outbound[0])
	}
}

func TestProcessMessageAndSend_ExplicitExecOutputPublishesToolResultAndFinalAnswer(t *testing.T) {
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
	}

	msgBus := bus.NewMessageBus()
	provider := &execToolCallProvider{showOutput: true}
	al := NewAgentLoop(cfg, msgBus, provider)

	response, err := al.processMessageAndSend(context.Background(), bus.InboundMessage{
		Channel:    "test",
		SenderID:   "user1",
		ChatID:     "chat1",
		Content:    "show me the exact command output",
		SessionKey: "test-session",
	})
	if err != nil {
		t.Fatalf("processMessageAndSend failed: %v", err)
	}
	if response != "Final answer" {
		t.Fatalf("response = %q, want %q", response, "Final answer")
	}

	outbound := drainOutboundMessages(msgBus, 50*time.Millisecond)
	if len(outbound) != 2 {
		t.Fatalf("expected 2 outbound messages, got %d: %+v", len(outbound), outbound)
	}
	if !strings.Contains(outbound[0].Content, "hello from exec") {
		t.Fatalf("expected first outbound message to be exec output, got %+v", outbound[0])
	}
	if outbound[1].Content != "Final answer" {
		t.Fatalf("expected second outbound message to be final answer, got %+v", outbound[1])
	}
}

func TestProcessMessageAndSend_WhatsAppNativeReplyCarriesReplyTarget(t *testing.T) {
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
	}

	msgBus := bus.NewMessageBus()
	provider := &simpleMockProvider{response: "Final answer"}
	al := NewAgentLoop(cfg, msgBus, provider)

	response, err := al.processMessageAndSend(context.Background(), bus.InboundMessage{
		Channel:   "whatsapp_native",
		SenderID:  "987654321@s.whatsapp.net",
		ChatID:    "12345-678@g.us",
		MessageID: "wamid-1",
		Content:   "reply to this",
		Sender: bus.SenderInfo{
			Platform:   "whatsapp",
			PlatformID: "987654321@s.whatsapp.net",
		},
		Peer:       bus.Peer{Kind: "group", ID: "12345-678@g.us"},
		SessionKey: "test-session",
	})
	if err != nil {
		t.Fatalf("processMessageAndSend failed: %v", err)
	}
	if response != "Final answer" {
		t.Fatalf("response = %q, want %q", response, "Final answer")
	}

	outbound := drainOutboundMessages(msgBus, 50*time.Millisecond)
	if len(outbound) != 1 {
		t.Fatalf("expected 1 outbound message, got %d: %+v", len(outbound), outbound)
	}
	if outbound[0].ReplyToMessageID != "wamid-1" {
		t.Fatalf("ReplyToMessageID = %q, want %q", outbound[0].ReplyToMessageID, "wamid-1")
	}
	if outbound[0].ReplyToSenderID != "987654321@s.whatsapp.net" {
		t.Fatalf("ReplyToSenderID = %q, want %q", outbound[0].ReplyToSenderID, "987654321@s.whatsapp.net")
	}
}

func TestProcessMessageAndSend_WhatsAppNativeMessageToolCarriesReplyTarget(t *testing.T) {
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
	}

	msgBus := bus.NewMessageBus()
	provider := &singleToolCallProvider{
		toolName: "message",
		args: map[string]any{
			"content": "replying via tool",
		},
	}
	al := NewAgentLoop(cfg, msgBus, provider)

	response, err := al.processMessageAndSend(context.Background(), bus.InboundMessage{
		Channel:   "whatsapp_native",
		SenderID:  "whatsapp:130184887930990:59@lid",
		ChatID:    "130184887930990@lid",
		MessageID: "wamid-lid-1",
		Content:   "can you try again?",
		Sender: bus.SenderInfo{
			Platform:   "whatsapp",
			PlatformID: "130184887930990:59@lid",
		},
		Peer:       bus.Peer{Kind: "direct", ID: "130184887930990@lid"},
		SessionKey: "test-session",
	})
	if err != nil {
		t.Fatalf("processMessageAndSend failed: %v", err)
	}
	if response != "done" {
		t.Fatalf("response = %q, want %q", response, "done")
	}

	outbound := drainOutboundMessages(msgBus, 50*time.Millisecond)
	if len(outbound) != 1 {
		t.Fatalf("expected 1 outbound message, got %d: %+v", len(outbound), outbound)
	}
	if outbound[0].Content != "replying via tool" {
		t.Fatalf("Content = %q, want %q", outbound[0].Content, "replying via tool")
	}
	if outbound[0].ReplyToMessageID != "wamid-lid-1" {
		t.Fatalf("ReplyToMessageID = %q, want %q", outbound[0].ReplyToMessageID, "wamid-lid-1")
	}
	if outbound[0].ReplyToSenderID != "130184887930990:59@lid" {
		t.Fatalf("ReplyToSenderID = %q, want %q", outbound[0].ReplyToSenderID, "130184887930990:59@lid")
	}
}

func TestProcessMessageAndSend_MediaResultCarriesReplyTarget(t *testing.T) {
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
	}

	msgBus := bus.NewMessageBus()
	provider := &singleToolCallProvider{toolName: "mock_media_result"}
	al := NewAgentLoop(cfg, msgBus, provider)
	al.RegisterTool(&mockMediaResultTool{})

	response, err := al.processMessageAndSend(context.Background(), bus.InboundMessage{
		Channel:   "whatsapp_native",
		SenderID:  "987654321@s.whatsapp.net",
		ChatID:    "12345-678@g.us",
		MessageID: "wamid-2",
		Content:   "send media",
		Sender: bus.SenderInfo{
			Platform:   "whatsapp",
			PlatformID: "987654321@s.whatsapp.net",
		},
		Peer:       bus.Peer{Kind: "group", ID: "12345-678@g.us"},
		SessionKey: "test-session",
	})
	if err != nil {
		t.Fatalf("processMessageAndSend failed: %v", err)
	}
	if response != "done" {
		t.Fatalf("response = %q, want %q", response, "done")
	}

	outboundMedia := drainOutboundMediaMessages(msgBus, 50*time.Millisecond)
	if len(outboundMedia) != 1 {
		t.Fatalf("expected 1 outbound media message, got %d: %+v", len(outboundMedia), outboundMedia)
	}
	if outboundMedia[0].ReplyToMessageID != "wamid-2" {
		t.Fatalf("ReplyToMessageID = %q, want %q", outboundMedia[0].ReplyToMessageID, "wamid-2")
	}
	if outboundMedia[0].ReplyToSenderID != "987654321@s.whatsapp.net" {
		t.Fatalf("ReplyToSenderID = %q, want %q", outboundMedia[0].ReplyToSenderID, "987654321@s.whatsapp.net")
	}
}

func TestProcessMessage_AutoProvisionCreatesDedicatedAgent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
			AutoProvision: config.AutoProvisionConfig{
				Enabled: true,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &simpleMockProvider{response: "ok"}
	al := NewAgentLoop(cfg, msgBus, provider)
	helper := testHelper{al: al}

	route := al.registry.ResolveRoute(routing.RouteInput{
		Channel: "telegram",
		Peer:    &routing.RoutePeer{Kind: "direct", ID: "user42"},
	})
	if route.MatchedBy != "auto-provision" {
		t.Fatalf("MatchedBy = %q, want 'auto-provision'", route.MatchedBy)
	}
	if _, exists := al.registry.GetAgent(route.AgentID); exists {
		t.Fatalf("agent %q should not exist before first message", route.AgentID)
	}

	response := helper.executeAndGetResponse(t, context.Background(), bus.InboundMessage{
		Channel:  "telegram",
		SenderID: "user42",
		ChatID:   "chat42",
		Content:  "hello",
		Peer:     bus.Peer{Kind: "direct", ID: "user42"},
	})

	if response != "ok" {
		t.Fatalf("response = %q, want %q", response, "ok")
	}

	agent, exists := al.registry.GetAgent(route.AgentID)
	if !exists {
		t.Fatalf("expected auto-provisioned agent %q to exist", route.AgentID)
	}
	if _, ok := agent.Tools.Get("message"); !ok {
		t.Fatalf("expected shared message tool to be registered on auto-provisioned agent")
	}
	if !strings.Contains(agent.Workspace, "workspace-"+route.AgentID) {
		t.Errorf("workspace = %q, expected to contain %q", agent.Workspace, "workspace-"+route.AgentID)
	}
}

func TestProcessMessage_AutoProvisionInheritsRegisteredTools(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
			AutoProvision: config.AutoProvisionConfig{
				Enabled: true,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &simpleMockProvider{response: "ok"}
	al := NewAgentLoop(cfg, msgBus, provider)
	al.RegisterTool(&mockCustomTool{})
	help := testHelper{al: al}

	route := al.registry.ResolveRoute(routing.RouteInput{
		Channel: "telegram",
		Peer:    &routing.RoutePeer{Kind: "direct", ID: "user99"},
	})
	if route.MatchedBy != "auto-provision" {
		t.Fatalf("MatchedBy = %q, want 'auto-provision'", route.MatchedBy)
	}

	response := help.executeAndGetResponse(t, context.Background(), bus.InboundMessage{
		Channel:  "telegram",
		SenderID: "user99",
		ChatID:   "chat99",
		Content:  "hello",
		Peer:     bus.Peer{Kind: "direct", ID: "user99"},
	})

	if response != "ok" {
		t.Fatalf("response = %q, want %q", response, "ok")
	}

	agent, exists := al.registry.GetAgent(route.AgentID)
	if !exists {
		t.Fatalf("expected auto-provisioned agent %q to exist", route.AgentID)
	}
	if _, ok := agent.Tools.Get("mock_custom"); !ok {
		t.Fatalf("expected registered global tool to be available on auto-provisioned agent")
	}
}

// failFirstMockProvider fails on the first N calls with a specific error
type failFirstMockProvider struct {
	failures    int
	currentCall int
	failError   error
	successResp string
}

func (m *failFirstMockProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	m.currentCall++
	if m.currentCall <= m.failures {
		return nil, m.failError
	}
	return &providers.LLMResponse{
		Content:   m.successResp,
		ToolCalls: []providers.ToolCall{},
	}, nil
}

func (m *failFirstMockProvider) GetDefaultModel() string {
	return "mock-fail-model"
}

func TestAgentLoop_TimeoutRetry(t *testing.T) {
	tests := []struct {
		name     string
		failErr  error
		response string
	}{
		{
			name: "http 408",
			failErr: errors.New(
				"API request failed:\n  Status: 408\n  Body:   {\"error\":{\"message\":\"stream error: stream disconnected before completion: stream closed before response.completed\"}}",
			),
			response: "Recovered after timeout",
		},
		{
			name:     "stream ended without completed response",
			failErr:  errors.New("codex API call: stream ended without completed response"),
			response: "Recovered after stream reconnect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "agent-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			cfg := &config.Config{
				Agents: config.AgentsConfig{
					Defaults: config.AgentDefaults{
						Workspace:         tmpDir,
						Model:             "test-model",
						MaxTokens:         4096,
						MaxToolIterations: 10,
					},
				},
			}

			provider := &failFirstMockProvider{
				failures:    1,
				failError:   tt.failErr,
				successResp: tt.response,
			}
			al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
			al.setRetrySleep(func(time.Duration) {})

			response, err := al.ProcessDirectWithChannel(
				context.Background(),
				"Trigger message",
				"test-session-timeout",
				"test",
				"test-chat",
			)
			if err != nil {
				t.Fatalf("Expected success after timeout retry, got error: %v", err)
			}

			if response != tt.response {
				t.Fatalf("response = %q, want %q", response, tt.response)
			}

			if provider.currentCall != 2 {
				t.Fatalf("Expected 2 calls (1 fail + 1 success), got %d", provider.currentCall)
			}
		})
	}
}

// TestAgentLoop_ContextExhaustionRetry verify that the agent retries on context errors
func TestAgentLoop_ContextExhaustionRetry(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()

	// Create a provider that fails once with a context error
	contextErr := fmt.Errorf("InvalidParameter: Total tokens of image and text exceed max message tokens")
	provider := &failFirstMockProvider{
		failures:    1,
		failError:   contextErr,
		successResp: "Recovered from context error",
	}

	al := NewAgentLoop(cfg, msgBus, provider)

	// Inject some history to simulate a full context
	sessionKey := "test-session-context"
	// Create dummy history
	history := []providers.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "Old message 1"},
		{Role: "assistant", Content: "Old response 1"},
		{Role: "user", Content: "Old message 2"},
		{Role: "assistant", Content: "Old response 2"},
		{Role: "user", Content: "Trigger message"},
	}
	defaultAgent := al.registry.GetDefaultAgent()
	if defaultAgent == nil {
		t.Fatal("No default agent found")
	}
	defaultAgent.Sessions.SetHistory(sessionKey, history)

	// Call ProcessDirectWithChannel
	// Note: ProcessDirectWithChannel calls processMessage which will execute runLLMIteration
	response, err := al.ProcessDirectWithChannel(
		context.Background(),
		"Trigger message",
		sessionKey,
		"test",
		"test-chat",
	)
	if err != nil {
		t.Fatalf("Expected success after retry, got error: %v", err)
	}

	if response != "Recovered from context error" {
		t.Errorf("Expected 'Recovered from context error', got '%s'", response)
	}

	// We expect 2 calls: 1st failed, 2nd succeeded
	if provider.currentCall != 2 {
		t.Errorf("Expected 2 calls (1 fail + 1 success), got %d", provider.currentCall)
	}

	// Check final history length
	finalHistory := defaultAgent.Sessions.GetHistory(sessionKey)
	// We verify that the history has been modified (compressed)
	// Original length: 6
	// Expected behavior: compression drops ~50% of history (mid slice)
	// We can assert that the length is NOT what it would be without compression.
	// Without compression: 6 + 1 (new user msg) + 1 (assistant msg) = 8
	if len(finalHistory) >= 8 {
		t.Errorf("Expected history to be compressed (len < 8), got %d", len(finalHistory))
	}
}

func TestTargetReasoningChannelID_AllChannels(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	al := NewAgentLoop(cfg, bus.NewMessageBus(), &mockProvider{})
	chManager, err := channels.NewManager(&config.Config{}, bus.NewMessageBus(), nil)
	if err != nil {
		t.Fatalf("Failed to create channel manager: %v", err)
	}
	for name, id := range map[string]string{
		"whatsapp":  "rid-whatsapp",
		"telegram":  "rid-telegram",
		"feishu":    "rid-feishu",
		"discord":   "rid-discord",
		"maixcam":   "rid-maixcam",
		"qq":        "rid-qq",
		"dingtalk":  "rid-dingtalk",
		"slack":     "rid-slack",
		"line":      "rid-line",
		"onebot":    "rid-onebot",
		"wecom":     "rid-wecom",
		"wecom_app": "rid-wecom-app",
	} {
		chManager.RegisterChannel(name, &fakeChannel{id: id})
	}
	al.SetChannelManager(chManager)
	tests := []struct {
		channel string
		wantID  string
	}{
		{channel: "whatsapp", wantID: "rid-whatsapp"},
		{channel: "telegram", wantID: "rid-telegram"},
		{channel: "feishu", wantID: "rid-feishu"},
		{channel: "discord", wantID: "rid-discord"},
		{channel: "maixcam", wantID: "rid-maixcam"},
		{channel: "qq", wantID: "rid-qq"},
		{channel: "dingtalk", wantID: "rid-dingtalk"},
		{channel: "slack", wantID: "rid-slack"},
		{channel: "line", wantID: "rid-line"},
		{channel: "onebot", wantID: "rid-onebot"},
		{channel: "wecom", wantID: "rid-wecom"},
		{channel: "wecom_app", wantID: "rid-wecom-app"},
		{channel: "unknown", wantID: ""},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			got := al.targetReasoningChannelID(tt.channel)
			if got != tt.wantID {
				t.Fatalf("targetReasoningChannelID(%q) = %q, want %q", tt.channel, got, tt.wantID)
			}
		})
	}
}

func TestHandleReasoning(t *testing.T) {
	newLoop := func(t *testing.T) (*AgentLoop, *bus.MessageBus) {
		t.Helper()
		tmpDir, err := os.MkdirTemp("", "agent-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
		cfg := &config.Config{
			Agents: config.AgentsConfig{
				Defaults: config.AgentDefaults{
					Workspace:         tmpDir,
					Model:             "test-model",
					MaxTokens:         4096,
					MaxToolIterations: 10,
				},
			},
		}
		msgBus := bus.NewMessageBus()
		return NewAgentLoop(cfg, msgBus, &mockProvider{}), msgBus
	}

	t.Run("skips when any required field is empty", func(t *testing.T) {
		al, msgBus := newLoop(t)
		al.handleReasoning(context.Background(), "reasoning", "telegram", "")

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if msg, ok := msgBus.SubscribeOutbound(ctx); ok {
			t.Fatalf("expected no outbound message, got %+v", msg)
		}
	})

	t.Run("publishes one message for non telegram", func(t *testing.T) {
		al, msgBus := newLoop(t)
		al.handleReasoning(context.Background(), "hello reasoning", "slack", "channel-1")

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		msg, ok := msgBus.SubscribeOutbound(ctx)
		if !ok {
			t.Fatal("expected an outbound message")
		}
		if msg.Channel != "slack" || msg.ChatID != "channel-1" || msg.Content != "hello reasoning" {
			t.Fatalf("unexpected outbound message: %+v", msg)
		}
	})

	t.Run("publishes one message for telegram", func(t *testing.T) {
		al, msgBus := newLoop(t)
		reasoning := "hello telegram reasoning"
		al.handleReasoning(context.Background(), reasoning, "telegram", "tg-chat")

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		msg, ok := msgBus.SubscribeOutbound(ctx)
		if !ok {
			t.Fatal("expected outbound message")
		}

		if msg.Channel != "telegram" {
			t.Fatalf("expected telegram channel message, got %+v", msg)
		}
		if msg.ChatID != "tg-chat" {
			t.Fatalf("expected chatID tg-chat, got %+v", msg)
		}
		if msg.Content != reasoning {
			t.Fatalf("content mismatch: got %q want %q", msg.Content, reasoning)
		}
	})
	t.Run("expired ctx", func(t *testing.T) {
		al, msgBus := newLoop(t)
		reasoning := "hello telegram reasoning"
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		al.handleReasoning(ctx, reasoning, "telegram", "tg-chat")

		ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		msg, ok := msgBus.SubscribeOutbound(ctx)
		if ok {
			t.Fatalf("expected no outbound message, got %+v", msg)
		}
	})

	t.Run("returns promptly when bus is full", func(t *testing.T) {
		al, msgBus := newLoop(t)

		// Fill the outbound bus buffer until a publish would block.
		// Use a short timeout to detect when the buffer is full,
		// rather than hardcoding the buffer size.
		for i := 0; ; i++ {
			fillCtx, fillCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			err := msgBus.PublishOutbound(fillCtx, bus.OutboundMessage{
				Channel: "filler",
				ChatID:  "filler",
				Content: fmt.Sprintf("filler-%d", i),
			})
			fillCancel()
			if err != nil {
				// Buffer is full (timed out trying to send).
				break
			}
		}

		// Use a short-deadline parent context to bound the test.
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		start := time.Now()
		al.handleReasoning(ctx, "should timeout", "slack", "channel-full")
		elapsed := time.Since(start)

		// handleReasoning uses a 5s internal timeout, but the parent ctx
		// expires in 500ms. It should return within ~500ms, not 5s.
		if elapsed > 2*time.Second {
			t.Fatalf("handleReasoning blocked too long (%v); expected prompt return", elapsed)
		}

		// Drain the bus and verify the reasoning message was NOT published
		// (it should have been dropped due to timeout).
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer drainCancel()
		foundReasoning := false
		for {
			msg, ok := msgBus.SubscribeOutbound(drainCtx)
			if !ok {
				break
			}
			if msg.Content == "should timeout" {
				foundReasoning = true
			}
		}
		if foundReasoning {
			t.Fatal("expected reasoning message to be dropped when bus is full, but it was published")
		}
	})
}

func TestResolveMediaRefs_ResolvesToBase64(t *testing.T) {
	store := media.NewFileMediaStore()
	dir := t.TempDir()

	// Create a minimal valid PNG (8-byte header is enough for filetype detection)
	pngPath := filepath.Join(dir, "test.png")
	// PNG magic: 0x89 P N G \r \n 0x1A \n + minimal IHDR
	pngHeader := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x0D, // IHDR length
		0x49, 0x48, 0x44, 0x52, // "IHDR"
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02, // 1x1 RGB
		0x00, 0x00, 0x00, // no interlace
		0x90, 0x77, 0x53, 0xDE, // CRC
	}
	if err := os.WriteFile(pngPath, pngHeader, 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := store.Store(pngPath, media.MediaMeta{}, "test")
	if err != nil {
		t.Fatal(err)
	}

	messages := []providers.Message{
		{Role: "user", Content: "describe this", Media: []string{ref}},
	}
	result := resolveMediaRefs(messages, store, config.DefaultMaxMediaSize)

	if len(result[0].Media) != 1 {
		t.Fatalf("expected 1 resolved media, got %d", len(result[0].Media))
	}
	if !strings.HasPrefix(result[0].Media[0], "data:image/png;base64,") {
		t.Fatalf("expected data:image/png;base64, prefix, got %q", result[0].Media[0][:40])
	}
}

func TestResolveMediaRefs_SkipsOversizedFile(t *testing.T) {
	store := media.NewFileMediaStore()
	dir := t.TempDir()

	bigPath := filepath.Join(dir, "big.png")
	// Write PNG header + padding to exceed limit
	data := make([]byte, 1024+1) // 1KB + 1 byte
	copy(data, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	if err := os.WriteFile(bigPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	ref, _ := store.Store(bigPath, media.MediaMeta{}, "test")

	messages := []providers.Message{
		{Role: "user", Content: "hi", Media: []string{ref}},
	}
	// Use a tiny limit (1KB) so the file is oversized
	result := resolveMediaRefs(messages, store, 1024)

	if len(result[0].Media) != 0 {
		t.Fatalf("expected 0 media (oversized), got %d", len(result[0].Media))
	}
}

func TestResolveMediaRefs_SkipsUnknownType(t *testing.T) {
	store := media.NewFileMediaStore()
	dir := t.TempDir()

	txtPath := filepath.Join(dir, "readme.txt")
	if err := os.WriteFile(txtPath, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, _ := store.Store(txtPath, media.MediaMeta{}, "test")

	messages := []providers.Message{
		{Role: "user", Content: "hi", Media: []string{ref}},
	}
	result := resolveMediaRefs(messages, store, config.DefaultMaxMediaSize)

	if len(result[0].Media) != 0 {
		t.Fatalf("expected 0 media (unknown type), got %d", len(result[0].Media))
	}
}

func TestResolveMediaRefs_PassesThroughNonMediaRefs(t *testing.T) {
	messages := []providers.Message{
		{Role: "user", Content: "hi", Media: []string{"https://example.com/img.png"}},
	}
	result := resolveMediaRefs(messages, nil, config.DefaultMaxMediaSize)

	if len(result[0].Media) != 1 || result[0].Media[0] != "https://example.com/img.png" {
		t.Fatalf("expected passthrough of non-media:// URL, got %v", result[0].Media)
	}
}

func TestResolveMediaRefs_DoesNotMutateOriginal(t *testing.T) {
	store := media.NewFileMediaStore()
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "test.png")
	pngHeader := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02,
		0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE,
	}
	os.WriteFile(pngPath, pngHeader, 0o644)
	ref, _ := store.Store(pngPath, media.MediaMeta{}, "test")

	original := []providers.Message{
		{Role: "user", Content: "hi", Media: []string{ref}},
	}
	originalRef := original[0].Media[0]

	resolveMediaRefs(original, store, config.DefaultMaxMediaSize)

	if original[0].Media[0] != originalRef {
		t.Fatal("resolveMediaRefs mutated original message slice")
	}
}

func TestResolveMediaRefs_UsesMetaContentType(t *testing.T) {
	store := media.NewFileMediaStore()
	dir := t.TempDir()

	// File with JPEG content but stored with explicit content type
	jpegPath := filepath.Join(dir, "photo")
	jpegHeader := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG magic bytes
	os.WriteFile(jpegPath, jpegHeader, 0o644)
	ref, _ := store.Store(jpegPath, media.MediaMeta{ContentType: "image/jpeg"}, "test")

	messages := []providers.Message{
		{Role: "user", Content: "hi", Media: []string{ref}},
	}
	result := resolveMediaRefs(messages, store, config.DefaultMaxMediaSize)

	if len(result[0].Media) != 1 {
		t.Fatalf("expected 1 media, got %d", len(result[0].Media))
	}
	if !strings.HasPrefix(result[0].Media[0], "data:image/jpeg;base64,") {
		t.Fatalf("expected jpeg prefix, got %q", result[0].Media[0][:30])
	}
}

func TestResolveMediaRefs_SniffsGenericBinaryContentType(t *testing.T) {
	store := media.NewFileMediaStore()
	dir := t.TempDir()

	pngPath := filepath.Join(dir, "upload.bin")
	pngHeader := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02,
		0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE,
	}
	if err := os.WriteFile(pngPath, pngHeader, 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := store.Store(pngPath, media.MediaMeta{ContentType: "application/octet-stream"}, "test")
	if err != nil {
		t.Fatal(err)
	}

	messages := []providers.Message{
		{Role: "user", Content: "hi", Media: []string{ref}},
	}
	result := resolveMediaRefs(messages, store, config.DefaultMaxMediaSize)

	if len(result[0].Media) != 1 {
		t.Fatalf("expected 1 media, got %d", len(result[0].Media))
	}
	if !strings.HasPrefix(result[0].Media[0], "data:image/png;base64,") {
		t.Fatalf("expected png prefix, got %q", result[0].Media[0][:30])
	}
}

func TestNormalizeInboundPromptMedia_StagesAudioAndKeepsImages(t *testing.T) {
	store := media.NewFileMediaStore()
	workspace := t.TempDir()
	srcDir := t.TempDir()

	audioPath := filepath.Join(srcDir, "voice.ogg")
	if err := os.WriteFile(audioPath, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	audioRef, err := store.Store(audioPath, media.MediaMeta{
		Filename:    "voice.ogg",
		ContentType: "audio/ogg",
	}, "audio-scope")
	if err != nil {
		t.Fatal(err)
	}

	imagePath := filepath.Join(srcDir, "photo.png")
	pngHeader := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02,
		0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE,
	}
	if err := os.WriteFile(imagePath, pngHeader, 0o644); err != nil {
		t.Fatal(err)
	}
	imageRef, err := store.Store(imagePath, media.MediaMeta{
		Filename:    "photo.png",
		ContentType: "image/png",
	}, "image-scope")
	if err != nil {
		t.Fatal(err)
	}

	liveMessage, sessionMessage, mediaRefs := normalizeInboundPromptMedia(
		"summarize this voice note",
		"summarize this voice note",
		workspace,
		[]string{audioRef, imageRef},
		store,
	)

	if got, want := mediaRefs, []string{imageRef}; !slices.Equal(got, want) {
		t.Fatalf("normalized media = %v, want %v", got, want)
	}
	if !strings.Contains(liveMessage, "summarize this voice note") {
		t.Fatalf("live message lost original caption: %q", liveMessage)
	}
	if !strings.Contains(liveMessage, "skills/stt/SKILL.md") {
		t.Fatalf("live message missing stt skill hint: %q", liveMessage)
	}
	if !strings.Contains(liveMessage, "[Image attachments available for this turn]") {
		t.Fatalf("live message missing current image refs note: %q", liveMessage)
	}
	if !strings.Contains(liveMessage, "photo.png => "+imageRef) {
		t.Fatalf("live message missing exact image ref: %q", liveMessage)
	}

	stagedPath := extractPromptLineValue(liveMessage, "Local file:")
	if stagedPath == "" {
		t.Fatalf("live message missing staged local file path: %q", liveMessage)
	}
	rel, err := filepath.Rel(workspace, stagedPath)
	if err != nil {
		t.Fatalf("filepath.Rel failed: %v", err)
	}
	if !filepath.IsLocal(rel) {
		t.Fatalf("staged path %q should be inside workspace %q", stagedPath, workspace)
	}
	if _, err := os.Stat(stagedPath); err != nil {
		t.Fatalf("staged audio file missing: %v", err)
	}

	if strings.Contains(sessionMessage, stagedPath) {
		t.Fatalf("session message should not persist staged local path: %q", sessionMessage)
	}
	if strings.Contains(sessionMessage, imageRef) {
		t.Fatalf("session message should not persist current image refs: %q", sessionMessage)
	}
	if !strings.Contains(sessionMessage, "[Audio attachment available for this turn: voice.ogg]") {
		t.Fatalf("session message missing generic audio note: %q", sessionMessage)
	}

	resolved := resolveMediaRefs([]providers.Message{{Role: "user", Media: mediaRefs}}, store, config.DefaultMaxMediaSize)
	if got := resolved[0].Media; len(got) != 1 || !strings.HasPrefix(got[0], "data:image/png;base64,") {
		t.Fatalf("resolved media = %v, want single image data URL", got)
	}
}

func TestNormalizeInboundPromptMedia_ListsCurrentImageRefsInStableOrder(t *testing.T) {
	store := media.NewFileMediaStore()
	workspace := t.TempDir()
	srcDir := t.TempDir()

	firstPath := filepath.Join(srcDir, "first.png")
	secondPath := filepath.Join(srcDir, "second.png")
	pngHeader := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02,
		0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE,
	}
	if err := os.WriteFile(firstPath, pngHeader, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, pngHeader, 0o644); err != nil {
		t.Fatal(err)
	}
	firstRef, err := store.Store(firstPath, media.MediaMeta{Filename: "first.png", ContentType: "image/png"}, "scope")
	if err != nil {
		t.Fatal(err)
	}
	secondRef, err := store.Store(secondPath, media.MediaMeta{Filename: "second.png", ContentType: "image/png"}, "scope")
	if err != nil {
		t.Fatal(err)
	}

	liveMessage, sessionMessage, mediaRefs := normalizeInboundPromptMedia(
		"edit one of these photos",
		"edit one of these photos",
		workspace,
		[]string{firstRef, secondRef},
		store,
	)

	if got, want := mediaRefs, []string{firstRef, secondRef}; !slices.Equal(got, want) {
		t.Fatalf("normalized media = %v, want %v", got, want)
	}
	if firstIdx, secondIdx := strings.Index(liveMessage, "first.png => "+firstRef), strings.Index(liveMessage, "second.png => "+secondRef); firstIdx == -1 || secondIdx == -1 || firstIdx >= secondIdx {
		t.Fatalf("live message should list current image refs in order: %q", liveMessage)
	}
	if strings.Contains(sessionMessage, firstRef) || strings.Contains(sessionMessage, secondRef) {
		t.Fatalf("session message should not persist current image refs: %q", sessionMessage)
	}
	if got, want := currentTurnImageMediaRefs(mediaRefs, store), []string{firstRef, secondRef}; !slices.Equal(got, want) {
		t.Fatalf("current turn image refs = %v, want %v", got, want)
	}

	resolved := resolveMediaRefs([]providers.Message{{Role: "user", Media: mediaRefs}}, store, config.DefaultMaxMediaSize)
	if got := resolved[0].Media; len(got) != 2 {
		t.Fatalf("resolved media = %v, want 2 image data URLs", got)
	}
}

func TestNormalizeInboundPromptMedia_UsesContentTypeExtensionForExtensionlessAudio(t *testing.T) {
	store := media.NewFileMediaStore()
	workspace := t.TempDir()
	srcDir := t.TempDir()

	audioPath := filepath.Join(srcDir, "voice-note")
	if err := os.WriteFile(audioPath, []byte("fake mp3"), 0o644); err != nil {
		t.Fatal(err)
	}
	audioRef, err := store.Store(audioPath, media.MediaMeta{
		Filename:    "",
		ContentType: "audio/mpeg",
	}, "audio-scope")
	if err != nil {
		t.Fatal(err)
	}

	liveMessage, _, mediaRefs := normalizeInboundPromptMedia(
		"transcribe this",
		"transcribe this",
		workspace,
		[]string{audioRef},
		store,
	)

	if len(mediaRefs) != 0 {
		t.Fatalf("expected staged audio ref to be removed from provider media, got %v", mediaRefs)
	}

	stagedPath := extractPromptLineValue(liveMessage, "Local file:")
	if stagedPath == "" {
		t.Fatalf("live message missing staged local file path: %q", liveMessage)
	}
	if got := strings.ToLower(filepath.Ext(stagedPath)); got != ".mp3" {
		t.Fatalf("staged path extension = %q, want .mp3 (path=%q)", got, stagedPath)
	}
	if !strings.Contains(liveMessage, "Original filename: audio-") {
		t.Fatalf("live message should fall back to staged filename when original name is absent: %q", liveMessage)
	}
	assertStagedPathsInWorkspace(t, workspace, liveMessage)
}

func TestNormalizeInboundPromptMedia_IgnoresBinWhenContentTypeCanRecoverAudioExtension(t *testing.T) {
	store := media.NewFileMediaStore()
	workspace := t.TempDir()
	srcDir := t.TempDir()

	audioPath := filepath.Join(srcDir, "voice.bin")
	if err := os.WriteFile(audioPath, []byte("fake opus"), 0o644); err != nil {
		t.Fatal(err)
	}
	audioRef, err := store.Store(audioPath, media.MediaMeta{
		Filename:    "audio-msg.bin",
		ContentType: "audio/ogg; codecs=opus",
	}, "audio-scope")
	if err != nil {
		t.Fatal(err)
	}

	liveMessage, _, mediaRefs := normalizeInboundPromptMedia(
		"transcribe this",
		"transcribe this",
		workspace,
		[]string{audioRef},
		store,
	)

	if len(mediaRefs) != 0 {
		t.Fatalf("expected staged audio ref to be removed from provider media, got %v", mediaRefs)
	}

	stagedPath := extractPromptLineValue(liveMessage, "Local file:")
	if stagedPath == "" {
		t.Fatalf("live message missing staged local file path: %q", liveMessage)
	}
	if got := strings.ToLower(filepath.Ext(stagedPath)); got != ".ogg" {
		t.Fatalf("staged path extension = %q, want .ogg (path=%q)", got, stagedPath)
	}
	assertStagedPathsInWorkspace(t, workspace, liveMessage)
}

func TestNormalizeInboundPromptMedia_DegradesGracefullyWhenAudioStageFails(t *testing.T) {
	store := media.NewFileMediaStore()
	workspace := t.TempDir()
	srcDir := t.TempDir()

	stagePath := filepath.Join(workspace, inboundAttachmentStageDir)
	if err := os.MkdirAll(filepath.Dir(stagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagePath, []byte("block staging dir creation"), 0o644); err != nil {
		t.Fatal(err)
	}

	audioPath := filepath.Join(srcDir, "voice.ogg")
	if err := os.WriteFile(audioPath, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	audioRef, err := store.Store(audioPath, media.MediaMeta{
		Filename:    "voice.ogg",
		ContentType: "audio/ogg",
	}, "audio-scope")
	if err != nil {
		t.Fatal(err)
	}

	liveMessage, sessionMessage, mediaRefs := normalizeInboundPromptMedia(
		"[Audio]",
		"[Audio]",
		workspace,
		[]string{audioRef},
		store,
	)

	if got, want := mediaRefs, []string{audioRef}; !slices.Equal(got, want) {
		t.Fatalf("normalized media = %v, want %v", got, want)
	}
	if !strings.Contains(liveMessage, "could not be prepared for transcription") {
		t.Fatalf("live message missing graceful failure note: %q", liveMessage)
	}
	if !strings.Contains(sessionMessage, "[Audio attachment could not be prepared for this turn: voice.ogg]") {
		t.Fatalf("session message missing graceful failure note: %q", sessionMessage)
	}

	resolved := resolveMediaRefs([]providers.Message{{Role: "user", Media: mediaRefs}}, store, config.DefaultMaxMediaSize)
	if got := resolved[0].Media; len(got) != 1 || !strings.HasPrefix(got[0], "data:audio/ogg;base64,") {
		t.Fatalf("resolved media = %v, want single audio data URL", got)
	}
}

func TestNormalizeInboundPromptMedia_StagesFileAndVideoAttachments(t *testing.T) {
	store := media.NewFileMediaStore()
	workspace := t.TempDir()
	srcDir := t.TempDir()

	docPath := filepath.Join(srcDir, "slides.pptx")
	if err := os.WriteFile(docPath, []byte("fake-pptx"), 0o644); err != nil {
		t.Fatal(err)
	}
	docRef, err := store.Store(docPath, media.MediaMeta{
		Filename:    "slides.pptx",
		ContentType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}, "doc-scope")
	if err != nil {
		t.Fatal(err)
	}

	videoPath := filepath.Join(srcDir, "clip.mp4")
	if err := os.WriteFile(videoPath, []byte("fake-mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	videoRef, err := store.Store(videoPath, media.MediaMeta{
		Filename:    "clip.mp4",
		ContentType: "video/mp4",
	}, "video-scope")
	if err != nil {
		t.Fatal(err)
	}

	liveMessage, sessionMessage, mediaRefs := normalizeInboundPromptMedia(
		"tell me what is in these files",
		"tell me what is in these files",
		workspace,
		[]string{docRef, videoRef},
		store,
	)

	if len(mediaRefs) != 0 {
		t.Fatalf("expected non-image attachments to be removed from provider media, got %v", mediaRefs)
	}
	if !strings.Contains(liveMessage, "[File attachment available]") {
		t.Fatalf("live message missing file note: %q", liveMessage)
	}
	if !strings.Contains(liveMessage, "skills/office-parse/SKILL.md") {
		t.Fatalf("live message missing office-parse hint: %q", liveMessage)
	}
	if !strings.Contains(liveMessage, "[Video attachment available]") {
		t.Fatalf("live message missing video note: %q", liveMessage)
	}
	if !strings.Contains(liveMessage, "Keep any caption or text in this message as the primary intent signal.") {
		t.Fatalf("live message missing primary intent guidance: %q", liveMessage)
	}
	if strings.Contains(sessionMessage, "Local file:") {
		t.Fatalf("session message should not persist staged paths: %q", sessionMessage)
	}
	if !strings.Contains(sessionMessage, "[File attachment available for this turn: slides.pptx]") {
		t.Fatalf("session message missing file note: %q", sessionMessage)
	}
	if !strings.Contains(sessionMessage, "[Video attachment available for this turn: clip.mp4]") {
		t.Fatalf("session message missing video note: %q", sessionMessage)
	}

	localFileLines := strings.Count(liveMessage, "Local file:")
	if localFileLines != 2 {
		t.Fatalf("expected 2 staged attachment notes, got %d in %q", localFileLines, liveMessage)
	}
	assertStagedPathsInWorkspace(t, workspace, liveMessage)
}

func TestNormalizeInboundPromptMedia_StagesPDFWithPDFSkillHint(t *testing.T) {
	store := media.NewFileMediaStore()
	workspace := t.TempDir()
	srcDir := t.TempDir()

	pdfPath := filepath.Join(srcDir, "report.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	pdfRef, err := store.Store(pdfPath, media.MediaMeta{
		Filename:    "report.pdf",
		ContentType: "application/pdf",
	}, "pdf-scope")
	if err != nil {
		t.Fatal(err)
	}

	liveMessage, _, mediaRefs := normalizeInboundPromptMedia(
		"what does this pdf say",
		"what does this pdf say",
		workspace,
		[]string{pdfRef},
		store,
	)

	if len(mediaRefs) != 0 {
		t.Fatalf("expected pdf attachment to be removed from provider media, got %v", mediaRefs)
	}
	if !strings.Contains(liveMessage, "skills/pdf-parse/SKILL.md") {
		t.Fatalf("live message missing pdf-parse hint: %q", liveMessage)
	}
	assertStagedPathsInWorkspace(t, workspace, liveMessage)
}

func TestProcessMessage_AudioAttachmentPromptsSkillInsteadOfPassingMedia(t *testing.T) {
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
	}

	msgBus := bus.NewMessageBus()
	provider := &captureMessagesProvider{response: "ok"}
	al := NewAgentLoop(cfg, msgBus, provider)

	store := media.NewFileMediaStore()
	al.SetMediaStore(store)

	audioPath := filepath.Join(t.TempDir(), "voice.ogg")
	if err := os.WriteFile(audioPath, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	audioRef, err := store.Store(audioPath, media.MediaMeta{
		Filename:    "voice.ogg",
		ContentType: "audio/ogg",
	}, "whatsapp:chat-1:msg-1")
	if err != nil {
		t.Fatal(err)
	}

	_, err = al.processMessage(context.Background(), bus.InboundMessage{
		Channel:    "whatsapp",
		SenderID:   "whatsapp:user-1",
		ChatID:     "chat-1",
		Content:    "summarize this voice note",
		Media:      []string{audioRef},
		SessionKey: "session-1",
		Peer:       bus.Peer{Kind: "direct", ID: "user-1"},
	})
	if err != nil {
		t.Fatalf("processMessage failed: %v", err)
	}
	if len(provider.calls) == 0 {
		t.Fatal("expected provider to receive a prompt")
	}

	userMessage := provider.calls[0][len(provider.calls[0])-1]
	if userMessage.Role != "user" {
		t.Fatalf("last provider message role = %q, want user", userMessage.Role)
	}
	if len(userMessage.Media) != 0 {
		t.Fatalf("audio should not be passed as provider media, got %v", userMessage.Media)
	}
	if !strings.Contains(userMessage.Content, "summarize this voice note") {
		t.Fatalf("provider prompt lost original caption: %q", userMessage.Content)
	}
	if !strings.Contains(userMessage.Content, "skills/stt/SKILL.md") {
		t.Fatalf("provider prompt missing stt hint: %q", userMessage.Content)
	}

	stagedPath := extractPromptLineValue(userMessage.Content, "Local file:")
	if stagedPath == "" {
		t.Fatalf("provider prompt missing staged audio path: %q", userMessage.Content)
	}
	if _, err := os.Stat(stagedPath); err != nil {
		t.Fatalf("staged audio path missing on disk: %v", err)
	}
}

func TestProcessMessage_VoiceNoteUsesTranscriptAsUserMessage(t *testing.T) {
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
	}

	msgBus := bus.NewMessageBus()
	provider := &captureMessagesProvider{response: "ok"}
	al := NewAgentLoop(cfg, msgBus, provider)

	store := media.NewFileMediaStore()
	al.SetMediaStore(store)

	transcriber := &fakeVoiceNoteTranscriber{transcript: "hello from the voice note"}
	al.setVoiceNoteTranscriber(transcriber)

	audioPath := filepath.Join(t.TempDir(), "voice.ogg")
	if err := os.WriteFile(audioPath, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	audioRef, err := store.Store(audioPath, media.MediaMeta{
		Filename:    "voice.ogg",
		ContentType: "audio/ogg",
	}, "telegram:chat-1:msg-1")
	if err != nil {
		t.Fatal(err)
	}

	msg := bus.InboundMessage{
		Channel:  "telegram",
		SenderID: "telegram:user-1",
		Sender: bus.SenderInfo{
			Platform:   "telegram",
			PlatformID: "user-1",
		},
		ChatID:   "chat-1",
		Content:  "[voice]",
		Media:    []string{audioRef},
		Peer:     bus.Peer{Kind: "direct", ID: "user-1"},
		Metadata: map[string]string{bus.MetadataMessageSubtype: bus.MessageSubtypeVoiceNote},
	}

	_, err = al.processMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("processMessage failed: %v", err)
	}
	if len(provider.calls) == 0 {
		t.Fatal("expected provider to receive a prompt")
	}

	userMessage := provider.calls[0][len(provider.calls[0])-1]
	if got := userMessage.Content; got != transcriber.transcript {
		t.Fatalf("user message content = %q, want %q", got, transcriber.transcript)
	}
	if len(userMessage.Media) != 0 {
		t.Fatalf("voice note audio should not be passed as provider media, got %v", userMessage.Media)
	}
	if strings.Contains(userMessage.Content, "skills/stt/SKILL.md") {
		t.Fatalf("voice note prompt should not include stt skill hint after successful transcription: %q", userMessage.Content)
	}
	if len(transcriber.calls) != 1 {
		t.Fatalf("transcriber call count = %d, want 1", len(transcriber.calls))
	}

	sessionKey := al.registry.ResolveRoute(routing.RouteInput{
		Channel: "telegram",
		Peer:    &routing.RoutePeer{Kind: "direct", ID: "user-1"},
	}).SessionKey
	history := al.registry.GetDefaultAgent().Sessions.GetHistory(sessionKey)
	if len(history) < 2 {
		t.Fatalf("history length = %d, want at least 2", len(history))
	}
	if got := history[0].Content; got != transcriber.transcript {
		t.Fatalf("stored user history = %q, want %q", got, transcriber.transcript)
	}
}

func TestProcessMessage_VoiceNoteKeepsCaptionAsContext(t *testing.T) {
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
	}

	msgBus := bus.NewMessageBus()
	provider := &captureMessagesProvider{response: "ok"}
	al := NewAgentLoop(cfg, msgBus, provider)

	store := media.NewFileMediaStore()
	al.SetMediaStore(store)
	al.setVoiceNoteTranscriber(&fakeVoiceNoteTranscriber{transcript: "spoken words"})

	audioPath := filepath.Join(t.TempDir(), "voice.ogg")
	if err := os.WriteFile(audioPath, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	audioRef, err := store.Store(audioPath, media.MediaMeta{
		Filename:    "voice.ogg",
		ContentType: "audio/ogg",
	}, "telegram:chat-1:msg-2")
	if err != nil {
		t.Fatal(err)
	}

	_, err = al.processMessage(context.Background(), bus.InboundMessage{
		Channel:  "telegram",
		SenderID: "telegram:user-1",
		Sender: bus.SenderInfo{
			Platform:   "telegram",
			PlatformID: "user-1",
		},
		ChatID:   "chat-1",
		Content:  "please keep this in mind\n[voice]",
		Media:    []string{audioRef},
		Peer:     bus.Peer{Kind: "direct", ID: "user-1"},
		Metadata: map[string]string{bus.MetadataMessageSubtype: bus.MessageSubtypeVoiceNote},
	})
	if err != nil {
		t.Fatalf("processMessage failed: %v", err)
	}

	userMessage := provider.calls[0][len(provider.calls[0])-1]
	if !strings.HasPrefix(userMessage.Content, "spoken words") {
		t.Fatalf("voice note transcript should lead the user message, got %q", userMessage.Content)
	}
	if !strings.Contains(userMessage.Content, "[Accompanying text from the same message]\nplease keep this in mind") {
		t.Fatalf("voice note caption context missing from user message: %q", userMessage.Content)
	}
	if strings.Contains(userMessage.Content, "[voice]") {
		t.Fatalf("voice placeholder should be removed after transcription: %q", userMessage.Content)
	}
}

func TestProcessMessage_VoiceNoteFallsBackToAttachmentPromptOnTranscriptionFailure(t *testing.T) {
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
	}

	msgBus := bus.NewMessageBus()
	provider := &captureMessagesProvider{response: "ok"}
	al := NewAgentLoop(cfg, msgBus, provider)

	store := media.NewFileMediaStore()
	al.SetMediaStore(store)
	al.setVoiceNoteTranscriber(&fakeVoiceNoteTranscriber{err: errors.New("transcription failed")})

	audioPath := filepath.Join(t.TempDir(), "voice.ogg")
	if err := os.WriteFile(audioPath, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	audioRef, err := store.Store(audioPath, media.MediaMeta{
		Filename:    "voice.ogg",
		ContentType: "audio/ogg",
	}, "telegram:chat-1:msg-3")
	if err != nil {
		t.Fatal(err)
	}

	_, err = al.processMessage(context.Background(), bus.InboundMessage{
		Channel:  "telegram",
		SenderID: "telegram:user-1",
		Sender: bus.SenderInfo{
			Platform:   "telegram",
			PlatformID: "user-1",
		},
		ChatID:   "chat-1",
		Content:  "[voice]",
		Media:    []string{audioRef},
		Peer:     bus.Peer{Kind: "direct", ID: "user-1"},
		Metadata: map[string]string{bus.MetadataMessageSubtype: bus.MessageSubtypeVoiceNote},
	})
	if err != nil {
		t.Fatalf("processMessage failed: %v", err)
	}

	userMessage := provider.calls[0][len(provider.calls[0])-1]
	if !strings.Contains(userMessage.Content, "skills/stt/SKILL.md") {
		t.Fatalf("voice note fallback should use stt skill hint, got %q", userMessage.Content)
	}
	stagedPath := extractPromptLineValue(userMessage.Content, "Local file:")
	if stagedPath == "" {
		t.Fatalf("voice note fallback missing staged local file path: %q", userMessage.Content)
	}
}

func TestProcessMessage_GroupVoiceNotePersistsAttributedTranscript(t *testing.T) {
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
	}

	msgBus := bus.NewMessageBus()
	provider := &captureMessagesProvider{response: "ok"}
	al := NewAgentLoop(cfg, msgBus, provider)

	store := media.NewFileMediaStore()
	al.SetMediaStore(store)
	al.setVoiceNoteTranscriber(&fakeVoiceNoteTranscriber{transcript: "hello group"})

	audioPath := filepath.Join(t.TempDir(), "voice.ogg")
	if err := os.WriteFile(audioPath, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	audioRef, err := store.Store(audioPath, media.MediaMeta{
		Filename:    "voice.ogg",
		ContentType: "audio/ogg",
	}, "telegram:group-1:msg-1")
	if err != nil {
		t.Fatal(err)
	}

	_, err = al.processMessage(context.Background(), bus.InboundMessage{
		Channel:  "telegram",
		SenderID: "telegram:user-1",
		Sender: bus.SenderInfo{
			Platform:    "telegram",
			PlatformID:  "user-1",
			DisplayName: "Alice",
		},
		ChatID:   "group-1",
		Content:  "[voice]",
		Media:    []string{audioRef},
		Peer:     bus.Peer{Kind: "group", ID: "group-1"},
		Metadata: map[string]string{bus.MetadataMessageSubtype: bus.MessageSubtypeVoiceNote},
	})
	if err != nil {
		t.Fatalf("processMessage failed: %v", err)
	}

	userMessage := provider.calls[0][len(provider.calls[0])-1]
	if got := userMessage.Content; got != "[From: Alice] hello group" {
		t.Fatalf("group voice note content = %q, want %q", got, "[From: Alice] hello group")
	}
}

func TestProcessMessage_FileAttachmentPromptsLocalFileInsteadOfPassingMedia(t *testing.T) {
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
	}

	msgBus := bus.NewMessageBus()
	provider := &captureMessagesProvider{response: "ok"}
	al := NewAgentLoop(cfg, msgBus, provider)

	store := media.NewFileMediaStore()
	al.SetMediaStore(store)

	filePath := filepath.Join(t.TempDir(), "report.docx")
	if err := os.WriteFile(filePath, []byte("fake docx"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileRef, err := store.Store(filePath, media.MediaMeta{
		Filename:    "report.docx",
		ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	}, "whatsapp:chat-1:msg-2")
	if err != nil {
		t.Fatal(err)
	}

	_, err = al.processMessage(context.Background(), bus.InboundMessage{
		Channel:    "whatsapp",
		SenderID:   "whatsapp:user-1",
		ChatID:     "chat-1",
		Content:    "extract the main points",
		Media:      []string{fileRef},
		SessionKey: "session-1",
		Peer:       bus.Peer{Kind: "direct", ID: "user-1"},
	})
	if err != nil {
		t.Fatalf("processMessage failed: %v", err)
	}
	if len(provider.calls) == 0 {
		t.Fatal("expected provider to receive a prompt")
	}

	userMessage := provider.calls[0][len(provider.calls[0])-1]
	if userMessage.Role != "user" {
		t.Fatalf("last provider message role = %q, want user", userMessage.Role)
	}
	if len(userMessage.Media) != 0 {
		t.Fatalf("file attachment should not be passed as provider media, got %v", userMessage.Media)
	}
	if !strings.Contains(userMessage.Content, "extract the main points") {
		t.Fatalf("provider prompt lost original caption: %q", userMessage.Content)
	}
	if !strings.Contains(userMessage.Content, "[File attachment available]") {
		t.Fatalf("provider prompt missing file note: %q", userMessage.Content)
	}
	if !strings.Contains(userMessage.Content, "skills/office-parse/SKILL.md") {
		t.Fatalf("provider prompt missing office-parse hint: %q", userMessage.Content)
	}
	if strings.Contains(userMessage.Content, "skills/stt/SKILL.md") {
		t.Fatalf("provider prompt should not mention stt for generic files: %q", userMessage.Content)
	}

	stagedPath := extractPromptLineValue(userMessage.Content, "Local file:")
	if stagedPath == "" {
		t.Fatalf("provider prompt missing staged file path: %q", userMessage.Content)
	}
	if _, err := os.Stat(stagedPath); err != nil {
		t.Fatalf("staged file path missing on disk: %v", err)
	}
}

func TestProcessMessage_KeepsOwnedInboundMediaUntilTTL(t *testing.T) {
	al, _, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()

	store := media.NewFileMediaStore()
	al.SetMediaStore(store)

	dir := t.TempDir()
	path := filepath.Join(dir, "inbound.png")
	pngHeader := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02,
		0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE,
	}
	if err := os.WriteFile(path, pngHeader, 0o644); err != nil {
		t.Fatal(err)
	}

	scope := "telegram:chat-1:msg-1"
	ref, err := store.Store(path, media.MediaMeta{Filename: "inbound.png", Owned: true}, scope)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	_, err = al.processMessage(context.Background(), bus.InboundMessage{
		Channel:    "telegram",
		SenderID:   "telegram:user-1",
		ChatID:     "chat-1",
		Content:    "describe the image",
		Media:      []string{ref},
		MediaScope: scope,
		SessionKey: "session-1",
	})
	if err != nil {
		t.Fatalf("processMessage failed: %v", err)
	}

	resolved, err := store.Resolve(ref)
	if err != nil {
		t.Fatalf("owned media ref should remain available until TTL cleanup: %v", err)
	}
	if resolved != path {
		t.Fatalf("Resolve returned %q, want %q", resolved, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("owned temp file should still exist before TTL cleanup: %v", err)
	}
}

func TestProcessMessage_KeepsUnownedInboundMediaUntilTTL(t *testing.T) {
	al, _, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()

	store := media.NewFileMediaStore()
	al.SetMediaStore(store)

	dir := t.TempDir()
	path := filepath.Join(dir, "original.png")
	pngHeader := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02,
		0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE,
	}
	if err := os.WriteFile(path, pngHeader, 0o644); err != nil {
		t.Fatal(err)
	}

	scope := "telegram:chat-1:msg-2"
	ref, err := store.Store(path, media.MediaMeta{Filename: "original.png"}, scope)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	_, err = al.processMessage(context.Background(), bus.InboundMessage{
		Channel:    "telegram",
		SenderID:   "telegram:user-1",
		ChatID:     "chat-1",
		Content:    "describe the image",
		Media:      []string{ref},
		MediaScope: scope,
		SessionKey: "session-1",
	})
	if err != nil {
		t.Fatalf("processMessage failed: %v", err)
	}

	resolved, err := store.Resolve(ref)
	if err != nil {
		t.Fatalf("unowned media ref should remain available until TTL cleanup: %v", err)
	}
	if resolved != path {
		t.Fatalf("Resolve returned %q, want %q", resolved, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("unowned source file should remain on disk: %v", err)
	}
}

func assertStagedPathsInWorkspace(t *testing.T, workspace, content string) {
	t.Helper()

	for _, line := range strings.Split(content, "\n") {
		stagedPath, ok := strings.CutPrefix(line, "Local file:")
		if !ok {
			continue
		}
		stagedPath = strings.TrimSpace(stagedPath)
		if stagedPath == "" {
			t.Fatalf("empty staged path in line %q", line)
		}
		rel, err := filepath.Rel(workspace, stagedPath)
		if err != nil {
			t.Fatalf("filepath.Rel failed: %v", err)
		}
		if !filepath.IsLocal(rel) {
			t.Fatalf("staged path %q should be inside workspace %q", stagedPath, workspace)
		}
		if _, err := os.Stat(stagedPath); err != nil {
			t.Fatalf("staged attachment missing: %v", err)
		}
	}
}
