package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/gamemode/turtlesoup"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type TurtleSoupTool struct {
	engine   *turtlesoup.Engine
	provider providers.LLMProvider
	model    string
}

func NewTurtleSoupTool(engine *turtlesoup.Engine, provider providers.LLMProvider, model string) *TurtleSoupTool {
	return &TurtleSoupTool{
		engine:   engine,
		provider: provider,
		model:    strings.TrimSpace(model),
	}
}

func (t *TurtleSoupTool) Name() string {
	return "turtle_soup"
}

func (t *TurtleSoupTool) Description() string {
	return "Host the built-in 海龜湯 / turtle soup yes-no mystery game in the current chat. " +
		"Use this instead of inventing your own turtle soup puzzle when the user wants to play, asks for a hint/status, " +
		"asks a game question, makes a solution guess, or gives up. The tool returns visible game text only; " +
		"relay that visible response naturally and do not expose hidden solution details unless the tool reveals them."
}

func (t *TurtleSoupTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Game action to perform.",
				"enum":        []string{"start", "turn", "hint", "status", "surrender"},
			},
			"message": map[string]any{
				"type": "string",
				"description": "The user's exact game question or guess. Required for action=turn. " +
					"For hint/status/surrender, include the user's original command when it has a public game code.",
			},
		},
		"required": []string{"action"},
	}
}

func (t *TurtleSoupTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	if t == nil || t.engine == nil {
		return ErrorResult("turtle soup game is not configured")
	}
	sessionKey := strings.TrimSpace(ToolSessionKey(ctx))
	if sessionKey == "" {
		return ErrorResult("no session context. Use this tool in an active conversation.")
	}

	action := strings.ToLower(strings.TrimSpace(turtleSoupStringArg(args, "action")))
	message := strings.TrimSpace(turtleSoupStringArg(args, "message"))

	var (
		response string
		err      error
	)
	switch action {
	case "start", "new":
		response, err = t.engine.Start(sessionKey)
	case "turn", "ask", "question", "guess":
		if message == "" {
			return ErrorResult("message is required for turtle_soup action=turn")
		}
		response, err = t.engine.Handle(ctx, sessionKey, message, t.judge())
	case "hint":
		response, err = t.engine.Handle(ctx, sessionKey, defaultString(message, "hint"), nil)
	case "status":
		response, err = t.engine.Handle(ctx, sessionKey, defaultString(message, "status"), nil)
	case "surrender", "giveup", "give_up", "reveal", "answer":
		response, err = t.engine.Handle(ctx, sessionKey, defaultString(message, "giveup"), nil)
	default:
		return ErrorResult(fmt.Sprintf("unknown turtle_soup action: %s", action))
	}
	if errors.Is(err, turtlesoup.ErrNoActiveGame) {
		return ErrorResult("no active turtle soup game. Use action=start first.")
	}
	if err != nil {
		return ErrorResult(fmt.Sprintf("turtle soup error: %v", err)).WithError(err)
	}
	return SilentResult(response)
}

func (t *TurtleSoupTool) judge() turtlesoup.Judge {
	if t == nil || t.provider == nil {
		return nil
	}
	return turtlesoup.LLMJudge{
		Provider: t.provider,
		Model:    t.model,
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func turtleSoupStringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, ok := args[key]
	if !ok {
		return ""
	}
	result, _ := value.(string)
	return result
}
