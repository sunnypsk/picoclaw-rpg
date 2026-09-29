package cron

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/routing"
)

func TestRepairPreviewHashBackupAndUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	data := []byte(`{"version":1,"unknown_root":{"keep":true},"jobs":[{"id":"9b31a8aa031e4762","enabled":true,"schedule":{"kind":"cron","expr":"15 9 * * *","unknown":9},"payload":{"channel":"whatsapp_native","to":"123@g.us","deliver":false,"message":"private content","session_key":"agent:auto-whatsapp_native-direct-123:scheduled-reminder:0832","future":42},"future_job":"keep"},{"id":"other","keep":"unchanged"}]}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	resolver := routing.NewRouteResolver(config.DefaultConfig())
	minimum := 2
	options := RepairOptions{ID: "9b31a8aa031e4762", Timezone: "Asia/Hong_Kong", MinVerifiedSources: &minimum}
	report, hash, err := Repair(path, options, resolver)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(data, after) {
		t.Fatal("preview wrote file")
	}
	if len(report) != 1 || !strings.Contains(report[0].After, ":group:") {
		t.Fatalf("bad route: %+v", report)
	}
	options.Apply = true
	options.ExpectedHash = "wrong"
	if _, _, err = Repair(path, options, resolver); err == nil {
		t.Fatal("accepted stale hash")
	}
	options.ExpectedHash = hash
	if _, _, err = Repair(path, options, resolver); err != nil {
		t.Fatal(err)
	}
	backup, _ := os.ReadFile(path + ".backup-" + hash)
	if !bytes.Equal(backup, data) {
		t.Fatal("backup not exact")
	}
	after, _ = os.ReadFile(path)
	for _, want := range []string{`"future": 42`, `"future_job": "keep"`, `"keep": "unchanged"`, `"deliver": false`, `"expr": "15 9 * * *"`, `"min_verified_sources": 2`} {
		if !bytes.Contains(after, []byte(want)) {
			t.Fatalf("lost field %s", want)
		}
	}
	_, newHash, err := Repair(path, RepairOptions{ID: options.ID}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	options.ExpectedHash = newHash
	if _, _, err = Repair(path, options, resolver); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(path)
	if !bytes.Equal(after, again) {
		t.Fatal("repair not idempotent")
	}
}

func TestCronUsesExplicitTimezone(t *testing.T) {
	service := NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	next := service.computeNextRun(&CronSchedule{Kind: "cron", Expr: "15 9 * * *", TZ: "Asia/Hong_Kong"}, now.UnixMilli())
	want := time.Date(2026, 9, 29, 1, 15, 0, 0, time.UTC).UnixMilli()
	if next == nil || *next != want {
		t.Fatalf("next=%v want=%d", next, want)
	}
}

func TestRepairPreservesNonWhatsAppGroupSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	data := []byte(`{"jobs":[{"id":"slack","schedule":{"kind":"cron","expr":"15 9 * * *"},"payload":{"channel":"slack","to":"C123","source_peer":{"Kind":"channel","ID":"C123"},"session_key":"agent:main:slack:channel:c123"}}]}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	report, _, err := Repair(path, RepairOptions{ID: "slack", Timezone: "Asia/Hong_Kong"}, routing.NewRouteResolver(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if len(report) != 1 || report[0].After != "agent:main:slack:channel:c123" {
		t.Fatalf("lost channel: %+v", report)
	}
}

func TestFailedExecutionPersistsErrorStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	service := NewCronService(path, func(*CronJob) (string, error) { return "", errors.New("verification failed") })
	interval := int64(60000)
	job, err := service.AddJob("test", CronSchedule{Kind: "every", EveryMS: &interval}, "content", false, "test", "chat", "")
	if err != nil {
		t.Fatal(err)
	}
	service.executeJobByID(job.ID)
	restored := NewCronService(path, nil).ListJobs(true)
	if len(restored) != 1 || restored[0].State.LastStatus != "error" || restored[0].State.LastError != "verification failed" {
		t.Fatalf("failure not persisted: %+v", restored)
	}
}
