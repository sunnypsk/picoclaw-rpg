package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/gamemode/turtlesoup"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type turtleSoupProvider struct {
	response string
	calls    [][]providers.Message
}

func (p *turtleSoupProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	cloned := make([]providers.Message, len(messages))
	copy(cloned, messages)
	p.calls = append(p.calls, cloned)
	return &providers.LLMResponse{Content: p.response}, nil
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
	al.turtleSoup = turtlesoup.NewEngine(
		turtlesoup.NewStore(filepath.Join(root, "games", "turtle_soup")),
		[]turtlesoup.Puzzle{{
			ID:       "test",
			Surface:  "一名男子每天搭電梯到十樓。某天電梯停在八樓時，他突然哭了。",
			Solution: "男子是視障者，平日妻子會在八樓進電梯陪他並確認十樓按鍵。",
			Hints:    []string{"八樓平常會有固定的人出現。"},
		}},
	)
	return al, cfg
}

func TestTurtleSoupStartBypassesNormalModelAndStoresVisibleHistoryOnly(t *testing.T) {
	provider := &turtleSoupProvider{response: `{"kind":"question","label":"yes"}`}
	al, cfg := newTurtleSoupTestLoop(t, provider)
	msg := bus.InboundMessage{
		Channel:  "telegram",
		ChatID:   "chat-1",
		SenderID: "user-1",
		Content:  "開一局海龜湯",
		Peer:     bus.Peer{Kind: "direct", ID: "user-1"},
	}

	response, agent, err := al.processMessageCore(context.Background(), msg, false)
	if err != nil {
		t.Fatalf("processMessageCore(start) error = %v", err)
	}
	if !strings.Contains(response, "湯面") {
		t.Fatalf("start response should include surface, got %q", response)
	}
	if strings.Contains(response, "視障") || strings.Contains(response, "妻子") {
		t.Fatalf("start response leaked solution: %q", response)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("start should bypass provider, got %d calls", len(provider.calls))
	}

	history := agent.Sessions.GetHistory("agent:main:telegram:direct:user-1")
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2: %+v", len(history), history)
	}
	joined := history[0].Content + "\n" + history[1].Content
	if strings.Contains(joined, "視障") || strings.Contains(joined, "妻子") {
		t.Fatalf("visible history leaked hidden solution: %s", joined)
	}

	workspaceText := readAllFilesForTest(t, cfg.WorkspacePath())
	if strings.Contains(workspaceText, "視障") || strings.Contains(workspaceText, "妻子") {
		t.Fatalf("workspace-visible files leaked hidden solution:\n%s", workspaceText)
	}
}

