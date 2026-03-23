package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type captureProvider struct {
	response     string
	lastMessages []providers.Message
}

func (p *captureProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	p.lastMessages = append([]providers.Message(nil), messages...)
	return &providers.LLMResponse{Content: p.response}, nil
}

func (p *captureProvider) GetDefaultModel() string {
	return "capture-model"
}

type scriptedCaptureProvider struct {
	responses    []*providers.LLMResponse
	errors       []error
	lastMessages []providers.Message
	allCalls     [][]providers.Message
}

func (p *scriptedCaptureProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	cloned := append([]providers.Message(nil), messages...)
	p.lastMessages = cloned
	p.allCalls = append(p.allCalls, cloned)

	index := len(p.allCalls) - 1
	if index < len(p.errors) && p.errors[index] != nil {
		return nil, p.errors[index]
	}
	if index < len(p.responses) && p.responses[index] != nil {
		return p.responses[index], nil
	}
	return &providers.LLMResponse{Content: "ok"}, nil
}

func (p *scriptedCaptureProvider) GetDefaultModel() string {
	return "scripted-capture-model"
}

func dailyNotePath(workspace string) string {
	today := time.Now().Format("20060102")
	return filepath.Join(workspace, "memory", today[:6], today+".md")
}

func TestProcessMessage_NewCommandRotatesSessionAndLogsDailyNotes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-new-session-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
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

	provider := &captureProvider{response: "assistant reply"}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)

	ctx := context.Background()
	if _, err := al.processMessage(ctx, bus.InboundMessage{
		Channel:  "test",
		SenderID: "u1",
		ChatID:   "chat1",
		Content:  "hello",
	}); err != nil {
		t.Fatalf("first message failed: %v", err)
	}

	resp, err := al.processMessage(ctx, bus.InboundMessage{
		Channel:  "test",
		SenderID: "u1",
		ChatID:   "chat1",
		Content:  "/new",
	})
	if err != nil {
		t.Fatalf("/new failed: %v", err)
	}
	const newSessionPrefix = "Started a new session. New session key: "
	if !strings.HasPrefix(resp, newSessionPrefix) {
		t.Fatalf("unexpected /new response: %q", resp)
	}
	newSessionKey := strings.TrimSpace(strings.TrimPrefix(resp, newSessionPrefix))
	if newSessionKey == "" {
		t.Fatalf("missing rotated session key in response: %q", resp)
	}

	agent := al.registry.GetDefaultAgent()
	if agent == nil {
		t.Fatal("default agent is nil")
	}
	state, err := agent.StateStore.LoadState()
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	_, rel := requireRelationshipForIdentifier(t, state, "test", "u1")
	if rel.LastSessionKey != newSessionKey {
		t.Fatalf("relationship LastSessionKey = %q, want %q", rel.LastSessionKey, newSessionKey)
	}

	if _, err := al.processMessage(ctx, bus.InboundMessage{
		Channel:  "test",
		SenderID: "u1",
		ChatID:   "chat1",
		Content:  "follow up",
	}); err != nil {
		t.Fatalf("message after /new failed: %v", err)
	}

	sessionsDir := filepath.Join(tmpDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		t.Fatalf("read sessions dir: %v", err)
	}
	jsonCount := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			jsonCount++
		}
	}
	if jsonCount < 2 {
		t.Fatalf("expected at least 2 session files after /new rotation, got %d", jsonCount)
	}

	dailyRaw, err := os.ReadFile(dailyNotePath(tmpDir))
	if err != nil {
		t.Fatalf("read daily note: %v", err)
	}
	dailyText := string(dailyRaw)
	if !strings.Contains(dailyText, `"event":"session_closed_by_new"`) {
		t.Fatalf("daily note missing session_closed_by_new event:\n%s", dailyText)
	}
	if !strings.Contains(dailyText, `"role":"user","content":"hello"`) {
		t.Fatalf("daily note missing user line from closed session:\n%s", dailyText)
	}
	if !strings.Contains(dailyText, `"role":"assistant","content":"assistant reply"`) {
		t.Fatalf("daily note missing assistant line from closed session:\n%s", dailyText)
	}
}

