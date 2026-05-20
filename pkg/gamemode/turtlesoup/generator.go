package turtlesoup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/providers"
)

type LLMGenerator struct {
	Provider providers.LLMProvider
	Model    string
}

func (g LLMGenerator) Generate(ctx context.Context, request GenerationRequest) (Puzzle, error) {
	if g.Provider == nil {
		return Puzzle{}, errors.New("turtle soup generator provider is nil")
	}

	system := `You create original turtle soup / situation puzzle stories.
Return JSON only with exactly these fields:
{"surface":"public mystery setup","solution":"hidden complete explanation","hints":["hint 1","hint 2","hint 3"],"difficulty":"requested difficulty phrase","themes":["tag"]}
The surface must be public and intriguing but must not reveal the solution.
The solution must be specific enough for an internal judge to answer yes/no questions.
Hints must be progressive and must not directly reveal the hidden solution.
Follow the requested difficulty phrase and theme tags.
If the difficulty is relative, such as "harder than last time" or "easier", compare it against recent_games[0] when present.
For harder puzzles, increase deduction depth, misdirection, required inference steps, and clue subtlety while keeping the puzzle fair.
For easier puzzles, reduce inference steps and make clues more direct.
Match the user's language when it is clear; otherwise use Traditional Chinese.`

	payload := map[string]any{
		"difficulty":   request.Difficulty,
		"themes":       request.Themes,
		"user_request": strings.TrimSpace(request.Message),
		"recent_games": request.RecentGames,
	}
	payloadBytes, _ := json.Marshal(payload)

	resp, err := g.Provider.Chat(ctx, []providers.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: string(payloadBytes)},
	}, nil, g.Model, map[string]any{
		"max_tokens":  900,
		"temperature": 0.9,
	})
	if err != nil {
		return Puzzle{}, err
	}
	if resp == nil {
		return Puzzle{}, errors.New("empty turtle soup generator response")
	}
	return parseGeneratedPuzzle(resp.Content)
}

func parseGeneratedPuzzle(raw string) (Puzzle, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Puzzle{}, errors.New("empty turtle soup generator response")
	}
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end >= start {
			raw = raw[start : end+1]
		}
	}

	var puzzle Puzzle
	if err := json.Unmarshal([]byte(raw), &puzzle); err != nil {
		return Puzzle{}, fmt.Errorf("decode turtle soup generator response: %w", err)
	}
	return puzzle, nil
}
