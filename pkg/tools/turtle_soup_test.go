package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/gamemode/turtlesoup"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type turtleSoupToolProvider struct {
	response string
	calls    [][]providers.Message
}

func (p *turtleSoupToolProvider) Chat(
	_ context.Context,
	messages []providers.Message,
	_ []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	cloned := make([]providers.Message, len(messages))
	copy(cloned, messages)
	p.calls = append(p.calls, cloned)
	return &providers.LLMResponse{Content: p.response}, nil
}

func (p *turtleSoupToolProvider) GetDefaultModel() string {
	return "mock-model"
}

func TestTurtleSoupToolRequiresSessionContext(t *testing.T) {
	engine := turtlesoup.NewEngine(turtlesoup.NewStore(t.TempDir()), []turtlesoup.Puzzle{{
		ID:       "test",
		Surface:  "surface text",
		Solution: "hidden answer",
	}})
	tool := NewTurtleSoupTool(engine, nil, "")

	result := tool.Execute(context.Background(), map[string]any{"action": "start"})
	if result == nil || !result.IsError {
		t.Fatalf("expected missing session context error, got %+v", result)
	}
}

func TestTurtleSoupToolStartDoesNotLeakSolution(t *testing.T) {
	root := t.TempDir()
	engine := turtlesoup.NewEngine(
		turtlesoup.NewStore(root),
		[]turtlesoup.Puzzle{{
			ID:       "test",
			Surface:  "surface text",
			Solution: "hidden answer",
		}},
	)
	tool := NewTurtleSoupTool(engine, nil, "")
	ctx := WithToolExecutionContext(context.Background(), "telegram", "chat-1", "", "", "session-1", nil)

	result := tool.Execute(ctx, map[string]any{"action": "start"})
	if result == nil || result.IsError {
		t.Fatalf("start result error = %+v", result)
	}
	if !result.Silent {
		t.Fatalf("turtle soup tool should return a silent result for the agent to relay")
	}
	if !strings.Contains(result.ForLLM, "surface text") || !strings.Contains(result.ForLLM, "TS-") {
		t.Fatalf("start result should include public surface and code, got %q", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, "hidden answer") {
		t.Fatalf("start result leaked hidden solution: %q", result.ForLLM)
	}
}

func TestTurtleSoupToolTurnUsesJudge(t *testing.T) {
	root := t.TempDir()
	provider := &turtleSoupToolProvider{response: `{"kind":"question","label":"yes"}`}
	engine := turtlesoup.NewEngine(
		turtlesoup.NewStore(root),
		[]turtlesoup.Puzzle{{
			ID:       "test",
			Surface:  "surface text",
			Solution: "hidden answer",
		}},
	)
	tool := NewTurtleSoupTool(engine, provider, "mock-model")
	ctx := WithToolExecutionContext(context.Background(), "telegram", "chat-1", "", "", "session-1", nil)
	if result := tool.Execute(ctx, map[string]any{"action": "start"}); result.IsError {
		t.Fatalf("start result error = %+v", result)
	}

	result := tool.Execute(ctx, map[string]any{
		"action":  "turn",
		"message": "is it about food?",
	})
	if result == nil || result.IsError {
		t.Fatalf("turn result error = %+v", result)
	}
	if result.ForLLM != "是" {
		t.Fatalf("turn response = %q, want 是", result.ForLLM)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("judge calls = %d, want 1", len(provider.calls))
	}
	payload := provider.calls[0][1].Content
	if !strings.Contains(payload, "hidden_solution") || !strings.Contains(payload, "hidden answer") {
		t.Fatalf("judge payload should include hidden solution: %s", payload)
	}
	if !strings.Contains(payload, "is it about food?") {
		t.Fatalf("judge payload should include player message: %s", payload)
	}
}

func TestTurtleSoupToolControlActionsDoNotCallJudge(t *testing.T) {
	root := t.TempDir()
	provider := &turtleSoupToolProvider{response: `{"kind":"question","label":"yes"}`}
	engine := turtlesoup.NewEngine(
		turtlesoup.NewStore(root),
		[]turtlesoup.Puzzle{{
			ID:       "test",
			Surface:  "surface text",
			Solution: "hidden answer",
			Hints:    []string{"first hint"},
		}},
	)
	tool := NewTurtleSoupTool(engine, provider, "mock-model")
	ctx := WithToolExecutionContext(context.Background(), "telegram", "chat-1", "", "", "session-1", nil)
	start := tool.Execute(ctx, map[string]any{"action": "start"})
	if start == nil || start.IsError {
		t.Fatalf("start result error = %+v", start)
	}
	code := turtleSoupPublicCode(start.ForLLM)
	if code == "" {
		t.Fatalf("start result should include public code, got %q", start.ForLLM)
	}

	hint := tool.Execute(ctx, map[string]any{
		"action":  "hint",
		"message": code + " can I get a hint?",
	})
	if hint == nil || hint.IsError || !strings.Contains(hint.ForLLM, "first hint") {
		t.Fatalf("hint result = %+v", hint)
	}
	status := tool.Execute(ctx, map[string]any{
		"action":  "status",
		"message": "please check " + code,
	})
	if status == nil || status.IsError || !strings.Contains(status.ForLLM, "surface text") {
		t.Fatalf("status result = %+v", status)
	}
	reveal := tool.Execute(ctx, map[string]any{
		"action":  "surrender",
		"message": "/turtle " + code + " please reveal the answer",
	})
	if reveal == nil || reveal.IsError || !strings.Contains(reveal.ForLLM, "hidden answer") {
		t.Fatalf("surrender result = %+v", reveal)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("control actions should not call judge, got %d calls", len(provider.calls))
	}
}
