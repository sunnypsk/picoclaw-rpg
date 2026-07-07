package ui

import (
	"os"
	"strings"
	"testing"
)

func TestModelReferenceSyncIncludesMaintenanceModelName(t *testing.T) {
	raw, err := os.ReadFile("index.html")
	if err != nil {
		t.Fatalf("ReadFile(index.html) error = %v", err)
	}
	source := string(raw)

	for _, want := range []string{
		"defaults.maintenance_model_name === name",
		"delete defaults.maintenance_model_name",
		"defaults.maintenance_model_name === previousName",
		"defaults.maintenance_model_name = newName",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("index.html missing maintenance model reference sync snippet %q", want)
		}
	}
}
