package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/constants"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/routing"
)

const proactiveNoopToken = "PROACTIVE_NOOP"

type proactiveEvaluation struct {
	Ready       bool
	Triggered   bool
	Probability float64
	Tolerance   time.Duration
	Silence     time.Duration
}

func (al *AgentLoop) RunProactiveHeartbeat(ctx context.Context) {
	if al == nil || al.cfg == nil || al.registry == nil {
		return
	}
	proactiveCfg := normalizeHeartbeatProactiveConfig(al.cfg.Heartbeat.Proactive)
	if !proactiveCfg.Enabled {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	interval := heartbeatIntervalDuration(al.cfg.Heartbeat.Interval)
	now := time.Now().UTC()
	agentIDs := al.registry.ListAgentIDs()
	sort.Strings(agentIDs)

	for _, agentID := range agentIDs {
		agent, ok := al.registry.GetAgent(agentID)
		if !ok || agent == nil || agent.StateStore == nil {
			continue
		}
		state, err := agent.StateStore.LoadState()
		if err != nil {
			logger.WarnCF("agent", "Failed to load state for proactive heartbeat", map[string]any{
				"agent_id": agentID,
				"error":    err.Error(),
			})
			continue
		}
		relationshipKeys := make([]string, 0, len(state.Relationships))
		for key := range state.Relationships {
			relationshipKeys = append(relationshipKeys, key)
		}
		sort.Strings(relationshipKeys)
		for _, relationshipKey := range relationshipKeys {
			rel := state.Relationships[relationshipKey]
			eval := evaluateProactiveOpportunity(rel, proactiveCfg, interval, now, rand.Float64())
			if !eval.Triggered {
				continue
			}
			if err := recordProactiveAttempt(agent, relationshipKey, now); err != nil {
				logger.WarnCF("agent", "Failed to record proactive attempt", map[string]any{
					"agent_id":         agent.ID,
					"relationship_key": relationshipKey,
					"error":            err.Error(),
				})
			}
			sent, err := al.runProactiveOutreach(ctx, agent, relationshipKey, rel, eval)
			if err != nil {
				logger.WarnCF("agent", "Proactive outreach failed", map[string]any{
					"agent_id":         agent.ID,
					"relationship_key": relationshipKey,
					"error":            err.Error(),
				})
				continue
			}
			if !sent {
				continue
			}
			if err := recordProactiveSuccess(agent, relationshipKey, time.Now().UTC()); err != nil {
				logger.WarnCF("agent", "Failed to record proactive success", map[string]any{
					"agent_id":         agent.ID,
					"relationship_key": relationshipKey,
					"error":            err.Error(),
				})
			}
		}
	}
}

func (al *AgentLoop) runProactiveOutreach(
	ctx context.Context,
	agent *AgentInstance,
	relationshipKey string,
	rel NPCRelationship,
	eval proactiveEvaluation,
) (bool, error) {
	if agent == nil {
		return false, nil
	}
	routedSessionKey := proactiveContextSessionKey(al.cfg, agent.ID, relationshipKey, rel)
	if routedSessionKey == "" {
		logger.DebugCF("agent", "Skipped proactive outreach without routed session key", map[string]any{
			"agent_id":         agent.ID,
			"relationship_key": relationshipKey,
		})
		return false, nil
	}
	if tool, ok := agent.Tools.Get("message"); ok {
		if resetter, ok := tool.(interface{ ResetSentInRound() }); ok {
			resetter.ResetSentInRound()
		}
	}
	capture := &proactiveOutputCapture{}
	proactiveCtx := withProactiveOutputCapture(withProactiveSessionKey(ctx, routedSessionKey), capture)
	response, err := al.runAgentLoop(proactiveCtx, agent, processOptions{
		SessionKey:        proactiveSessionKey(agent.ID, relationshipKey),
		ContextSessionKey: routedSessionKey,
		Channel:           rel.LastChannel,
		ChatID:            rel.LastChatID,
		UserMessage:       buildProactivePrompt(relationshipKey, rel, eval),
		AutoRecallQuery:   proactiveAutoRecallQuery(agent, routedSessionKey),
		DefaultResponse:   proactiveNoopToken,
		EnableSummary:     false,
		SendResponse:      false,
		NoHistory:         false,
		PersistSession:    false,
	})
	if err != nil {
		return false, err
	}
	visibleMessages := capture.Messages()
	if len(visibleMessages) > 0 {
		al.appendVisibleAssistantMessagesToSession(agent, routedSessionKey, rel.LastChannel, rel.LastChatID, visibleMessages)
		return true, nil
	}
	if strings.TrimSpace(response) != "" && strings.TrimSpace(response) != proactiveNoopToken {
		al.publishAgentMessage(proactiveCtx, agent, rel.LastChannel, rel.LastChatID, response, true)
		return true, nil
	}
	return false, nil
}

func proactiveSessionKey(agentID, relationshipKey string) string {
	replacer := strings.NewReplacer(":", "-", "/", "-", "\\", "-")
	return fmt.Sprintf("heartbeat-proactive-%s-%s", replacer.Replace(agentID), replacer.Replace(relationshipKey))
}

func proactiveContextSessionKey(
	cfg *config.Config,
	agentID, relationshipKey string,
	rel NPCRelationship,
) string {
	if sessionKey := strings.TrimSpace(rel.LastSessionKey); sessionKey != "" {
		return strings.ToLower(sessionKey)
	}
	if cfg == nil {
		return ""
	}
	channel := strings.TrimSpace(rel.LastChannel)
	chatID := strings.TrimSpace(rel.LastChatID)
	if channel == "" || chatID == "" {
		return ""
	}
	peerKind := normalizeRelationshipPeerKind(rel.LastPeerKind)
	peerID := proactivePeerID(relationshipKey, rel)
	if peerKind == "" || peerID == "" {
		return ""
	}
	return strings.ToLower(routing.BuildAgentPeerSessionKey(routing.SessionKeyParams{
		AgentID:       agentID,
		Channel:       channel,
		Peer:          &routing.RoutePeer{Kind: peerKind, ID: peerID},
		DMScope:       routing.DMScope(cfg.Session.DMScope),
		IdentityLinks: cfg.Session.IdentityLinks,
	}))
}

func proactivePeerID(relationshipKey string, rel NPCRelationship) string {
	peerKind := normalizeRelationshipPeerKind(rel.LastPeerKind)
	peerID := relationshipPeerID(relationshipKey)
	switch peerKind {
	case "group", "channel":
		return strings.TrimSpace(rel.LastChatID)
	case "direct":
		return peerID
	default:
		if strings.TrimSpace(rel.LastChatID) != "" && !strings.EqualFold(strings.TrimSpace(rel.LastChatID), peerID) {
			return strings.TrimSpace(rel.LastChatID)
		}
		return peerID
	}
}

func relationshipPeerID(relationshipKey string) string {
	parts := strings.SplitN(strings.ToLower(strings.TrimSpace(relationshipKey)), ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func proactiveAutoRecallQuery(agent *AgentInstance, sessionKey string) string {
	if agent == nil || agent.Sessions == nil || strings.TrimSpace(sessionKey) == "" {
		return ""
	}
	history := agent.Sessions.GetHistory(sessionKey)
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != "user" {
			continue
		}
		content := strings.TrimSpace(history[i].Content)
		if content == "" {
			continue
		}
		return content
	}
	return ""
}

func buildProactivePrompt(relationshipKey string, rel NPCRelationship, eval proactiveEvaluation) string {
	relationshipJSON, _ := json.MarshalIndent(rel, "", "  ")
	return fmt.Sprintf(`# Proactive Outreach Check

Relationship key: %s
Target channel: %s
Target chat ID: %s
Target chat kind: %s
Silence since last conversation activity: %s
Effective silence tolerance: %s
Current outreach probability on this heartbeat: %.2f

Relationship snapshot:
%s

Decide whether you should proactively say something now.

Rules:
- It is completely acceptable to stay silent.
- Prefer silence if the user may be working, sleeping, focused, socially tired, or simply seems to want space.
- If you decide to send something, use the message tool with a short, natural message. You can omit channel/chat_id and use the current target.
- If you decide not to send anything, respond ONLY with: %s
- Do not mention probabilities, timers, heartbeat checks, or internal state.
`, relationshipKey, rel.LastChannel, rel.LastChatID, rel.LastPeerKind, eval.Silence.Round(time.Minute), eval.Tolerance.Round(time.Minute), eval.Probability, string(relationshipJSON), proactiveNoopToken)
}

func normalizeHeartbeatProactiveConfig(cfg config.HeartbeatProactiveConfig) config.HeartbeatProactiveConfig {
	if cfg.BaseToleranceMinutes <= 0 {
		cfg.BaseToleranceMinutes = 240
	}
	if cfg.MinToleranceMinutes <= 0 {
		cfg.MinToleranceMinutes = 60
	}
	if cfg.MinToleranceMinutes > cfg.BaseToleranceMinutes {
		cfg.MinToleranceMinutes = cfg.BaseToleranceMinutes
	}
	if cfg.RelationshipStepMinutes < 0 {
		cfg.RelationshipStepMinutes = 0
	}
	if cfg.InitialProbability < 0 {
		cfg.InitialProbability = 0
	}
	if cfg.ProbabilityRampPerHeartbeat < 0 {
		cfg.ProbabilityRampPerHeartbeat = 0
	}
	if cfg.MaxProbability <= 0 {
		cfg.MaxProbability = cfg.InitialProbability
	}
	if cfg.MaxProbability > 1 {
		cfg.MaxProbability = 1
	}
	if cfg.InitialProbability > cfg.MaxProbability {
		cfg.InitialProbability = cfg.MaxProbability
	}
	if cfg.CooldownMinutes < 0 {
		cfg.CooldownMinutes = 0
	}
	return cfg
}

func evaluateProactiveOpportunity(
	rel NPCRelationship,
	cfg config.HeartbeatProactiveConfig,
	interval time.Duration,
	now time.Time,
	roll float64,
) proactiveEvaluation {
	eval := proactiveEvaluation{}
	if constants.IsInternalChannel(rel.LastChannel) || rel.LastChannel == "" || strings.TrimSpace(rel.LastChatID) == "" {
		return eval
	}
	lastConversationAt, ok := lastConversationAt(rel)
	if !ok {
		return eval
	}
	eval.Silence = now.Sub(lastConversationAt)
	eval.Tolerance = effectiveProactiveTolerance(cfg, rel)
	if eval.Silence < eval.Tolerance {
		return eval
	}
	if cooldownActive(rel, cfg, now) {
		return eval
	}
	eval.Ready = true
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	steps := int((eval.Silence-eval.Tolerance)/interval) + 1
	if steps < 1 {
		steps = 1
	}
	eval.Probability = cfg.InitialProbability + float64(steps-1)*cfg.ProbabilityRampPerHeartbeat
	if eval.Probability > cfg.MaxProbability {
		eval.Probability = cfg.MaxProbability
	}
	if eval.Probability <= 0 {
		return eval
	}
	eval.Triggered = roll <= eval.Probability
	return eval
}

func heartbeatIntervalDuration(minutes int) time.Duration {
	if minutes == 0 {
		minutes = 30
	}
	if minutes < 5 {
		minutes = 5
	}
	return time.Duration(minutes) * time.Minute
}

func effectiveProactiveTolerance(cfg config.HeartbeatProactiveConfig, rel NPCRelationship) time.Duration {
	score := proactiveRelationshipScore(rel.Affinity) +
		proactiveRelationshipScore(rel.Trust) +
		proactiveRelationshipScore(rel.Familiarity)
	minutes := cfg.BaseToleranceMinutes - score*cfg.RelationshipStepMinutes
	if minutes < cfg.MinToleranceMinutes {
		minutes = cfg.MinToleranceMinutes
	}
	return time.Duration(minutes) * time.Minute
}

func proactiveRelationshipScore(level NPCLevel) int {
	switch normalizeNPCLevel(level, NPCLevelLow) {
	case NPCLevelHigh:
		return 2
	case NPCLevelMid:
		return 1
	default:
		return 0
	}
}

func lastConversationAt(rel NPCRelationship) (time.Time, bool) {
	var latest time.Time
	hasLatest := false
	for _, candidate := range []string{rel.LastUserMessageAt, rel.LastAgentMessageAt, rel.LastInteractionAt} {
		parsed, ok := parseRFC3339(candidate)
		if !ok {
			continue
		}
		if !hasLatest || parsed.After(latest) {
			latest = parsed
			hasLatest = true
		}
	}
	return latest, hasLatest
}

func cooldownActive(rel NPCRelationship, cfg config.HeartbeatProactiveConfig, now time.Time) bool {
	if cfg.CooldownMinutes <= 0 {
		return false
	}
	lastSuccess, ok := parseRFC3339(rel.LastProactiveSuccessAt)
	if !ok {
		return false
	}
	return now.Sub(lastSuccess) < time.Duration(cfg.CooldownMinutes)*time.Minute
}
