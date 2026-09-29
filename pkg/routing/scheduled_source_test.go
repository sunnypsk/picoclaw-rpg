package routing

import "testing"

func TestScheduledSourcePreservesKindsAndCase(t *testing.T) {
	for _, tc := range []struct{ channel, id, kind, session string }{
		{"slack", "C123", "channel", "agent:main:slack:channel:c123"},
		{"telegram", "-123", "group", "agent:main:telegram:group:-123"},
		{"whatsapp_native", "123@g.us", "group", "agent:wrong:scheduled-reminder:old"},
	} {
		for _, peer := range []*RoutePeer{nil, {Kind: tc.kind, ID: tc.id}} {
			input := ScheduledSourceInput(tc.channel, tc.id, tc.session, peer)
			if input.Peer == nil || input.Peer.ID != tc.id || input.Peer.Kind != tc.kind {
				t.Fatalf("source changed: %+v", input.Peer)
			}
		}
	}
}
