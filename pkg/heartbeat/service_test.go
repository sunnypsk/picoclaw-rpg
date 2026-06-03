package heartbeat

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/tools"
)

func TestJitteredHeartbeatInterval(t *testing.T) {
	base := 60 * time.Minute

	tests := []struct {
		name string
		roll float64
		want time.Duration
	}{
		{name: "minimum roll", roll: 0, want: 48 * time.Minute},
		{name: "middle roll", roll: 0.5, want: 60 * time.Minute},
		{name: "maximum roll", roll: 1, want: 72 * time.Minute},
		{name: "negative roll clamps", roll: -1, want: 48 * time.Minute},
		{name: "high roll clamps", roll: 2, want: 72 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jitteredHeartbeatInterval(base, tt.roll)
			if got != tt.want {
				t.Fatalf("jitteredHeartbeatInterval(%s, %v) = %s, want %s", base, tt.roll, got, tt.want)
			}
		})
	}
}

func TestJitteredHeartbeatIntervalClampsToMinimum(t *testing.T) {
	got := jitteredHeartbeatInterval(5*time.Minute, 0)
	if got != 5*time.Minute {
		t.Fatalf("jitteredHeartbeatInterval() = %s, want minimum 5m", got)
	}
}

func TestSilentPeriodActive(t *testing.T) {
	hs := NewHeartbeatService("", 30, true)
	if err := hs.SetSilentPeriod(true, "01:00", "06:00"); err != nil {
		t.Fatalf("SetSilentPeriod() error = %v", err)
	}

	tests := []struct {
		name string
		hour int
		min  int
		want bool
	}{
		{name: "before start", hour: 0, min: 59, want: false},
		{name: "at start", hour: 1, min: 0, want: true},
		{name: "middle", hour: 3, min: 30, want: true},
		{name: "just before end", hour: 5, min: 59, want: true},
		{name: "at end", hour: 6, min: 0, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 1, 1, tt.hour, tt.min, 0, 0, time.Local)
			got := hs.isSilentPeriodActive(now)
			if got != tt.want {
				t.Fatalf("isSilentPeriodActive(%02d:%02d) = %t, want %t", tt.hour, tt.min, got, tt.want)
			}
		})
	}
}

func TestSilentPeriodActiveOvernight(t *testing.T) {
	hs := NewHeartbeatService("", 30, true)
	if err := hs.SetSilentPeriod(true, "23:00", "06:00"); err != nil {
		t.Fatalf("SetSilentPeriod() error = %v", err)
	}

	tests := []struct {
		name string
		hour int
		want bool
	}{
		{name: "before start", hour: 22, want: false},
		{name: "after start", hour: 23, want: true},
		{name: "after midnight", hour: 3, want: true},
		{name: "at end", hour: 6, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 1, 1, tt.hour, 0, 0, 0, time.Local)
			got := hs.isSilentPeriodActive(now)
			if got != tt.want {
				t.Fatalf("isSilentPeriodActive(%02d:00) = %t, want %t", tt.hour, got, tt.want)
			}
		})
	}
}

func TestSetSilentPeriodRejectsInvalidTime(t *testing.T) {
	hs := NewHeartbeatService("", 30, true)

	tests := []struct {
		name  string
		start string
		end   string
	}{
		{name: "missing leading zero", start: "1:00", end: "06:00"},
		{name: "bad hour", start: "24:00", end: "06:00"},
		{name: "bad minute", start: "01:60", end: "06:00"},
		{name: "same time", start: "01:00", end: "01:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := hs.SetSilentPeriod(true, tt.start, tt.end); err == nil {
				t.Fatal("SetSilentPeriod() error = nil, want error")
			}
		})
	}
}

