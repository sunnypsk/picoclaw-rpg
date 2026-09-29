package routing

import "strings"

// ScheduledSourceInput preserves valid peer kinds while rebuilding missing or
// internal execution-session origins from the actual delivery destination.
func ScheduledSourceInput(channel, destination, session string, source *RoutePeer) RouteInput {
	channel = strings.ToLower(strings.TrimSpace(channel))
	destination = strings.TrimSpace(destination)
	input := RouteInput{Channel: channel}
	if parsed := ParseAgentSessionKey(session); parsed != nil {
		parts := strings.Split(parsed.Rest, ":")
		switch {
		case len(parts) == 2 && parts[0] == "direct" && strings.EqualFold(parts[1], destination):
			input.Peer = &RoutePeer{Kind: "direct", ID: destination}
		case len(parts) == 3 && strings.EqualFold(parts[0], channel) && validSourceKind(parts[1]) && strings.EqualFold(parts[2], destination):
			input.Peer = &RoutePeer{Kind: parts[1], ID: destination}
		case len(parts) == 4 && strings.EqualFold(parts[0], channel) && parts[2] == "direct" && strings.EqualFold(parts[3], destination):
			input.AccountID = parts[1]
			input.Peer = &RoutePeer{Kind: "direct", ID: destination}
		}
	}
	if source != nil && strings.EqualFold(source.ID, destination) && validSourceKind(source.Kind) {
		input.Peer = &RoutePeer{Kind: strings.ToLower(source.Kind), ID: destination}
	}
	if input.Peer == nil && destination != "" {
		input.Peer = &RoutePeer{Kind: "direct", ID: destination}
	}
	if input.Peer != nil && strings.HasSuffix(strings.ToLower(destination), "@g.us") {
		input.Peer.Kind = "group"
	}
	return input
}

func validSourceKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "direct", "group", "channel":
		return true
	}
	return false
}

func IsConversationSource(session string) bool {
	parsed := ParseAgentSessionKey(session)
	if parsed == nil {
		return false
	}
	rest := strings.ToLower(parsed.Rest)
	return rest != "" && !strings.HasPrefix(rest, "scheduled-reminder:") && !strings.HasPrefix(rest, "heartbeat")
}
