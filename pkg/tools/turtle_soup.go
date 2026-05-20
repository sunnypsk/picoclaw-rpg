package tools

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/sipeed/picoclaw/pkg/gamemode/turtlesoup"
	"github.com/sipeed/picoclaw/pkg/providers"
)

var turtleSoupPublicCodePattern = regexp.MustCompile(`(?i)\bTS-?[A-Z0-9]{4}\b`)

type TurtleSoupTool struct {
	engine        *turtlesoup.Engine
	provider      providers.LLMProvider
	model         string
	modelResolver func() string
}

func NewTurtleSoupTool(engine *turtlesoup.Engine, provider providers.LLMProvider, model string) *TurtleSoupTool {
	return &TurtleSoupTool{
		engine:   engine,
		provider: provider,
		model:    strings.TrimSpace(model),
	}
}

func NewTurtleSoupToolWithModelResolver(
	engine *turtlesoup.Engine,
	provider providers.LLMProvider,
	modelResolver func() string,
) *TurtleSoupTool {
	return &TurtleSoupTool{
		engine:        engine,
		provider:      provider,
		modelResolver: modelResolver,
	}
}

func (t *TurtleSoupTool) Name() string {
	return "turtle_soup"
}

func (t *TurtleSoupTool) Description() string {
	return "Host a generated 海龜湯 / turtle soup yes-no mystery game in the current chat. " +
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
			"difficulty": map[string]any{
				"type":        "string",
				"description": "Optional free-text difficulty phrase for action=start, such as easy, very hard, harder than last time, or expert but fair.",
			},
			"themes": map[string]any{
				"type":        "array",
				"description": "Optional theme tags for action=start.",
				"items": map[string]any{
					"type": "string",
				},
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
		response, err = t.engine.StartWithOptions(ctx, sessionKey, turtlesoup.StartOptions{
			Difficulty: turtleSoupStringArg(args, "difficulty"),
			Themes:     turtleSoupStringSliceArg(args, "themes"),
			Message:    message,
			Generator:  t.generator(),
		})
	case "turn", "ask", "question", "guess":
		if message == "" {
			return ErrorResult("message is required for turtle_soup action=turn")
		}
		response, err = t.engine.Handle(ctx, sessionKey, message, t.judge())
	case "hint":
		response, err = t.engine.Handle(ctx, sessionKey, turtleSoupControlInput(message, "hint"), nil)
	case "status":
		response, err = t.engine.Handle(ctx, sessionKey, turtleSoupControlInput(message, "status"), nil)
	case "surrender", "giveup", "give_up", "reveal", "answer":
		response, err = t.engine.Handle(ctx, sessionKey, turtleSoupControlInput(message, "giveup"), nil)
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
		Model:    t.currentModel(),
	}
}

func (t *TurtleSoupTool) generator() turtlesoup.PuzzleGenerator {
	if t == nil || t.provider == nil {
		return nil
	}
	return turtlesoup.LLMGenerator{
		Provider: t.provider,
		Model:    t.currentModel(),
	}
}

func (t *TurtleSoupTool) currentModel() string {
	if t == nil {
		return ""
	}
	if t.modelResolver != nil {
		if model := strings.TrimSpace(t.modelResolver()); model != "" {
			return model
		}
	}
	return t.model
}

func turtleSoupControlInput(message, command string) string {
	if code := turtleSoupPublicCode(message); code != "" {
		return code + " " + command
	}
	return command
}

func turtleSoupPublicCode(message string) string {
	match := turtleSoupPublicCodePattern.FindString(message)
	if match == "" {
		return ""
	}
	compact := strings.ToUpper(strings.ReplaceAll(match, "-", ""))
	if len(compact) != 6 || !strings.HasPrefix(compact, "TS") {
		return ""
	}
	return "TS-" + compact[2:]
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

func turtleSoupStringSliceArg(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	value, ok := args[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}
