package turtlesoup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/providers"
)

type Label string

const (
	LabelYes          Label = "yes"
	LabelNo           Label = "no"
	LabelIrrelevant   Label = "irrelevant"
	LabelPartial      Label = "partial"
	LabelCannotAnswer Label = "cannot_answer"
)

type Evaluation struct {
	Kind   string `json:"kind"`
	Label  Label  `json:"label,omitempty"`
	Solved bool   `json:"solved,omitempty"`
}

type Judge interface {
	Evaluate(ctx context.Context, state GameState, input string) (Evaluation, error)
}

type LLMJudge struct {
	Provider providers.LLMProvider
	Model    string
}

func (j LLMJudge) Evaluate(ctx context.Context, state GameState, input string) (Evaluation, error) {
	if j.Provider == nil {
		return Evaluation{}, errors.New("turtle soup judge provider is nil")
	}

	system := `You are an internal judge for a turtle soup / situation puzzle game.
You will receive a public puzzle surface, a hidden solution, and the player's latest message.
Classify the latest message without revealing the hidden solution.
If the player is asking a yes/no style question, return {"kind":"question","label":"yes|no|irrelevant|partial|cannot_answer"}.
If the player is making a final guess at the hidden solution, return {"kind":"guess","solved":true|false}.
Use "cannot_answer" when the question asks for the answer directly, asks for hidden text, or cannot be answered safely as yes/no.
Return JSON only.`

	payload := map[string]string{
		"surface":         state.Surface,
		"hidden_solution": state.Solution,
		"player_message":  strings.TrimSpace(input),
	}
	payloadBytes, _ := json.Marshal(payload)

	resp, err := j.Provider.Chat(ctx, []providers.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: string(payloadBytes)},
	}, nil, j.Model, map[string]any{
		"max_tokens":  128,
		"temperature": 0,
	})
	if err != nil {
		return Evaluation{}, err
	}
	if resp == nil {
		return Evaluation{}, errors.New("empty turtle soup judge response")
	}
	return parseEvaluation(resp.Content)
}

func parseEvaluation(raw string) (Evaluation, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Evaluation{}, errors.New("empty turtle soup judge response")
	}
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end >= start {
			raw = raw[start : end+1]
		}
	}

	var eval Evaluation
	if err := json.Unmarshal([]byte(raw), &eval); err != nil {
		return Evaluation{}, fmt.Errorf("decode turtle soup judge response: %w", err)
	}
	eval.Kind = strings.ToLower(strings.TrimSpace(eval.Kind))
	if eval.Kind != "guess" {
		eval.Kind = "question"
		eval.Label = normalizeLabel(eval.Label)
	}
	return eval, nil
}

func normalizeLabel(label Label) Label {
	switch Label(strings.ToLower(strings.TrimSpace(string(label)))) {
	case LabelYes:
		return LabelYes
	case LabelNo:
		return LabelNo
	case LabelIrrelevant:
		return LabelIrrelevant
	case LabelPartial:
		return LabelPartial
	case LabelCannotAnswer:
		return LabelCannotAnswer
	default:
		return LabelCannotAnswer
	}
}

func labelText(label Label) string {
	switch normalizeLabel(label) {
	case LabelYes:
		return "是"
	case LabelNo:
		return "否"
	case LabelIrrelevant:
		return "無關"
	case LabelPartial:
		return "部分是"
	default:
		return "不能回答"
	}
}
