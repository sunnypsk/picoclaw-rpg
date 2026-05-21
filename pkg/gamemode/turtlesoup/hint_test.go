package turtlesoup

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLLMHintProviderUsesTurnHistory(t *testing.T) {
	provider := &judgeProvider{response: `{"hint":"Focus on why the spoken destination was unsafe."}`}
	hints := LLMHintProvider{Provider: provider, Model: "mock-model"}
	hint, err := hints.GenerateHint(context.Background(), GameState{
		Surface:    "public surface",
		Solution:   "hidden answer",
		Hints:      []string{"static hint one", "static hint two"},
		ShownHints: []string{"generated hint one"},
		HintsUsed:  1,
		Turns: []Turn{{
			PlayerMessage: "is the passenger the victim?",
			Kind:          "question",
			Label:         LabelNo,
		}},
	})
	if err != nil {
		t.Fatalf("GenerateHint() error = %v", err)
	}
	if !strings.Contains(hint, "spoken destination") {
		t.Fatalf("hint = %q", hint)
	}
	if len(provider.calls) != 1 || len(provider.calls[0]) != 2 {
		t.Fatalf("provider calls = %+v", provider.calls)
	}

	var payload struct {
		HiddenSolution string       `json:"hidden_solution"`
		UsedHints      []string     `json:"used_hints"`
		RemainingHints []string     `json:"remaining_hints"`
		TurnHistory    []promptTurn `json:"turn_history"`
	}
	if err := json.Unmarshal([]byte(provider.calls[0][1].Content), &payload); err != nil {
		t.Fatalf("decode hint payload: %v", err)
	}
	if payload.HiddenSolution != "hidden answer" {
		t.Fatalf("hidden solution = %q", payload.HiddenSolution)
	}
	if strings.Join(payload.UsedHints, ",") != "generated hint one" {
		t.Fatalf("used hints = %+v", payload.UsedHints)
	}
	if strings.Join(payload.RemainingHints, ",") != "static hint two" {
		t.Fatalf("remaining hints = %+v", payload.RemainingHints)
	}
	if len(payload.TurnHistory) != 1 || payload.TurnHistory[0].Label != string(LabelNo) {
		t.Fatalf("turn history = %+v", payload.TurnHistory)
	}
}

func TestParseGeneratedHintRejectsEmptyHint(t *testing.T) {
	if _, err := parseGeneratedHint(`{"hint":""}`); err == nil {
		t.Fatal("expected empty hint error")
	}
}