func TestForceCompression_LogsOnlyDroppedSegment(t *testing.T) {
	al, _, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()

	agent := al.registry.GetDefaultAgent()
	if agent == nil {
		t.Fatal("default agent is nil")
	}

	sessionKey := "agent:main:compression-test"
	agent.Sessions.GetOrCreate(sessionKey)
	agent.Sessions.SetHistory(sessionKey, []providers.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "u3"},
		{Role: "assistant", Content: "a3"},
	})

	al.forceCompression(agent, sessionKey, "test", "chat1")

	dailyRaw, err := os.ReadFile(dailyNotePath(agent.Workspace))
	if err != nil {
		t.Fatalf("read daily note: %v", err)
	}
	dailyText := string(dailyRaw)

	if !strings.Contains(dailyText, `"event":"pre_compression"`) {
		t.Fatalf("daily note missing pre_compression event:\n%s", dailyText)
	}
	if !strings.Contains(dailyText, `"role":"assistant","content":"a1"`) {
		t.Fatalf("daily note missing dropped assistant message a1:\n%s", dailyText)
	}
	if !strings.Contains(dailyText, `"role":"user","content":"u2"`) {
		t.Fatalf("daily note missing dropped user message u2:\n%s", dailyText)
	}
	if strings.Contains(dailyText, `"content":"a2"`) {
		t.Fatalf("daily note should not include kept segment message a2:\n%s", dailyText)
	}
}

func TestAutoRecallInjectsMemoryContextWhenEnabled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-auto-recall-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.MkdirAll(filepath.Join(tmpDir, "memory"), 0o755); err != nil {
		t.Fatalf("create memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte("user_timezone: Asia/Hong_Kong"), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	provider := &scriptedCaptureProvider{
		responses: []*providers.LLMResponse{
			{Content: `{"keywords":["timezone","preference"]}`},
			{Content: "ok"},
		},
	}
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				MemorySearch: config.MemorySearchConfig{
					AutoRecall: config.MemoryAutoRecallConfig{
						Enabled:  true,
						TopK:     2,
						MaxChars: 800,
					},
				},
			},
		},
	}

	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	if _, err := al.processMessage(context.Background(), bus.InboundMessage{
		Channel:  "test",
		SenderID: "u1",
		ChatID:   "chat1",
		Content:  "What is my timezone preference?",
	}); err != nil {
		t.Fatalf("process message failed: %v", err)
	}

	if len(provider.allCalls) < 2 {
		t.Fatalf("expected keyword extraction and main agent calls, got %d", len(provider.allCalls))
	}
	extractionCall := provider.allCalls[0]
	if len(extractionCall) != 2 || extractionCall[0].Role != "system" || extractionCall[1].Role != "user" {
		t.Fatalf("unexpected extraction call shape: %+v", extractionCall)
	}
	if !strings.Contains(extractionCall[0].Content, autoRecallKeywordExtractorPromptTag) {
		t.Fatalf("keyword extractor prompt missing tag: %s", extractionCall[0].Content)
	}

	system := provider.allCalls[len(provider.allCalls)-1][0].Content
	if !strings.Contains(system, "RELEVANT_MEMORY (keyword recall)") {
		t.Fatalf("system prompt missing auto-recall block:\n%s", system)
	}
	if !strings.Contains(system, "Asia/Hong_Kong") {
		t.Fatalf("system prompt missing recalled snippet:\n%s", system)
	}
}

func TestAutoRecallInjectsMemoryContextFromLLMKeywords(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "memory"), 0o755); err != nil {
		t.Fatalf("create memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte("北海道 行程規劃,景點偏好"), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	provider := &scriptedCaptureProvider{
		responses: []*providers.LLMResponse{
			{Content: `{"keywords":["北海道","行程"]}`},
			{Content: "ok"},
		},
	}
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				MemorySearch: config.MemorySearchConfig{
					AutoRecall: config.MemoryAutoRecallConfig{
						Enabled:  true,
						TopK:     2,
						MaxChars: 800,
					},
				},
			},
		},
	}

	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	if _, err := al.processMessage(context.Background(), bus.InboundMessage{
		Channel:  "test",
		SenderID: "u1",
		ChatID:   "chat1",
		Content:  "飛鼠仔好關心你北海道嘅行程",
	}); err != nil {
		t.Fatalf("process message failed: %v", err)
	}

	system := provider.allCalls[len(provider.allCalls)-1][0].Content
	if !strings.Contains(system, "RELEVANT_MEMORY (keyword recall)") {
		t.Fatalf("system prompt missing auto-recall block:\n%s", system)
	}
	if !strings.Contains(system, "北海道") {
		t.Fatalf("system prompt missing recalled snippet:\n%s", system)
	}
}

