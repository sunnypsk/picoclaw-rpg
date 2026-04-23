package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/cron"
)

func TestCronToolDescriptionMentionsScopedListAndToolResults(t *testing.T) {
	tool, _ := newTestCronTool(t)
	desc := tool.Description()
	if !strings.Contains(desc, "inspect enabled reminders for the current conversation") {
		t.Fatalf("expected cron description to scope list to the current conversation, got %q", desc)
	}
	if !strings.Contains(desc, "tool result itself as confirmation") {
		t.Fatalf("expected cron description to use tool results as confirmation, got %q", desc)
	}
	if strings.Contains(desc, "Re-check with list") {
		t.Fatalf("cron description should not recommend re-checking with list, got %q", desc)
	}
}

func TestCronTool_ListJob_IsScopedToCurrentConversation(t *testing.T) {
	tool, service := newTestCronTool(t)

	addEveryJob(t, service, "stretch in chat 1", "telegram", "chat-1")
	addEveryJob(t, service, "stretch in chat 2", "telegram", "chat-2")

	ctx := WithToolContext(context.Background(), "telegram", "chat-1")
	result := tool.Execute(ctx, map[string]any{
		"action": "list",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "stretch in chat 1") {
		t.Fatalf("expected current chat job to appear in list, got %q", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, "stretch in chat 2") {
		t.Fatalf("did not expect other chat job in list, got %q", result.ForLLM)
	}
}

func TestCronTool_ListJob_OmitsDisabledJobs(t *testing.T) {
	tool, service := newTestCronTool(t)

	addEveryJob(t, service, "stretch in chat 1", "telegram", "chat-1")
	jobs := service.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	disableResult := tool.Execute(context.Background(), map[string]any{
		"action": "disable",
		"job_id": jobs[0].ID,
	})
	if disableResult.IsError {
		t.Fatalf("expected disable success, got error: %s", disableResult.ForLLM)
	}

	listResult := tool.Execute(WithToolContext(context.Background(), "telegram", "chat-1"), map[string]any{
		"action": "list",
	})
	if listResult.IsError {
		t.Fatalf("expected list success, got error: %s", listResult.ForLLM)
	}
	if strings.Contains(listResult.ForLLM, "stretch in chat 1") {
		t.Fatalf("expected disabled job to be omitted from list, got %q", listResult.ForLLM)
	}
	if listResult.ForLLM != "No scheduled jobs" {
		t.Fatalf("expected no enabled jobs after disable, got %q", listResult.ForLLM)
	}
}

func TestCronTool_AddJob_WithNaturalRequest(t *testing.T) {
	tool, service := newTestCronTool(t)

	ctx := WithToolContext(context.Background(), "telegram", "chat-1")
	result := tool.Execute(ctx, map[string]any{
		"action":          "add",
		"natural_request": "remind me in 10 minutes to stretch",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}

	jobs := service.ListJobs(false)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	job := jobs[0]
	if job.Schedule.Kind != "at" {
		t.Fatalf("expected one-time schedule, got %s", job.Schedule.Kind)
	}
	if !strings.Contains(strings.ToLower(job.Payload.Message), "stretch") {
		t.Fatalf("expected message to include 'stretch', got %q", job.Payload.Message)
	}
}

func TestCronTool_AddJob_DerivesMessageFromNaturalRequestWithExplicitSchedule(t *testing.T) {
	tool, service := newTestCronTool(t)

	ctx := WithToolContext(context.Background(), "telegram", "chat-1")
	result := tool.Execute(ctx, map[string]any{
		"action":          "add",
		"at_seconds":      600,
		"natural_request": "remind me to stretch",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}

	jobs := service.ListJobs(false)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	job := jobs[0]
	if job.Schedule.Kind != "at" {
		t.Fatalf("expected one-time schedule, got %s", job.Schedule.Kind)
	}
	if !strings.Contains(strings.ToLower(job.Payload.Message), "stretch") {
		t.Fatalf("expected derived message to include 'stretch', got %q", job.Payload.Message)
	}
}

func TestCronTool_AddJob_StoresRoutedSessionKey(t *testing.T) {
	tool, service := newTestCronTool(t)

	ctx := WithToolExecutionContext(
		context.Background(),
		"telegram",
		"chat-1",
		"msg-1",
		"user-1",
		"agent:main:telegram:direct:user-1",
		nil,
	)
	result := tool.Execute(ctx, map[string]any{
		"action":     "add",
		"message":    "stretch",
		"at_seconds": 60,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}

	jobs := service.ListJobs(false)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Payload.SessionKey != "agent:main:telegram:direct:user-1" {
		t.Fatalf("expected session key to be stored, got %q", jobs[0].Payload.SessionKey)
	}
}

func TestCronTool_AddJob_IgnoresZeroValuedOptionalScheduleArgs(t *testing.T) {
	tool, service := newTestCronTool(t)

	ctx := WithToolContext(context.Background(), "telegram", "chat-1")
	result := tool.Execute(ctx, map[string]any{
		"action":        "add",
		"message":       "stretch",
		"at_seconds":    0,
		"every_seconds": 7200,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}

	jobs := service.ListJobs(false)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	job := jobs[0]
	if job.Schedule.Kind != "every" {
		t.Fatalf("expected recurring schedule, got %s", job.Schedule.Kind)
	}
	if job.Schedule.EveryMS == nil || *job.Schedule.EveryMS != 7200*1000 {
		t.Fatalf("expected every_seconds schedule to be preserved, got %+v", job.Schedule)
	}
}

func TestCronTool_AddJob_WithAmbiguousNaturalRequest(t *testing.T) {
	tool, _ := newTestCronTool(t)

	ctx := WithToolContext(context.Background(), "telegram", "chat-1")
	result := tool.Execute(ctx, map[string]any{
		"action":          "add",
		"natural_request": "remind me later to stretch",
	})

	if !result.IsError {
		t.Fatal("expected error for ambiguous schedule")
	}
	if !strings.Contains(result.ForLLM, "couldn't infer schedule") {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
}

func TestCronTool_RemoveJob_ByQuerySingleMatch(t *testing.T) {
	tool, service := newTestCronTool(t)

	addEveryJob(t, service, "pay rent", "cli", "direct")
	addEveryJob(t, service, "water plants", "cli", "direct")

	ctx := WithToolContext(context.Background(), "cli", "direct")
	result := tool.Execute(ctx, map[string]any{
		"action": "remove",
		"query":  "pay rent",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}

	jobs := service.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 remaining job, got %d", len(jobs))
	}
	if !strings.Contains(strings.ToLower(jobs[0].Payload.Message), "water") {
		t.Fatalf("unexpected remaining job: %q", jobs[0].Payload.Message)
	}
}

func TestCronTool_RemoveJob_NaturalRequestUsesContextScope(t *testing.T) {
	tool, service := newTestCronTool(t)

	addEveryJob(t, service, "buy milk", "telegram", "chat-1")
	addEveryJob(t, service, "buy milk", "telegram", "chat-2")

	ctx := WithToolContext(context.Background(), "telegram", "chat-1")
	result := tool.Execute(ctx, map[string]any{
		"action":          "remove",
		"natural_request": "cancel that reminder",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}

	jobs := service.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 remaining job, got %d", len(jobs))
	}
	if jobs[0].Payload.To != "chat-2" {
		t.Fatalf("expected chat-2 reminder to remain, got chat_id=%q", jobs[0].Payload.To)
	}
}

func TestCronTool_RemoveJob_MultipleMatchesNeedsDisambiguation(t *testing.T) {
	tool, service := newTestCronTool(t)

	addEveryJob(t, service, "pay rent", "cli", "direct")
	addEveryJob(t, service, "pay electric bill", "cli", "direct")

	ctx := WithToolContext(context.Background(), "cli", "direct")
	result := tool.Execute(ctx, map[string]any{
		"action": "remove",
		"query":  "pay",
	})

	if !result.IsError {
		t.Fatal("expected disambiguation error")
	}
	if !strings.Contains(result.ForLLM, "Multiple jobs matched") {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	jobs := service.ListJobs(true)
	if len(jobs) != 2 {
		t.Fatalf("expected no deletions, got %d jobs", len(jobs))
	}
}

func newTestCronTool(t *testing.T) (*CronTool, *cron.CronService) {
	t.Helper()

	storePath := filepath.Join(t.TempDir(), "cron", "jobs.json")
	service := cron.NewCronService(storePath, nil)

	tool, err := NewCronTool(service, nil, nil, t.TempDir(), true, 0, nil)
	if err != nil {
		t.Fatalf("failed to create cron tool: %v", err)
	}

	return tool, service
}

func addEveryJob(t *testing.T, service *cron.CronService, message, channel, chatID string) {
	t.Helper()

	everyMS := int64(60 * 1000)
	_, err := service.AddJob(
		message,
		cron.CronSchedule{Kind: "every", EveryMS: &everyMS},
		message,
		true,
		channel,
		chatID,
		"",
	)
	if err != nil {
		t.Fatalf("failed to add job: %v", err)
	}
}
