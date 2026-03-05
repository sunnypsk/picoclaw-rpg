package agent

import (
	"context"
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
	if !strings.Contains(resp, "Started a new session") {
		t.Fatalf("unexpected /new response: %q", resp)
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

	provider := &captureProvider{response: "ok"}
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

	if len(provider.lastMessages) == 0 {
		t.Fatal("provider did not receive messages")
	}
	system := provider.lastMessages[0].Content
	if !strings.Contains(system, "RELEVANT_MEMORY (keyword recall)") {
		t.Fatalf("system prompt missing auto-recall block:\n%s", system)
	}
	if !strings.Contains(system, "Asia/Hong_Kong") {
		t.Fatalf("system prompt missing recalled snippet:\n%s", system)
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