func TestAutoRecallInjectsMemoryContextFromMultilingualKeywords(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "memory"), 0o755); err != nil {
		t.Fatalf("create memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte("name: Sunny\ntrip destination: Hokkaido"), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	provider := &scriptedCaptureProvider{
		responses: []*providers.LLMResponse{
			{Content: `{"keywords":["\u5317\u6d77\u9053","\u884c\u7a0b","Hokkaido","trip itinerary"]}`},
			{Content: "ok"},
		},
	}
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				MemorySearch: config.MemorySearchConfig{
					AutoRecall: config.MemoryAutoRecallConfig{
						Enabled:  true,
						TopK:     2,
						MaxChars: 800,
					},
				},
			},
		},
	}

	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	if _, err := al.processMessage(context.Background(), bus.InboundMessage{
		Channel:  "test",
		SenderID: "u1",
		ChatID:   "chat1",
		Content:  "\u98db\u9f20\u4ed4\u597d\u95dc\u5fc3\u4f60\u5317\u6d77\u9053\u5605\u884c\u7a0b",
	}); err != nil {
		t.Fatalf("process message failed: %v", err)
	}

	extractionCall := provider.allCalls[0]
	if !strings.Contains(extractionCall[0].Content, "Add likely English aliases or translations") {
		t.Fatalf("keyword extractor prompt missing multilingual instruction: %s", extractionCall[0].Content)
	}

	system := provider.allCalls[len(provider.allCalls)-1][0].Content
	if !strings.Contains(system, "Hokkaido") {
		t.Fatalf("system prompt missing multilingual recalled snippet:\n%s", system)
	}
}

func TestAutoRecallSkipsWhenKeywordExtractionReturnsInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "memory"), 0o755); err != nil {
		t.Fatalf("create memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte("user_timezone: Asia/Hong_Kong"), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	provider := &scriptedCaptureProvider{
		responses: []*providers.LLMResponse{
			{Content: `not json`},
			{Content: "ok"},
		},
	}
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				MemorySearch: config.MemorySearchConfig{
					AutoRecall: config.MemoryAutoRecallConfig{
						Enabled:  true,
						TopK:     2,
						MaxChars: 800,
					},
				},
			},
		},
	}

	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	if _, err := al.processMessage(context.Background(), bus.InboundMessage{
		Channel:  "test",
		SenderID: "u1",
		ChatID:   "chat1",
		Content:  "What is my timezone preference?",
	}); err != nil {
		t.Fatalf("process message failed: %v", err)
	}

	system := provider.allCalls[len(provider.allCalls)-1][0].Content
	if strings.Contains(system, "RELEVANT_MEMORY (keyword recall)") {
		t.Fatalf("system prompt should not include auto-recall block on invalid JSON:\n%s", system)
	}
}

func TestAutoRecallSkipsWhenKeywordExtractionFails(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "memory"), 0o755); err != nil {
		t.Fatalf("create memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte("user_timezone: Asia/Hong_Kong"), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	provider := &scriptedCaptureProvider{
		errors: []error{errors.New("provider unavailable")},
		responses: []*providers.LLMResponse{
			nil,
			{Content: "ok"},
		},
	}
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				MemorySearch: config.MemorySearchConfig{
					AutoRecall: config.MemoryAutoRecallConfig{
						Enabled:  true,
						TopK:     2,
						MaxChars: 800,
					},
				},
			},
		},
	}

	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	if _, err := al.processMessage(context.Background(), bus.InboundMessage{
		Channel:  "test",
		SenderID: "u1",
		ChatID:   "chat1",
		Content:  "What is my timezone preference?",
	}); err != nil {
		t.Fatalf("process message failed: %v", err)
	}

	system := provider.allCalls[len(provider.allCalls)-1][0].Content
	if strings.Contains(system, "RELEVANT_MEMORY (keyword recall)") {
		t.Fatalf("system prompt should not include auto-recall block on provider error:\n%s", system)
	}
}

