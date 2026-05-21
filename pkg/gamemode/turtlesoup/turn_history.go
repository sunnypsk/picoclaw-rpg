package turtlesoup

import "strings"

type promptTurn struct {
	PlayerMessage string `json:"player_message"`
	Kind          string `json:"kind"`
	Label         string `json:"label,omitempty"`
	Solved        *bool  `json:"solved,omitempty"`
}

func promptTurnHistory(turns []Turn) []promptTurn {
	if len(turns) == 0 {
		return nil
	}
	out := make([]promptTurn, 0, len(turns))
	for _, turn := range turns {
		message := strings.TrimSpace(turn.PlayerMessage)
		if message == "" {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(turn.Kind))
		if kind != "guess" {
			kind = "question"
		}
		entry := promptTurn{
			PlayerMessage: message,
			Kind:          kind,
		}
		if kind == "guess" {
			entry.Solved = boolPtr(turn.Solved)
		} else {
			entry.Label = string(normalizeLabel(turn.Label))
		}
		out = append(out, entry)
	}
	return out
}

func boolPtr(value bool) *bool {
	return &value
}
