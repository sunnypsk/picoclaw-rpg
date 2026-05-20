package turtlesoup

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
)

type generatorProvider struct {
	response string
	calls    [][]providers.Message
}

func (p *generatorProvider) Chat(
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

func (p *generatorProvider) GetDefaultModel() string {
	return "mock-model"
}

func TestLLMGeneratorSendsRecentGamesForRelativeDifficulty(t *testing.T) {
	provider := &generatorProvider{
		response: `{"surface":"new surface","solution":"new secret","hints":["one","two","three"]}`,
	}
	generator := LLMGenerator{Provider: provider, Model: "mock-model"}
	_, err := generator.Generate(context.Background(), GenerationRequest{
		Difficulty: "harder than last time",
		RecentGames: []GameSummary{{
			Surface:       "previous public surface",
			Difficulty:    "hard",
			Themes:        []string{"library"},
			QuestionCount: 8,
			HintsUsed:     1,
			Outcome:       OutcomeSolved,
			StartedAt:     time.Date(2026, 5, 20, 1, 0, 0, 0, time.UTC),
			EndedAt:       time.Date(2026, 5, 20, 1, 10, 0, 0, time.UTC),
		}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(provider.calls) != 1 || len(provider.calls[0]) != 2 {
		t.Fatalf("provider calls = %+v", provider.calls)
	}

	var payload struct {
		Difficulty  string        `json:"difficulty"`
		RecentGames []GameSummary `json:"recent_games"`
	}
	if err := json.Unmarshal([]byte(provider.calls[0][1].Content), &payload); err != nil {
		t.Fatalf("decode generator payload: %v", err)
	}
	if payload.Difficulty != "harder than last time" {
		t.Fatalf("difficulty = %q", payload.Difficulty)
	}
	if len(payload.RecentGames) != 1 || payload.RecentGames[0].Surface != "previous public surface" {
		t.Fatalf("recent_games = %+v", payload.RecentGames)
	}
}