func TestExecuteScheduledHeartbeatSkipsSilentPeriod(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hs := NewHeartbeatService(tmpDir, 30, true)
	hs.stopChan = make(chan struct{})
	if err := hs.SetSilentPeriod(true, "01:00", "06:00"); err != nil {
		t.Fatalf("SetSilentPeriod() error = %v", err)
	}

	handlerCalls := 0
	tickCalls := 0
	hs.SetHandler(func(prompt, channel, chatID string) *tools.ToolResult {
		handlerCalls++
		return tools.SilentResult("ok")
	})
	hs.SetTickHandler(func() {
		tickCalls++
	})
	if err := os.WriteFile(filepath.Join(tmpDir, "HEARTBEAT.md"), []byte("Test task"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	hs.executeScheduledHeartbeat(time.Date(2026, 1, 1, 2, 0, 0, 0, time.Local))
	if handlerCalls != 0 {
		t.Fatalf("handler calls during silent period = %d, want 0", handlerCalls)
	}
	if tickCalls != 0 {
		t.Fatalf("tick calls during silent period = %d, want 0", tickCalls)
	}

	hs.executeScheduledHeartbeat(time.Date(2026, 1, 1, 6, 0, 0, 0, time.Local))
	if handlerCalls != 1 {
		t.Fatalf("handler calls after silent period = %d, want 1", handlerCalls)
	}
	if tickCalls != 1 {
		t.Fatalf("tick calls after silent period = %d, want 1", tickCalls)
	}
}

func TestExecuteHeartbeat_Async(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hs := NewHeartbeatService(tmpDir, 30, true)
	hs.stopChan = make(chan struct{}) // Enable for testing

	asyncCalled := false
	asyncResult := &tools.ToolResult{
		ForLLM:  "Background task started",
		ForUser: "Task started in background",
		Silent:  false,
		IsError: false,
		Async:   true,
	}

	hs.SetHandler(func(prompt, channel, chatID string) *tools.ToolResult {
		asyncCalled = true
		if prompt == "" {
			t.Error("Expected non-empty prompt")
		}
		return asyncResult
	})

	// Create HEARTBEAT.md
	os.WriteFile(filepath.Join(tmpDir, "HEARTBEAT.md"), []byte("Test task"), 0o644)

	// Execute heartbeat directly (internal method for testing)
	hs.executeHeartbeat()

	if !asyncCalled {
		t.Error("Expected handler to be called")
	}
}

func TestExecuteHeartbeat_RunsTickHandlerWithoutPrompt(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hs := NewHeartbeatService(tmpDir, 30, true)
	hs.stopChan = make(chan struct{})

	tickCalled := false
	handlerCalled := false
	hs.SetTickHandler(func() {
		tickCalled = true
	})
	hs.SetHandler(func(prompt, channel, chatID string) *tools.ToolResult {
		handlerCalled = true
		return tools.SilentResult("ok")
	})

	hs.executeHeartbeat()

	if !tickCalled {
		t.Fatal("expected tick handler to run even without HEARTBEAT.md prompt")
	}
	if handlerCalled {
		t.Fatal("handler should not run when HEARTBEAT.md prompt is missing")
	}
}

func TestExecuteHeartbeat_ResultLogging(t *testing.T) {
	tests := []struct {
		name    string
		result  *tools.ToolResult
		wantLog string
	}{
		{
			name: "error result",
			result: &tools.ToolResult{
				ForLLM:  "Heartbeat failed: connection error",
				ForUser: "",
				Silent:  false,
				IsError: true,
				Async:   false,
			},
			wantLog: "error message",
		},
		{
			name: "silent result",
			result: &tools.ToolResult{
				ForLLM:  "Heartbeat completed successfully",
				ForUser: "",
				Silent:  true,
				IsError: false,
				Async:   false,
			},
			wantLog: "completion message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			hs := NewHeartbeatService(tmpDir, 30, true)
			hs.stopChan = make(chan struct{}) // Enable for testing

			hs.SetHandler(func(prompt, channel, chatID string) *tools.ToolResult {
				return tt.result
			})

			os.WriteFile(filepath.Join(tmpDir, "HEARTBEAT.md"), []byte("Test task"), 0o644)
			hs.executeHeartbeat()

			logFile := filepath.Join(tmpDir, "heartbeat.log")
			data, err := os.ReadFile(logFile)
			if err != nil {
				t.Fatalf("Failed to read log file: %v", err)
			}
			if string(data) == "" {
				t.Errorf("Expected log file to contain %s", tt.wantLog)
			}
		})
	}
}

func TestHeartbeatService_StartStop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hs := NewHeartbeatService(tmpDir, 1, true)

	err = hs.Start()
	if err != nil {
		t.Fatalf("Failed to start heartbeat service: %v", err)
	}

	hs.Stop()

	time.Sleep(100 * time.Millisecond)
}

func TestHeartbeatService_Disabled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hs := NewHeartbeatService(tmpDir, 1, false)

	if hs.enabled != false {
		t.Error("Expected service to be disabled")
	}

	err = hs.Start()
	_ = err // Disabled service returns nil
}

func TestExecuteHeartbeat_NilResult(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hs := NewHeartbeatService(tmpDir, 30, true)
	hs.stopChan = make(chan struct{}) // Enable for testing

	hs.SetHandler(func(prompt, channel, chatID string) *tools.ToolResult {
		return nil
	})

	// Create HEARTBEAT.md
	os.WriteFile(filepath.Join(tmpDir, "HEARTBEAT.md"), []byte("Test task"), 0o644)

	// Should not panic with nil result
	hs.executeHeartbeat()
}

func TestExecuteHeartbeat_TickHandlerRunsWithoutPrompt(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hs := NewHeartbeatService(tmpDir, 30, true)
	hs.stopChan = make(chan struct{})

	tickCalled := false
	hs.SetTickHandler(func() {
		tickCalled = true
	})

	hs.executeHeartbeat()

	if !tickCalled {
		t.Fatal("expected tick handler to run even without HEARTBEAT.md content")
	}
}

// TestLogPath verifies heartbeat log is written to workspace directory
func TestLogPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hs := NewHeartbeatService(tmpDir, 30, true)

	// Write a log entry
	hs.logf("INFO", "Test log entry")

	// Verify log file exists at workspace root
	expectedLogPath := filepath.Join(tmpDir, "heartbeat.log")
	if _, err := os.Stat(expectedLogPath); os.IsNotExist(err) {
		t.Errorf("Expected log file at %s, but it doesn't exist", expectedLogPath)
	}
}

// TestHeartbeatFilePath verifies HEARTBEAT.md is at workspace root
func TestHeartbeatFilePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hs := NewHeartbeatService(tmpDir, 30, true)

	// Trigger default template creation
	hs.buildPrompt()

	// Verify HEARTBEAT.md exists at workspace root
	expectedPath := filepath.Join(tmpDir, "HEARTBEAT.md")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Expected HEARTBEAT.md at %s, but it doesn't exist", expectedPath)
	}
}
