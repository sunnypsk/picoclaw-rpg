package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/routing"
	"github.com/sipeed/picoclaw/pkg/tools"
)

func (al *AgentLoop) ProcessScheduledReminder(
	ctx context.Context,
	req tools.ScheduledReminderRequest,
) (string, error) {
	if al == nil || al.registry == nil {
		return "", fmt.Errorf("scheduled reminder executor unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	agent, routedSessionKey := al.resolveScheduledReminderTarget(req)
	if agent == nil {
		return "", fmt.Errorf("no agent available for scheduled reminder")
	}

	if req.Deliver {
		sendCtx := ctx
		mirrorToSession := false
		if routedSessionKey != "" {
			sendCtx = withMirroredSessionKey(sendCtx, routedSessionKey)
			mirrorToSession = true
		}
		if ok := al.publishAgentMessage(sendCtx, agent, req.Channel, req.ChatID, req.Content, mirrorToSession); !ok {
			return "", fmt.Errorf("failed to publish scheduled reminder")
		}
		return "ok", nil
	}

	if tool, ok := agent.Tools.Get("message"); ok {
		if resetter, ok := tool.(interface{ ResetSentInRound() }); ok {
			resetter.ResetSentInRound()
		}
	}

	reminderCtx := ctx
	capture := &proactiveOutputCapture{}
	if routedSessionKey != "" {
		reminderCtx = withMirroredSessionKey(reminderCtx, routedSessionKey)
		reminderCtx = withMirroredOutboundCapture(reminderCtx, capture)
	}

	response, err := al.runAgentLoop(reminderCtx, agent, processOptions{
		SessionKey:        scheduledReminderSessionKey(agent.ID, req.JobID),
		ContextSessionKey: routedSessionKey,
		Channel:           req.Channel,
		ChatID:            req.ChatID,
		UserMessage:       req.Content,
		AutoRecallQuery:   req.Content,
		DefaultResponse:   defaultResponse,
		EnableSummary:     false,
		SendResponse:      false,
		PersistSession:    false,
	})
	if err != nil {
		return "", err
	}

	if routedSessionKey != "" {
		visibleMessages := capture.Messages()
		if len(visibleMessages) > 0 {
			al.appendVisibleAssistantMessagesToSession(agent, routedSessionKey, req.Channel, req.ChatID, visibleMessages)
			return "ok", nil
		}
	}

	if agentMessageAlreadySent(agent) {
		return "ok", nil
	}

	trimmed := strings.TrimSpace(response)
	if trimmed == "" {
		return "", nil
	}

	sendCtx := ctx
	mirrorToSession := false
	if routedSessionKey != "" {
		sendCtx = withMirroredSessionKey(sendCtx, routedSessionKey)
		mirrorToSession = true
	}
	if ok := al.publishAgentMessage(sendCtx, agent, req.Channel, req.ChatID, trimmed, mirrorToSession); !ok {
		return "", fmt.Errorf("failed to publish scheduled reminder response")
	}

	return trimmed, nil
}

func (al *AgentLoop) resolveScheduledReminderTarget(
	req tools.ScheduledReminderRequest,
) (*AgentInstance, string) {
	sessionKey := strings.ToLower(strings.TrimSpace(req.SessionKey))
	if parsed := routing.ParseAgentSessionKey(sessionKey); parsed != nil {
		if agent, ok := al.registry.GetAgent(parsed.AgentID); ok && agent != nil {
			return agent, al.resolveRotatedSessionKey(parsed.AgentID, sessionKey)
		}
		if agent := al.registry.GetDefaultAgent(); agent != nil {
			return agent, al.resolveRotatedSessionKey(parsed.AgentID, sessionKey)
		}
	}

	return al.registry.GetDefaultAgent(), ""
}

func scheduledReminderSessionKey(agentID, jobID string) string {
	replacer := strings.NewReplacer(":", "-", "/", "-", "\\", "-")
	normalizedAgentID := routing.NormalizeAgentID(agentID)
	if normalizedAgentID == "" {
		normalizedAgentID = routing.NormalizeAgentID("main")
	}
	return fmt.Sprintf("agent:%s:scheduled-reminder:%s", normalizedAgentID, replacer.Replace(strings.TrimSpace(jobID)))
}
