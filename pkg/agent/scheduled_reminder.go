package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/sipeed/picoclaw/pkg/bus"
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

	ctx = bus.WithTrace(ctx, "cron:"+req.JobID+":"+uuid.NewString())
	agent, routedSessionKey := al.resolveScheduledReminderTarget(req)
	if agent == nil {
		return "", fmt.Errorf("no agent available for scheduled reminder")
	}

	if req.Deliver && req.MinVerifiedSources == 0 {
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

	reminderCtx := tools.WithSendState(ctx)
	if source, ok := scheduledReminderRouteInput(req, req.SessionKey); ok {
		reminderCtx = tools.WithSourcePeer(reminderCtx, source.Peer)
	}
	capture := &proactiveOutputCapture{}
	if routedSessionKey != "" {
		reminderCtx = withMirroredSessionKey(reminderCtx, routedSessionKey)
		reminderCtx = withMirroredOutboundCapture(reminderCtx, capture)
	}

	var evidence *tools.WebEvidence
	if req.MinVerifiedSources > 0 {
		reminderCtx, _ = bus.WithOutputBuffer(reminderCtx)
		reminderCtx, evidence = tools.WithWebEvidence(reminderCtx)
		req.Content += newsInstruction + fmt.Sprintf("\nRequired independent sites: %d", req.MinVerifiedSources)
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
	if req.MinVerifiedSources > 0 {
		final := ""
		if err == nil {
			final, err = validateNews(response, req.MinVerifiedSources, evidence)
		}
		if err != nil {
			final = newsFailure
		}
		sendCtx := withMirroredSessionKey(ctx, routedSessionKey)
		if !al.publishAgentMessage(sendCtx, agent, req.Channel, req.ChatID, final, routedSessionKey != "") {
			return "", fmt.Errorf("failed to publish final news result")
		}
		return final, err
	}
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

	if tools.MessageSent(reminderCtx) {
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

	if route, ok := al.scheduledReminderRoute(req, req.SessionKey); ok {
		if agent := al.resolveAgentForRoute(route); agent != nil {
			session := route.SessionKey
			if parsed := routing.ParseAgentSessionKey(req.SessionKey); parsed != nil && parsed.AgentID == agent.ID {
				parts := strings.Split(parsed.Rest, ":")
				if len(parts) == 3 && parts[0] == req.Channel && parts[2] == strings.ToLower(req.ChatID) && parts[1] == scheduledPeerKind(req.ChatID) {
					session = req.SessionKey
				}
			}
			return agent, al.resolveRotatedSessionKey(agent.ID, session)
		}
	}
	return al.registry.GetDefaultAgent(), ""

}

func (al *AgentLoop) scheduledReminderRoute(
	req tools.ScheduledReminderRequest,
	sessionKey string,
) (routing.ResolvedRoute, bool) {
	input, ok := scheduledReminderRouteInput(req, sessionKey)
	if !ok || al == nil || al.registry == nil {
		return routing.ResolvedRoute{}, false
	}
	return al.registry.ResolveRoute(input), true
}

func scheduledReminderRouteInput(
	req tools.ScheduledReminderRequest,
	sessionKey string,
) (routing.RouteInput, bool) {
	input := routing.ScheduledSourceInput(req.Channel, req.ChatID, sessionKey, req.SourcePeer)
	return input, input.Channel != ""
}

func isScheduledReminderPeerKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "direct", "group", "channel":
		return true
	default:
		return false
	}
}

func scheduledReminderSessionKey(agentID, jobID string) string {
	replacer := strings.NewReplacer(":", "-", "/", "-", "\\", "-")
	normalizedAgentID := routing.NormalizeAgentID(agentID)
	if normalizedAgentID == "" {
		normalizedAgentID = routing.NormalizeAgentID("main")
	}
	return fmt.Sprintf("agent:%s:scheduled-reminder:%s", normalizedAgentID, replacer.Replace(strings.TrimSpace(jobID)))
}

func scheduledPeerKind(chat string) string {
	if strings.HasSuffix(chat, "@g.us") {
		return "group"
	}
	return "direct"
}
