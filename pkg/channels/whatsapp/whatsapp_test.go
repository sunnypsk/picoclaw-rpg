package whatsapp

import (
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
)

func TestWhatsAppBridgeMessageSubtype(t *testing.T) {
	msg := map[string]any{bus.MetadataMessageSubtype: bus.MessageSubtypeVoiceNote}
	if got := whatsAppBridgeMessageSubtype(msg); got != bus.MessageSubtypeVoiceNote {
		t.Fatalf("bridge voice subtype = %q, want %q", got, bus.MessageSubtypeVoiceNote)
	}

	if got := whatsAppBridgeMessageSubtype(map[string]any{bus.MetadataMessageSubtype: "audio"}); got != "" {
		t.Fatalf("unexpected bridge subtype passthrough: %q", got)
	}

	if got := whatsAppBridgeMessageSubtype(nil); got != "" {
		t.Fatalf("nil payload subtype = %q, want empty", got)
	}
}
