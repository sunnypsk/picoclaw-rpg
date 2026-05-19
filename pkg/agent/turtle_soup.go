package agent

import (
	"context"
	"errors"
	"strings"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/gamemode/turtlesoup"
)

func (al *AgentLoop) handleTurtleSoupTurn(
	ctx context.Context,
	agent *AgentInstance,
	msg bus.InboundMessage,
	sessionKey string,
) (string, bool) {
	if al == nil || al.turtleSoup == nil || agent == nil {
		return "", false
	}

	content := strings.TrimSpace(msg.Content)
	if content == "" || isNewSessionCommand(content) {
		return "", false
	}

	if al.turtleSoup.IsStartRequest(content) {
		response, err := al.turtleSoup.Start(sessionKey)
		if err != nil {
			return "海龜湯暫時無法開始，請稍後再試。", true
		}
		return response, true
	}

	if !al.turtleSoup.HasActive(sessionKey) {
		if al.turtleSoup.ReferencesGameCode(content) {
			return "找不到這局海龜湯，請確認代號。", true
		}
		return "", false
	}

	judge := turtlesoup.LLMJudge{
		Provider: agent.Provider,
		Model:    agent.Model,
	}
	response, err := al.turtleSoup.Handle(ctx, sessionKey, content, judge)
	if errors.Is(err, turtlesoup.ErrNoActiveGame) {
		return "", false
	}
	if err != nil {
		return "海龜湯暫時出錯，請稍後再試。", true
	}
	return response, true
}

func isNewSessionCommand(content string) bool {
	parts := strings.Fields(strings.TrimSpace(content))
	return len(parts) > 0 && parts[0] == "/new"
}

func (al *AgentLoop) persistTurtleSoupVisibleTurn(
	agent *AgentInstance,
	sessionKey string,
	msg bus.InboundMessage,
	response string,
) {
	if agent == nil || agent.Sessions == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	userMessage := attributeInboundMessage(msg)
	if userMessage != "" {
		agent.Sessions.AddMessage(sessionKey, "user", userMessage)
	}
	if strings.TrimSpace(response) != "" {
		agent.Sessions.AddMessage(sessionKey, "assistant", strings.TrimSpace(response))
	}
	agent.Sessions.Save(sessionKey)
	al.maybeSummarize(agent, sessionKey, msg.Channel, msg.ChatID)
}
