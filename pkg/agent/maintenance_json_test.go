package agent

import (
	"strings"
	"testing"
)

func TestMaintenancePreviewForLogRedactsPrefixedSecretKeys(t *testing.T) {
	input := `provider returned {"OPENAI_API_KEY":"sk-live-secret",` +
		`"PICOCLAW_API_KEY":"pc-secret","access_token":"tok-secret"} ` +
		`client-secret=client-secret-value url=https://internal.example/v1`

	preview := maintenancePreviewForLog(input, 1000)

	for _, leaked := range []string{
		"sk-live-secret",
		"pc-secret",
		"tok-secret",
		"client-secret-value",
		"https://internal.example/v1",
	} {
		if strings.Contains(preview, leaked) {
			t.Fatalf("preview leaked %q: %s", leaked, preview)
		}
	}
	for _, key := range []string{"OPENAI_API_KEY", "PICOCLAW_API_KEY", "access_token", "client-secret"} {
		if !strings.Contains(preview, key) {
			t.Fatalf("preview missing redacted key %q: %s", key, preview)
		}
	}
}