func TestAutoRecallSkipsWhenKeywordExtractionReturnsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "memory"), 0o755); err != nil {
		t.Fatalf("create memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte("user_timezone: Asia/Hong_Kong"), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	provider := &scriptedCaptureProvider{
		responses: []*providers.LLMResponse{
			{Content: `{"keywords":[]}`},
			{Content: "ok"},
		},
	}
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				MemorySearch: config.MemorySearchConfig{
					AutoRecall: config.MemoryAutoRecallConfig{
						Enabled:  true,
						TopK:     2,
						MaxChars: 800,
					},
				},
			},
		},
	}

	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	if _, err := al.processMessage(context.Background(), bus.InboundMessage{
		Channel:  "test",
		SenderID: "u1",
		ChatID:   "chat1",
		Content:  "What is my timezone preference?",
	}); err != nil {
		t.Fatalf("process message failed: %v", err)
	}

	system := provider.allCalls[len(provider.allCalls)-1][0].Content
	if strings.Contains(system, "RELEVANT_MEMORY (keyword recall)") {
		t.Fatalf("system prompt should not include auto-recall block on empty keywords:\n%s", system)
	}
}

func TestProcessMessage_NewCommandRotatesExplicitSessionKey(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-new-explicit-session-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
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

	provider := &captureProvider{response: "assistant reply"}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)

	ctx := context.Background()
	explicitSessionKey := "agent:main:manual-session"

	if _, err := al.processMessage(ctx, bus.InboundMessage{
		Channel:    "test",
		SenderID:   "u1",
		ChatID:     "chat1",
		SessionKey: explicitSessionKey,
		Content:    "hello",
	}); err != nil {
		t.Fatalf("first message failed: %v", err)
	}

	resp, err := al.processMessage(ctx, bus.InboundMessage{
		Channel:    "test",
		SenderID:   "u1",
		ChatID:     "chat1",
		SessionKey: explicitSessionKey,
		Content:    "/new",
	})
	if err != nil {
		t.Fatalf("/new failed: %v", err)
	}

	const prefix = "Started a new session. New session key: "
	if !strings.HasPrefix(resp, prefix) {
		t.Fatalf("unexpected /new response: %q", resp)
	}
	newSessionKey := strings.TrimSpace(strings.TrimPrefix(resp, prefix))
	if newSessionKey == "" || newSessionKey == explicitSessionKey {
		t.Fatalf("invalid new session key: %q", newSessionKey)
	}

	if _, err := al.processMessage(ctx, bus.InboundMessage{
		Channel:    "test",
		SenderID:   "u1",
		ChatID:     "chat1",
		SessionKey: explicitSessionKey,
		Content:    "follow up",
	}); err != nil {
		t.Fatalf("message after /new failed: %v", err)
	}

	agent := al.registry.GetDefaultAgent()
	if agent == nil {
		t.Fatal("default agent is nil")
	}

	oldHistory := agent.Sessions.GetHistory(explicitSessionKey)
	for _, msg := range oldHistory {
		if msg.Role == "user" && msg.Content == "follow up" {
			t.Fatalf("follow-up message should not stay in original session %q", explicitSessionKey)
		}
	}

	newHistory := agent.Sessions.GetHistory(newSessionKey)
	if len(newHistory) == 0 {
		t.Fatalf("expected new session history for %q", newSessionKey)
	}
	foundFollowUp := false
	for _, msg := range newHistory {
		if msg.Role == "user" && msg.Content == "follow up" {
			foundFollowUp = true
			break
		}
	}
	if !foundFollowUp {
		t.Fatalf("new session history missing follow-up message: %+v", newHistory)
	}
}

