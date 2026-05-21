package turtlesoup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/providers"
)

type LLMHintProvider struct {
	Provider providers.LLMProvider
	Model    string
}

func (h LLMHintProvider) GenerateHint(ctx context.Context, state GameState) (string, error) {
	if h.Provider == nil {
		return "", errors.New("turtle soup hint provider is nil")
	}

	system := `You write context-aware hints for a turtle soup / situation puzzle game.
Return JSON only with exactly this shape: {"hint":"short public hint"}
You will receive the public surface, hidden solution, static hints, used hints, and previous judged turns.
The hint must help the player make new progress without revealing the full solution.
Do not repeat facts the player already established in previous judged turns.
Do not contradict previous judged turns.
Prefer a hint that points to the next missing inference.
Match the player's language when it is clear; otherwise use Traditional Chinese.`

	usedHints := state.Hints[:minInt(state.HintsUsed, len(state.Hints))]
	remainingHints := state.Hints[minInt(state.HintsUsed, len(state.Hints)):]
	payload := map[string]any{
		"surface":         state.Surface,
		"hidden_solution": state.Solution,
		"used_hints":      usedHints,
		"remaining_hints": remainingHints,
		"turn_history":    promptTurnHistory(state.Turns),
	}
	payloadBytes, _ := json.Marshal(payload)

	resp, err := h.Provider.Chat(ctx, []providers.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: string(payloadBytes)},
	}, nil, h.Model, map[string]any{
		"max_tokens":  160,
		"temperature": 0.4,
	})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.New("empty turtle soup hint response")
	}
	hint, err := parseGeneratedHint(resp.Content)
	if err != nil {
		return "", err
	}
	if visibleContainsSolution(hint, state.Solution) {
		return "", errors.New("generated turtle soup hint leaks the solution")
	}
	return hint, nil
}

func parseGeneratedHint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty turtle soup hint response")
	}
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end >= start {
			raw = raw[start : end+1]
		}
	}

	var response struct {
		Hint string `json:"hint"`
	}
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return "", fmt.Errorf("decode turtle soup hint response: %w", err)
	}
	hint := strings.TrimSpace(response.Hint)
	if hint == "" {
		return "", errors.New("empty turtle soup hint")
	}
	return hint, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
