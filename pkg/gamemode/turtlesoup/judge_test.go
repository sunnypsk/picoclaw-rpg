package turtlesoup

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
)

type judgeProvider struct {
	response string
	calls    [][]providers.Message
}

func (p *judgeProvider) Chat(
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

func (p *judgeProvider) GetDefaultModel() string {
	return "mock-model"
}

func TestParseEvaluationNormalizesLabels(t *testing.T) {
	eval, err := parseEvaluation(`before {"kind":"question","label":"partial"} after`)
	if err != nil {
		t.Fatalf("parseEvaluation() error = %v", err)
	}
	if eval.Kind != "question" || eval.Label != LabelPartial {
		t.Fatalf("unexpected eval: %+v", eval)
	}
}

func TestParseEvaluationDefaultsUnsafeLabelToCannotAnswer(t *testing.T) {
	eval, err := parseEvaluation(`{"kind":"question","label":"please_reveal"}`)
	if err != nil {
		t.Fatalf("parseEvaluation() error = %v", err)
	}
	if eval.Label != LabelCannotAnswer {
		t.Fatalf("label = %q, want %q", eval.Label, LabelCannotAnswer)
	}
}

func TestLLMJudgeSendsTurnHistoryForConsistency(t *testing.T) {
	provider := &judgeProvider{response: `{"kind":"question","label":"no"}`}
	judge := LLMJudge{Provider: provider, Model: "mock-model"}
	_, err := judge.Evaluate(context.Background(), GameState{
		Surface:  "public surface",
		Solution: "hidden answer",
		Turns: []Turn{{
			PlayerMessage: "is the driver involved?",
			Kind:          "question",
			Label:         LabelYes,
		}},
	}, "is the driver unrelated?")
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(provider.calls) != 1 || len(provider.calls[0]) != 2 {
		t.Fatalf("provider calls = %+v", provider.calls)
	}

	var payload struct {
		HiddenSolution string       `json:"hidden_solution"`
		TurnHistory    []promptTurn `json:"turn_history"`
		PlayerMessage  string       `json:"player_message"`
	}
	if err := json.Unmarshal([]byte(provider.calls[0][1].Content), &payload); err != nil {
		t.Fatalf("decode judge payload: %v", err)
	}
	if payload.HiddenSolution != "hidden answer" {
		t.Fatalf("hidden solution = %q", payload.HiddenSolution)
	}
	if len(payload.TurnHistory) != 1 || payload.TurnHistory[0].Label != string(LabelYes) {
		t.Fatalf("turn history = %+v", payload.TurnHistory)
	}
	if payload.PlayerMessage != "is the driver unrelated?" {
		t.Fatalf("player message = %q", payload.PlayerMessage)
	}
}
