package providers

import "testing"

func TestHTTPProviderSupportsMessageMedia(t *testing.T) {
	provider := NewHTTPProvider("test-key", "https://example.test/v1", "")
	supporter, ok := any(provider).(MessageMediaSupporter)
	if !ok {
		t.Fatal("HTTPProvider should advertise Message.Media support")
	}
	if !supporter.SupportsMessageMedia() {
		t.Fatal("HTTPProvider should support Message.Media serialization")
	}
}
