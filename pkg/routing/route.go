package routing

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"

	"github.com/sipeed/picoclaw/pkg/config"
)

// RouteInput contains the routing context from an inbound message.
type RouteInput struct {
	Channel    string
	AccountID  string
	Peer       *RoutePeer
	ParentPeer *RoutePeer
	GuildID    string
	TeamID     string
}

// ResolvedRoute is the result of agent routing.
type ResolvedRoute struct {
	AgentID        string
	Channel        string
	AccountID      string
	SessionKey     string
	MainSessionKey string
	MatchedBy      string // "binding.peer", "binding.peer.parent", "binding.guild", "binding.team", "binding.account", "binding.channel", "auto-provision", "default"
}

// RouteResolver determines which agent handles a message based on config bindings.
type RouteResolver struct {
	cfg *config.Config
}

// NewRouteResolver creates a new route resolver.
func NewRouteResolver(cfg *config.Config) *RouteResolver {
	return &RouteResolver{cfg: cfg}
}

// ResolveRoute determines which agent handles the message and constructs session keys.
// Priority cascade:
// 1) strict auto_provision (when enabled)
// 2) peer > parent_peer > guild > team > account > channel_wildcard > auto_provision > default
func (r *RouteResolver) ResolveRoute(input RouteInput) ResolvedRoute {
	channel := strings.ToLower(strings.TrimSpace(input.Channel))
	accountID := NormalizeAccountID(input.AccountID)
	peer := input.Peer

	dmScope := DMScope(r.cfg.Session.DMScope)
	if dmScope == "" {
		dmScope = DMScopeMain
	}
	identityLinks := r.cfg.Session.IdentityLinks

	bindings := r.filterBindings(channel, accountID)

	choose := func(agentID string, matchedBy string, allowUnlisted bool) ResolvedRoute {
		resolvedAgentID := ""
		if allowUnlisted {
			resolvedAgentID = NormalizeAgentID(agentID)
			if resolvedAgentID == "" {
				resolvedAgentID = NormalizeAgentID(r.resolveDefaultAgentID())
			}
		} else {
			resolvedAgentID = r.pickAgentID(agentID)
		}
		sessionKey := strings.ToLower(BuildAgentPeerSessionKey(SessionKeyParams{
			AgentID:       resolvedAgentID,
			Channel:       channel,
			AccountID:     accountID,
			Peer:          peer,
			DMScope:       dmScope,
			IdentityLinks: identityLinks,
		}))
		mainSessionKey := strings.ToLower(BuildAgentMainSessionKey(resolvedAgentID))
		return ResolvedRoute{
			AgentID:        resolvedAgentID,
			Channel:        channel,
			AccountID:      accountID,
			SessionKey:     sessionKey,
			MainSessionKey: mainSessionKey,
			MatchedBy:      matchedBy,
		}
	}

	// Strict one-to-one mode: for enabled peer kinds, force deterministic dedicated
	// auto-provisioned agent IDs and bypass binding rules.
	if r.cfg.Agents.AutoProvision.StrictOneToOne {
		if autoID, ok := r.resolveAutoProvisionAgentID(channel, accountID, peer); ok {
			return choose(autoID, "auto-provision", true)
		}
	}

	// Priority 1: Peer binding
	if peer != nil && strings.TrimSpace(peer.ID) != "" {
		if match := r.findPeerMatch(bindings, peer); match != nil {
			return choose(match.AgentID, "binding.peer", false)
		}
	}

	// Priority 2: Parent peer binding
	parentPeer := input.ParentPeer
	if parentPeer != nil && strings.TrimSpace(parentPeer.ID) != "" {
		if match := r.findPeerMatch(bindings, parentPeer); match != nil {
			return choose(match.AgentID, "binding.peer.parent", false)
		}
	}

	// Priority 3: Guild binding
	guildID := strings.TrimSpace(input.GuildID)
	if guildID != "" {
		if match := r.findGuildMatch(bindings, guildID); match != nil {
			return choose(match.AgentID, "binding.guild", false)
		}
	}

	// Priority 4: Team binding
	teamID := strings.TrimSpace(input.TeamID)
	if teamID != "" {
		if match := r.findTeamMatch(bindings, teamID); match != nil {
			return choose(match.AgentID, "binding.team", false)
		}
	}

	// Priority 5: Account binding
	if match := r.findAccountMatch(bindings); match != nil {
		return choose(match.AgentID, "binding.account", false)
	}

	// Priority 6: Channel wildcard binding
	if match := r.findChannelWildcardMatch(bindings); match != nil {
		return choose(match.AgentID, "binding.channel", false)
	}

	// Priority 7: Auto-provisioned dedicated agent for unmatched peers
	if autoID, ok := r.resolveAutoProvisionAgentID(channel, accountID, peer); ok {
		return choose(autoID, "auto-provision", true)
	}

	// Priority 8: Default agent
	return choose(r.resolveDefaultAgentID(), "default", false)
}

func (r *RouteResolver) filterBindings(channel, accountID string) []config.AgentBinding {
	var filtered []config.AgentBinding
	for _, b := range r.cfg.Bindings {
		matchChannel := strings.ToLower(strings.TrimSpace(b.Match.Channel))
		if matchChannel == "" || matchChannel != channel {
			continue
		}
		if !matchesAccountID(b.Match.AccountID, accountID) {
			continue
		}
		filtered = append(filtered, b)
	}
	return filtered
}