func TestProcessMessage_NewCommandRoutesToLatestSessionAfterMultipleRotations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-new-multi-rotation-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
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

	provider := &captureProvider{response: "assistant reply"}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)

	ctx := context.Background()
	explicitSessionKey := "agent:main:manual-session"

	if _, err := al.processMessage(ctx, bus.InboundMessage{
		Channel:    "test",
		SenderID:   "u1",
		ChatID:     "chat1",
		SessionKey: explicitSessionKey,
		Content:    "hello",
	}); err != nil {
		t.Fatalf("first message failed: %v", err)
	}

	const prefix = "Started a new session. New session key: "

	firstResp, err := al.processMessage(ctx, bus.InboundMessage{
		Channel:    "test",
		SenderID:   "u1",
		ChatID:     "chat1",
		SessionKey: explicitSessionKey,
		Content:    "/new",
	})
	if err != nil {
		t.Fatalf("first /new failed: %v", err)
	}
	if !strings.HasPrefix(firstResp, prefix) {
		t.Fatalf("unexpected first /new response: %q", firstResp)
	}
	firstRotatedKey := strings.TrimSpace(strings.TrimPrefix(firstResp, prefix))

	if _, err := al.processMessage(ctx, bus.InboundMessage{
		Channel:    "test",
		SenderID:   "u1",
		ChatID:     "chat1",
		SessionKey: explicitSessionKey,
		Content:    "first follow up",
	}); err != nil {
		t.Fatalf("first follow-up failed: %v", err)
	}

	secondResp, err := al.processMessage(ctx, bus.InboundMessage{
		Channel:    "test",
		SenderID:   "u1",
		ChatID:     "chat1",
		SessionKey: explicitSessionKey,
		Content:    "/new",
	})
	if err != nil {
		t.Fatalf("second /new failed: %v", err)
	}
	if !strings.HasPrefix(secondResp, prefix) {
		t.Fatalf("unexpected second /new response: %q", secondResp)
	}
	secondRotatedKey := strings.TrimSpace(strings.TrimPrefix(secondResp, prefix))
	if secondRotatedKey == "" || secondRotatedKey == firstRotatedKey {
		t.Fatalf("expected second rotated key to differ from first, got first=%q second=%q", firstRotatedKey, secondRotatedKey)
	}

	if _, err := al.processMessage(ctx, bus.InboundMessage{
		Channel:    "test",
		SenderID:   "u1",
		ChatID:     "chat1",
		SessionKey: explicitSessionKey,
		Content:    "second follow up",
	}); err != nil {
		t.Fatalf("second follow-up failed: %v", err)
	}

	agent := al.registry.GetDefaultAgent()
	if agent == nil {
		t.Fatal("default agent is nil")
	}

	firstHistory := agent.Sessions.GetHistory(firstRotatedKey)
	foundFirstFollowUp := false
	for _, msg := range firstHistory {
		if msg.Role == "user" && msg.Content == "first follow up" {
			foundFirstFollowUp = true
		}
		if msg.Role == "user" && msg.Content == "second follow up" {
			t.Fatalf("second follow-up should not stay in previous rotated session %q", firstRotatedKey)
		}
	}
	if !foundFirstFollowUp {
		t.Fatalf("first rotated session missing first follow-up: %+v", firstHistory)
	}

	secondHistory := agent.Sessions.GetHistory(secondRotatedKey)
	foundSecondFollowUp := false
	for _, msg := range secondHistory {
		if msg.Role == "user" && msg.Content == "second follow up" {
			foundSecondFollowUp = true
			break
		}
	}
	if !foundSecondFollowUp {
		t.Fatalf("latest rotated session missing second follow-up: %+v", secondHistory)
	}
}

func TestAppendDailyLogJSONL_RetriesAfterWriteFailure(t *testing.T) {
	al, _, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()

	agent := al.registry.GetDefaultAgent()
	if agent == nil {
		t.Fatal("default agent is nil")
	}

	memoryPath := filepath.Join(agent.Workspace, "memory")
	if err := os.RemoveAll(memoryPath); err != nil {
		t.Fatalf("remove memory path: %v", err)
	}
	if err := os.WriteFile(memoryPath, []byte("blocked"), 0o644); err != nil {
		t.Fatalf("create blocking memory file: %v", err)
	}

	sessionKey := "agent:main:daily-log-retry"
	segment := []providers.Message{{Role: "user", Content: "u1"}, {Role: "assistant", Content: "a1"}}
	dedupeKey := buildDailyLogDedupeKey(sessionKey, dailyLogEventPreCompression, filterUserAssistantMessages(segment))

	al.appendDailyLogJSONL(agent, sessionKey, "test", "chat1", dailyLogEventPreCompression, segment)
	if _, loaded := al.dailyLogDedupe.Load(dedupeKey); loaded {
		t.Fatal("dedupe key should be cleared after failed daily log append")
	}

	if err := os.Remove(memoryPath); err != nil {
		t.Fatalf("remove blocking memory file: %v", err)
	}
	if err := os.MkdirAll(memoryPath, 0o755); err != nil {
		t.Fatalf("recreate memory directory: %v", err)
	}

	al.appendDailyLogJSONL(agent, sessionKey, "test", "chat1", dailyLogEventPreCompression, segment)

	dailyRaw, err := os.ReadFile(dailyNotePath(agent.Workspace))
	if err != nil {
		t.Fatalf("read daily note after retry: %v", err)
	}
	if !strings.Contains(string(dailyRaw), `"event":"pre_compression"`) {
		t.Fatalf("daily note missing pre_compression after retry:\n%s", string(dailyRaw))
	}
}
