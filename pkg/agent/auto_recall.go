package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
)

func (al *AgentLoop) buildAutoRecallHints(
	ctx context.Context,
	agent *AgentInstance,
	userMessage string,
) string {
	if al == nil || al.cfg == nil || agent == nil || agent.MemoryIndex == nil {
		return ""
	}

	autoCfg := al.cfg.Agents.Defaults.MemorySearch.AutoRecall
	if !autoCfg.Enabled {
		return ""
	}

	message := strings.TrimSpace(userMessage)
	if message == "" || strings.HasPrefix(message, "/") {
		return ""
	}

	results, err := agent.MemoryIndex.Search(ctx, message, autoCfg.EffectiveTopK(), "")
	if err != nil {
		logger.DebugCF("agent", "Auto memory recall search failed",
			map[string]any{"agent_id": agent.ID, "error": err.Error()})
		return ""
	}
	if len(results) == 0 {
		return ""
	}

	maxChars := autoCfg.EffectiveMaxChars()
	var sb strings.Builder
	sb.WriteString("RELEVANT_MEMORY (keyword recall)\n")

	for i, r := range results {
		snippet := strings.TrimSpace(r.Snippet)
		if snippet == "" {
			continue
		}
		entry := fmt.Sprintf("%d. %s\n%s\n", i+1, r.Path, snippet)
		if sb.Len()+len(entry) > maxChars {
			break
		}
		sb.WriteString(entry)
	}

	if sb.Len() == 0 {
		return ""
	}

	return strings.TrimSpace(sb.String())
}

func injectAutoRecallHints(messages []providers.Message, hints string) []providers.Message {
	hints = strings.TrimSpace(hints)
	if hints == "" || len(messages) == 0 || messages[0].Role != "system" {
		return messages
	}

	hintBlock := hints
	messages[0].Content = messages[0].Content + "\n\n---\n\n" + hintBlock
	messages[0].SystemParts = append(messages[0].SystemParts, providers.ContentBlock{
		Type: "text",
		Text: hintBlock,
	})

	return messages
}