func matchesAccountID(matchAccountID, actual string) bool {
	trimmed := strings.TrimSpace(matchAccountID)
	if trimmed == "" {
		return actual == DefaultAccountID
	}
	if trimmed == "*" {
		return true
	}
	return strings.ToLower(trimmed) == strings.ToLower(actual)
}

func (r *RouteResolver) findPeerMatch(bindings []config.AgentBinding, peer *RoutePeer) *config.AgentBinding {
	for i := range bindings {
		b := &bindings[i]
		if b.Match.Peer == nil {
			continue
		}
		peerKind := strings.ToLower(strings.TrimSpace(b.Match.Peer.Kind))
		peerID := strings.TrimSpace(b.Match.Peer.ID)
		if peerKind == "" || peerID == "" {
			continue
		}
		if peerKind == strings.ToLower(peer.Kind) && peerID == peer.ID {
			return b
		}
	}
	return nil
}

func (r *RouteResolver) findGuildMatch(bindings []config.AgentBinding, guildID string) *config.AgentBinding {
	for i := range bindings {
		b := &bindings[i]
		matchGuild := strings.TrimSpace(b.Match.GuildID)
		if matchGuild != "" && matchGuild == guildID {
			return &bindings[i]
		}
	}
	return nil
}

func (r *RouteResolver) findTeamMatch(bindings []config.AgentBinding, teamID string) *config.AgentBinding {
	for i := range bindings {
		b := &bindings[i]
		matchTeam := strings.TrimSpace(b.Match.TeamID)
		if matchTeam != "" && matchTeam == teamID {
			return &bindings[i]
		}
	}
	return nil
}

func (r *RouteResolver) findAccountMatch(bindings []config.AgentBinding) *config.AgentBinding {
	for i := range bindings {
		b := &bindings[i]
		accountID := strings.TrimSpace(b.Match.AccountID)
		if accountID == "*" {
			continue
		}
		if b.Match.Peer != nil || b.Match.GuildID != "" || b.Match.TeamID != "" {
			continue
		}
		return &bindings[i]
	}
	return nil
}

func (r *RouteResolver) findChannelWildcardMatch(bindings []config.AgentBinding) *config.AgentBinding {
	for i := range bindings {
		b := &bindings[i]
		accountID := strings.TrimSpace(b.Match.AccountID)
		if accountID != "*" {
			continue
		}
		if b.Match.Peer != nil || b.Match.GuildID != "" || b.Match.TeamID != "" {
			continue
		}
		return &bindings[i]
	}
	return nil
}

func (r *RouteResolver) pickAgentID(agentID string) string {
	trimmed := strings.TrimSpace(agentID)
	if trimmed == "" {
		return NormalizeAgentID(r.resolveDefaultAgentID())
	}
	normalized := NormalizeAgentID(trimmed)
	agents := r.cfg.Agents.List
	if len(agents) == 0 {
		return normalized
	}
	for _, a := range agents {
		if NormalizeAgentID(a.ID) == normalized {
			return normalized
		}
	}
	return NormalizeAgentID(r.resolveDefaultAgentID())
}

func (r *RouteResolver) resolveAutoProvisionAgentID(channel, accountID string, peer *RoutePeer) (string, bool) {
	if peer == nil {
		return "", false
	}
	peerKind := strings.ToLower(strings.TrimSpace(peer.Kind))
	peerID := strings.TrimSpace(peer.ID)
	if peerKind == "" || peerID == "" {
		return "", false
	}
	if !r.cfg.Agents.AutoProvision.IsPeerKindEnabled(peerKind) {
		return "", false
	}
	return buildAutoProvisionAgentID(channel, accountID, peerKind, peerID), true
}

func buildAutoProvisionAgentID(channel, accountID, peerKind, peerID string) string {
	channelToken := normalizeAutoIDToken(channel, 16, "channel")
	kindToken := normalizeAutoIDToken(peerKind, 10, "peer")
	peerToken := normalizeAutoIDToken(peerID, 24, "id")
	hash := autoIDHash(channel + "|" + accountID + "|" + peerKind + "|" + peerID)

	id := "auto-" + channelToken + "-" + kindToken + "-" + peerToken + "-" + hash
	return NormalizeAgentID(id)
}

func normalizeAutoIDToken(raw string, maxLen int, fallback string) string {
	trimmed := strings.TrimSpace(raw)
	token := ""
	if trimmed == "" {
		token = fallback
	} else {
		token = NormalizeAccountID(trimmed)
		if token == DefaultAccountID && strings.ToLower(trimmed) != DefaultAccountID {
			token = fallback
		}
	}
	if maxLen > 0 && len(token) > maxLen {
		token = token[:maxLen]
	}
	return token
}

func autoIDHash(value string) string {
	sum := sha1.Sum([]byte(strings.ToLower(strings.TrimSpace(value))))
	encoded := hex.EncodeToString(sum[:])
	if len(encoded) < 8 {
		return encoded
	}
	return encoded[:8]
}

func (r *RouteResolver) resolveDefaultAgentID() string {
	agents := r.cfg.Agents.List
	if len(agents) == 0 {
		return DefaultAgentID
	}
	for _, a := range agents {
		if a.Default {
			id := strings.TrimSpace(a.ID)
			if id != "" {
				return NormalizeAgentID(id)
			}
		}
	}
	if id := strings.TrimSpace(agents[0].ID); id != "" {
		return NormalizeAgentID(id)
	}
	return DefaultAgentID
}