func TestTurtleSoupActiveQuestionUsesJudgeAndDoesNotCallNormalLoop(t *testing.T) {
	provider := &turtleSoupProvider{response: `{"kind":"question","label":"yes"}`}
	al, _ := newTurtleSoupTestLoop(t, provider)
	msg := bus.InboundMessage{
		Channel:  "telegram",
		ChatID:   "chat-1",
		SenderID: "user-1",
		Content:  "開一局海龜湯",
		Peer:     bus.Peer{Kind: "direct", ID: "user-1"},
	}
	if _, _, err := al.processMessageCore(context.Background(), msg, false); err != nil {
		t.Fatalf("start error = %v", err)
	}

	msg.Content = "這跟八樓的人有關嗎？"
	response, agent, err := al.processMessageCore(context.Background(), msg, false)
	if err != nil {
		t.Fatalf("question error = %v", err)
	}
	if response != "是" {
		t.Fatalf("question response = %q, want 是", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("active question should call only the judge once, got %d calls", len(provider.calls))
	}
	if len(provider.calls[0]) == 0 || !strings.Contains(provider.calls[0][1].Content, "hidden_solution") {
		t.Fatalf("judge call should receive hidden solution payload: %+v", provider.calls[0])
	}

	history := agent.Sessions.GetHistory("agent:main:telegram:direct:user-1")
	if len(history) != 4 {
		t.Fatalf("history len = %d, want 4: %+v", len(history), history)
	}
	if got := history[len(history)-1].Content; got != "是" {
		t.Fatalf("last visible response = %q, want 是", got)
	}
}

func TestTurtleSoupPublicCodeRoutesQuestionAndRejectsMismatch(t *testing.T) {
	provider := &turtleSoupProvider{response: `{"kind":"question","label":"yes"}`}
	al, _ := newTurtleSoupTestLoop(t, provider)
	msg := bus.InboundMessage{
		Channel:  "telegram",
		ChatID:   "chat-1",
		SenderID: "user-1",
		Content:  "開一局海龜湯",
		Peer:     bus.Peer{Kind: "direct", ID: "user-1"},
	}
	startResponse, _, err := al.processMessageCore(context.Background(), msg, false)
	if err != nil {
		t.Fatalf("start error = %v", err)
	}
	code := turtleSoupCodeFromResponse(t, startResponse)

	msg.Content = code + " 這跟八樓的人有關嗎？"
	response, _, err := al.processMessageCore(context.Background(), msg, false)
	if err != nil {
		t.Fatalf("question with code error = %v", err)
	}
	if response != "是" {
		t.Fatalf("question response = %q, want 是", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("coded question should call only the judge once, got %d calls", len(provider.calls))
	}
	judgePayload := provider.calls[0][1].Content
	if strings.Contains(judgePayload, code) {
		t.Fatalf("judge payload should receive stripped question without public code: %s", judgePayload)
	}
	if !strings.Contains(judgePayload, "這跟八樓的人有關嗎？") {
		t.Fatalf("judge payload should include stripped player question: %s", judgePayload)
	}

	msg.Content = "/turtle TS-0000 status"
	response, _, err = al.processMessageCore(context.Background(), msg, false)
	if err != nil {
		t.Fatalf("wrong code error = %v", err)
	}
	if !strings.Contains(response, "找不到這局海龜湯") {
		t.Fatalf("wrong-code response = %q", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("wrong code should not call judge or normal loop, got %d provider calls", len(provider.calls))
	}
}

func TestTurtleSoupUnknownPublicCodeDoesNotEnterNormalLoop(t *testing.T) {
	provider := &turtleSoupProvider{response: `normal model response`}
	al, _ := newTurtleSoupTestLoop(t, provider)
	msg := bus.InboundMessage{
		Channel:  "telegram",
		ChatID:   "chat-1",
		SenderID: "user-1",
		Content:  "TS-0000 這跟八樓有關嗎？",
		Peer:     bus.Peer{Kind: "direct", ID: "user-1"},
	}

	response, _, err := al.processMessageCore(context.Background(), msg, false)
	if err != nil {
		t.Fatalf("unknown code error = %v", err)
	}
	if !strings.Contains(response, "找不到這局海龜湯") {
		t.Fatalf("unknown-code response = %q", response)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("unknown turtle-soup code should not enter normal model loop, got %d calls", len(provider.calls))
	}
}

func TestTurtleSoupDirectSessionsAreIsolated(t *testing.T) {
	provider := &turtleSoupProvider{response: `{"kind":"question","label":"yes"}`}
	al, _ := newTurtleSoupTestLoop(t, provider)

	start := bus.InboundMessage{
		Channel:  "telegram",
		ChatID:   "chat-1",
		SenderID: "user-1",
		Content:  "開一局海龜湯",
		Peer:     bus.Peer{Kind: "direct", ID: "user-1"},
	}
	if _, _, err := al.processMessageCore(context.Background(), start, false); err != nil {
		t.Fatalf("start error = %v", err)
	}

	other := start
	other.ChatID = "chat-2"
	other.SenderID = "user-2"
	other.Peer = bus.Peer{Kind: "direct", ID: "user-2"}
	other.Content = "這跟八樓的人有關嗎？"
	response, _, err := al.processMessageCore(context.Background(), other, false)
	if err != nil {
		t.Fatalf("other session message error = %v", err)
	}
	if response == "是" {
		t.Fatalf("other session should not be routed to active game")
	}
}

func turtleSoupCodeFromResponse(t *testing.T, response string) string {
	t.Helper()
	idx := strings.Index(response, "TS-")
	if idx < 0 || len(response[idx:]) < len("TS-7K3P") {
		t.Fatalf("response does not include turtle soup code: %q", response)
	}
	return response[idx : idx+len("TS-7K3P")]
}

func readAllFilesForTest(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		b.Write(data)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%s) error = %v", root, err)
	}
	return b.String()
}
