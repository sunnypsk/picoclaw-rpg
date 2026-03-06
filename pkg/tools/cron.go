package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/cron"
	"github.com/sipeed/picoclaw/pkg/utils"
)

var (
	naturalAtPattern         = regexp.MustCompile(`(?i)\b(?:in|after)\s+(\d+)\s*(seconds?|secs?|minutes?|mins?|hours?|hrs?|days?)\b`)
	naturalEveryPattern      = regexp.MustCompile(`(?i)\bevery\s+(\d+)\s*(seconds?|secs?|minutes?|mins?|hours?|hrs?|days?|weeks?)\b`)
	naturalReminderLead      = regexp.MustCompile(`(?i)\b(?:please\s+)?(?:set\s+(?:a\s+)?)?(?:remind(?:\s+me)?|reminder|remember(?:\s+to)?)\b`)
	removeQuotedPattern      = regexp.MustCompile(`["']([^"']+)["']`)
	removeVerbPattern        = regexp.MustCompile(`(?i)\b(?:please\s+)?(?:can\s+you\s+)?(?:could\s+you\s+)?(?:cancel|delete|remove|disable|stop|clear)\b`)
	removeObjectNoisePattern = regexp.MustCompile(`(?i)\b(?:that|this|the|my|a|an)\b|\b(?:reminders?|cron|jobs?|scheduled(?:\s+job|\s+task)?)\b`)
)

// JobExecutor is the interface for executing cron jobs through the agent
type JobExecutor interface {
	ProcessDirectWithChannel(ctx context.Context, content, sessionKey, channel, chatID string) (string, error)
}

// CronTool provides scheduling capabilities for the agent
type CronTool struct {
	cronService *cron.CronService
	executor    JobExecutor
	msgBus      *bus.MessageBus
	execTool    *ExecTool
	channel     string
	chatID      string
	mu          sync.RWMutex
}

// NewCronTool creates a new CronTool
// execTimeout: 0 means no timeout, >0 sets the timeout duration
func NewCronTool(
	cronService *cron.CronService, executor JobExecutor, msgBus *bus.MessageBus, workspace string, restrict bool,
	execTimeout time.Duration, config *config.Config,
) (*CronTool, error) {
	execTool, err := NewExecToolWithConfig(workspace, restrict, config)
	if err != nil {
		return nil, fmt.Errorf("unable to configure exec tool: %w", err)
	}

	execTool.SetTimeout(execTimeout)
	return &CronTool{
		cronService: cronService,
		executor:    executor,
		msgBus:      msgBus,
		execTool:    execTool,
	}, nil
}

// Name returns the tool name
func (t *CronTool) Name() string {
	return "cron"
}

// Description returns the tool description
func (t *CronTool) Description() string {
	return "Schedule reminders, tasks, or system commands. Use this for explicit or implied reminder intent (e.g. future obligations). For add: provide at_seconds/every_seconds/cron_expr, or natural_request when the user gives a clear natural phrase. For remove: prefer job_id, but query or natural_request can remove by semantic match; auto-remove only when exactly one match."
}

// Parameters returns the tool parameters schema
func (t *CronTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"add", "list", "remove", "enable", "disable"},
				"description": "Action to perform. Use 'add' for scheduling reminders/tasks, and 'remove' for cancellation.",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Reminder/task message shown when triggered. If omitted with natural_request, the tool derives it.",
			},
			"natural_request": map[string]any{
				"type":        "string",
				"description": "Optional natural-language request (e.g. 'remind me in 30 minutes to stretch', 'cancel that reminder').",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "Optional: Shell command to execute directly (e.g., 'df -h'). If set, the agent will run this command and report output instead of just showing the message. 'deliver' will be forced to false for commands.",
			},
			"at_seconds": map[string]any{
				"type":        "integer",
				"description": "One-time reminder: seconds from now when to trigger (e.g., 600 for 10 minutes later). Use this for one-time reminders like 'remind me in 10 minutes'.",
			},
			"every_seconds": map[string]any{
				"type":        "integer",
				"description": "Recurring interval in seconds (e.g., 3600 for every hour). Use this ONLY for recurring tasks like 'every 2 hours' or 'daily reminder'.",
			},
			"cron_expr": map[string]any{
				"type":        "string",
				"description": "Cron expression for complex recurring schedules (e.g., '0 9 * * *' for daily at 9am). Use this for complex recurring schedules.",
			},
			"job_id": map[string]any{
				"type":        "string",
				"description": "Job ID (preferred for remove/enable/disable).",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Optional semantic match text for remove when job_id is unknown.",
			},
			"deliver": map[string]any{
				"type":        "boolean",
				"description": "If true, send message directly to channel. If false, let agent process message (for complex tasks). Default: true",
			},
		},
		"required": []string{"action"},
	}
}

// SetContext sets the current session context for job creation
func (t *CronTool) SetContext(channel, chatID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.channel = channel
	t.chatID = chatID
}

// Execute runs the tool with the given arguments
func (t *CronTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	action, ok := args["action"].(string)
	if !ok {
		return ErrorResult("action is required")
	}

	switch action {
	case "add":
		return t.addJob(args)
	case "list":
		return t.listJobs()
	case "remove":
		return t.removeJob(args)
	case "enable":
		return t.enableJob(args, true)
	case "disable":
		return t.enableJob(args, false)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s", action))
	}
}

func (t *CronTool) addJob(args map[string]any) *ToolResult {
	t.mu.RLock()
	channel := t.channel
	chatID := t.chatID
	t.mu.RUnlock()

	if channel == "" || chatID == "" {
		return ErrorResult("no session context (channel/chat_id not set). Use this tool in an active conversation.")
	}

	message := strings.TrimSpace(stringArg(args, "message"))
	naturalRequest := strings.TrimSpace(stringArg(args, "natural_request"))

	schedule, hasSchedule, err := buildScheduleFromArgs(args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	if !hasSchedule {
		if naturalRequest == "" {
			return ErrorResult("one of at_seconds, every_seconds, cron_expr, or natural_request is required")
		}

		message, schedule, err = resolveNaturalAddRequest(naturalRequest, message)
		if err != nil {
			return ErrorResult(err.Error())
		}
	}

	if message == "" && naturalRequest != "" {
		message = resolveNaturalMessage(naturalRequest, "")
	}

	if message == "" {
		return ErrorResult("message is required for add")
	}

	// Read deliver parameter, default to true
	deliver := true
	if d, ok := args["deliver"].(bool); ok {
		deliver = d
	}

	command, _ := args["command"].(string)
	if command != "" {
		// Commands must be processed by agent/exec tool, so deliver must be false (or handled specifically)
		// Actually, let's keep deliver=false to let the system know it's not a simple chat message
		// But for our new logic in ExecuteJob, we can handle it regardless of deliver flag if Payload.Command is set.
		// However, logically, it's not "delivered" to chat directly as is.
		deliver = false
	}

	// Truncate message for job name (max 30 chars)
	messagePreview := utils.Truncate(message, 30)

	job, err := t.cronService.AddJob(
		messagePreview,
		schedule,
		message,
		deliver,
		channel,
		chatID,
	)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Error adding job: %v", err))
	}

	if command != "" {
		job.Payload.Command = command
		// Need to save the updated payload
		t.cronService.UpdateJob(job)
	}

	return SilentResult(fmt.Sprintf("Cron job added: %s (id: %s)", job.Name, job.ID))
}

func (t *CronTool) listJobs() *ToolResult {
	jobs := t.cronService.ListJobs(false)

	if len(jobs) == 0 {
		return SilentResult("No scheduled jobs")
	}

	var result strings.Builder
	result.WriteString("Scheduled jobs:\n")
	for _, j := range jobs {
		result.WriteString(fmt.Sprintf("- %s (id: %s, %s)\n", j.Name, j.ID, formatScheduleInfo(j.Schedule)))
	}

	return SilentResult(result.String())
}

func (t *CronTool) removeJob(args map[string]any) *ToolResult {
	jobID := strings.TrimSpace(stringArg(args, "job_id"))
	if jobID != "" {
		if t.cronService.RemoveJob(jobID) {
			return SilentResult(fmt.Sprintf("Cron job removed: %s", jobID))
		}
		return ErrorResult(fmt.Sprintf("Job %s not found", jobID))
	}

	jobs := t.listScopedJobs()
	if len(jobs) == 0 {
		return ErrorResult("No scheduled jobs found")
	}

	query := strings.TrimSpace(stringArg(args, "query"))
	naturalRequest := strings.TrimSpace(stringArg(args, "natural_request"))
	if query == "" && naturalRequest != "" {
		query = extractRemovalQuery(naturalRequest)
	}

	if query == "" {
		if len(jobs) == 1 {
			if t.cronService.RemoveJob(jobs[0].ID) {
				return SilentResult(fmt.Sprintf("Cron job removed: %s", jobs[0].ID))
			}
			return ErrorResult(fmt.Sprintf("Job %s not found", jobs[0].ID))
		}
		return ErrorResult(buildRemovalDisambiguationMessage("Multiple jobs found", jobs))
	}

	matches := filterJobsByQuery(jobs, query)
	if len(matches) == 0 {
		return ErrorResult(fmt.Sprintf("No scheduled job matched %q", query))
	}
	if len(matches) == 1 {
		if t.cronService.RemoveJob(matches[0].ID) {
			return SilentResult(fmt.Sprintf("Cron job removed: %s", matches[0].ID))
		}
		return ErrorResult(fmt.Sprintf("Job %s not found", matches[0].ID))
	}

	return ErrorResult(buildRemovalDisambiguationMessage(
		fmt.Sprintf("Multiple jobs matched %q", query),
		matches,
	))
}

func stringArg(args map[string]any, key string) string {
	value, ok := args[key]
	if !ok {
		return ""
	}

	s, ok := value.(string)
	if !ok {
		return ""
	}

	return s
}

func intArg(args map[string]any, key string) (int64, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}

	switch n := v.(type) {
	case float64:
		return int64(n), true
	case float32:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case json.Number:
		parsed, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func buildScheduleFromArgs(args map[string]any) (cron.CronSchedule, bool, error) {
	atSeconds, hasAt := intArg(args, "at_seconds")
	everySeconds, hasEvery := intArg(args, "every_seconds")
	cronExpr := strings.TrimSpace(stringArg(args, "cron_expr"))
	hasCron := cronExpr != ""

	switch {
	case hasAt:
		if atSeconds <= 0 {
			return cron.CronSchedule{}, true, fmt.Errorf("at_seconds must be greater than 0")
		}
		atMS := time.Now().UnixMilli() + atSeconds*1000
		return cron.CronSchedule{Kind: "at", AtMS: &atMS}, true, nil
	case hasEvery:
		if everySeconds <= 0 {
			return cron.CronSchedule{}, true, fmt.Errorf("every_seconds must be greater than 0")
		}
		everyMS := everySeconds * 1000
		return cron.CronSchedule{Kind: "every", EveryMS: &everyMS}, true, nil
	case hasCron:
		return cron.CronSchedule{Kind: "cron", Expr: cronExpr}, true, nil
	default:
		return cron.CronSchedule{}, false, nil
	}
}

func resolveNaturalAddRequest(request, fallbackMessage string) (string, cron.CronSchedule, error) {
	request = strings.TrimSpace(request)
	if request == "" {
		return "", cron.CronSchedule{}, fmt.Errorf("natural_request is empty")
	}

	lower := strings.ToLower(request)

	if matches := naturalEveryPattern.FindStringSubmatch(lower); len(matches) == 3 {
		amount, _ := strconv.ParseInt(matches[1], 10, 64)
		seconds := durationToSeconds(amount, matches[2])
		if seconds > 0 {
			everyMS := seconds * 1000
			message := resolveNaturalMessage(request, fallbackMessage)
			return message, cron.CronSchedule{Kind: "every", EveryMS: &everyMS}, nil
		}
	}

	if strings.Contains(lower, "every day") || strings.Contains(lower, "daily") {
		everyMS := int64(24 * 60 * 60 * 1000)
		message := resolveNaturalMessage(request, fallbackMessage)
		return message, cron.CronSchedule{Kind: "every", EveryMS: &everyMS}, nil
	}

	if strings.Contains(lower, "every week") || strings.Contains(lower, "weekly") {
		everyMS := int64(7 * 24 * 60 * 60 * 1000)
		message := resolveNaturalMessage(request, fallbackMessage)
		return message, cron.CronSchedule{Kind: "every", EveryMS: &everyMS}, nil
	}

	if strings.Contains(lower, "every hour") || strings.Contains(lower, "hourly") {
		everyMS := int64(60 * 60 * 1000)
		message := resolveNaturalMessage(request, fallbackMessage)
		return message, cron.CronSchedule{Kind: "every", EveryMS: &everyMS}, nil
	}

	if matches := naturalAtPattern.FindStringSubmatch(lower); len(matches) == 3 {
		amount, _ := strconv.ParseInt(matches[1], 10, 64)
		seconds := durationToSeconds(amount, matches[2])
		if seconds > 0 {
			atMS := time.Now().UnixMilli() + seconds*1000
			message := resolveNaturalMessage(request, fallbackMessage)
			return message, cron.CronSchedule{Kind: "at", AtMS: &atMS}, nil
		}
	}

	return "", cron.CronSchedule{}, fmt.Errorf(
		"couldn't infer schedule from natural_request; provide clearer time like 'in 10 minutes' or use at_seconds/every_seconds/cron_expr",
	)
}

func durationToSeconds(amount int64, unit string) int64 {
	if amount <= 0 {
		return 0
	}

	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "second", "seconds", "sec", "secs":
		return amount
	case "minute", "minutes", "min", "mins":
		return amount * 60
	case "hour", "hours", "hr", "hrs":
		return amount * 60 * 60
	case "day", "days":
		return amount * 24 * 60 * 60
	case "week", "weeks":
		return amount * 7 * 24 * 60 * 60
	default:
		return 0
	}
}

func resolveNaturalMessage(request, fallback string) string {
	if trimmed := strings.TrimSpace(fallback); trimmed != "" {
		return trimmed
	}

	message := request
	message = naturalEveryPattern.ReplaceAllString(message, "")
	message = naturalAtPattern.ReplaceAllString(message, "")
	message = naturalReminderLead.ReplaceAllString(message, "")

	for _, phrase := range []string{"every day", "daily", "every week", "weekly", "every hour", "hourly"} {
		message = regexp.MustCompile(`(?i)`+regexp.QuoteMeta(phrase)).ReplaceAllString(message, "")
	}

	message = strings.TrimSpace(message)
	message = strings.Trim(message, " ,.;:!?-")

	lower := strings.ToLower(message)
	if strings.HasPrefix(lower, "to ") {
		message = strings.TrimSpace(message[3:])
	}
	if strings.HasPrefix(strings.ToLower(message), "me ") {
		message = strings.TrimSpace(message[3:])
	}

	if message == "" {
		return "Reminder"
	}

	return message
}

func (t *CronTool) listScopedJobs() []cron.CronJob {
	jobs := t.cronService.ListJobs(true)

	t.mu.RLock()
	channel := t.channel
	chatID := t.chatID
	t.mu.RUnlock()

	if channel == "" || chatID == "" {
		return jobs
	}

	scoped := make([]cron.CronJob, 0, len(jobs))
	for _, job := range jobs {
		if job.Payload.Channel == "" || job.Payload.To == "" {
			scoped = append(scoped, job)
			continue
		}
		if job.Payload.Channel == channel && job.Payload.To == chatID {
			scoped = append(scoped, job)
		}
	}

	return scoped
}

func extractRemovalQuery(request string) string {
	request = strings.TrimSpace(request)
	if request == "" {
		return ""
	}

	if quoted := removeQuotedPattern.FindStringSubmatch(request); len(quoted) == 2 {
		return strings.TrimSpace(quoted[1])
	}

	cleaned := removeVerbPattern.ReplaceAllString(request, "")
	cleaned = removeObjectNoisePattern.ReplaceAllString(cleaned, "")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.Trim(cleaned, " ,.;:!?")

	return cleaned
}

func filterJobsByQuery(jobs []cron.CronJob, query string) []cron.CronJob {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil
	}

	for _, job := range jobs {
		if strings.EqualFold(job.ID, query) {
			return []cron.CronJob{job}
		}
	}

	tokens := tokenizeQuery(query)
	matches := make([]cron.CronJob, 0)
	for _, job := range jobs {
		haystack := strings.ToLower(strings.Join([]string{
			job.ID,
			job.Name,
			job.Payload.Message,
			formatScheduleInfo(job.Schedule),
		}, " "))

		if strings.Contains(haystack, query) {
			matches = append(matches, job)
			continue
		}

		if len(tokens) > 0 && containsAllTokens(haystack, tokens) {
			matches = append(matches, job)
		}
	}

	return matches
}

func tokenizeQuery(query string) []string {
	parts := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})

	stopWords := map[string]struct{}{
		"a": {}, "an": {}, "the": {}, "my": {}, "that": {}, "this": {},
		"reminder": {}, "job": {}, "cron": {}, "scheduled": {}, "task": {},
	}

	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) < 2 {
			continue
		}
		if _, stop := stopWords[part]; stop {
			continue
		}
		tokens = append(tokens, part)
	}

	return tokens
}

func containsAllTokens(text string, tokens []string) bool {
	for _, token := range tokens {
		if !strings.Contains(text, token) {
			return false
		}
	}
	return true
}

func buildRemovalDisambiguationMessage(prefix string, jobs []cron.CronJob) string {
	copyJobs := make([]cron.CronJob, len(jobs))
	copy(copyJobs, jobs)
	sort.Slice(copyJobs, func(i, j int) bool {
		return copyJobs[i].UpdatedAtMS > copyJobs[j].UpdatedAtMS
	})

	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(". Provide job_id or a more specific query.\n")
	b.WriteString("Matches:\n")

	limit := len(copyJobs)
	if limit > 5 {
		limit = 5
	}

	for i := 0; i < limit; i++ {
		job := copyJobs[i]
		b.WriteString(fmt.Sprintf("- id=%s, name=%q, schedule=%s\n", job.ID, job.Name, formatScheduleInfo(job.Schedule)))
	}

	if len(copyJobs) > limit {
		b.WriteString(fmt.Sprintf("...and %d more\n", len(copyJobs)-limit))
	}

	return strings.TrimSpace(b.String())
}

func formatScheduleInfo(schedule cron.CronSchedule) string {
	if schedule.Kind == "every" && schedule.EveryMS != nil {
		return fmt.Sprintf("every %ds", *schedule.EveryMS/1000)
	}
	if schedule.Kind == "cron" {
		return schedule.Expr
	}
	if schedule.Kind == "at" {
		return "one-time"
	}
	return "unknown"
}

func (t *CronTool) enableJob(args map[string]any, enable bool) *ToolResult {
	jobID, ok := args["job_id"].(string)
	if !ok || jobID == "" {
		return ErrorResult("job_id is required for enable/disable")
	}

	job := t.cronService.EnableJob(jobID, enable)
	if job == nil {
		return ErrorResult(fmt.Sprintf("Job %s not found", jobID))
	}

	status := "enabled"
	if !enable {
		status = "disabled"
	}
	return SilentResult(fmt.Sprintf("Cron job '%s' %s", job.Name, status))
}

// ExecuteJob executes a cron job through the agent
func (t *CronTool) ExecuteJob(ctx context.Context, job *cron.CronJob) string {
	// Get channel/chatID from job payload
	channel := job.Payload.Channel
	chatID := job.Payload.To

	// Default values if not set
	if channel == "" {
		channel = "cli"
	}
	if chatID == "" {
		chatID = "direct"
	}

	// Execute command if present
	if job.Payload.Command != "" {
		args := map[string]any{
			"command": job.Payload.Command,
		}

		result := t.execTool.Execute(ctx, args)
		var output string
		if result.IsError {
			output = fmt.Sprintf("Error executing scheduled command: %s", result.ForLLM)
		} else {
			output = fmt.Sprintf("Scheduled command '%s' executed:\n%s", job.Payload.Command, result.ForLLM)
		}

		pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pubCancel()
		t.msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
			Channel: channel,
			ChatID:  chatID,
			Content: output,
		})
		return "ok"
	}

	// If deliver=true, send message directly without agent processing
	if job.Payload.Deliver {
		pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pubCancel()
		t.msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
			Channel: channel,
			ChatID:  chatID,
			Content: job.Payload.Message,
		})
		return "ok"
	}

	// For deliver=false, process through agent (for complex tasks)
	sessionKey := fmt.Sprintf("cron-%s", job.ID)

	// Call agent with job's message
	response, err := t.executor.ProcessDirectWithChannel(
		ctx,
		job.Payload.Message,
		sessionKey,
		channel,
		chatID,
	)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	// Response is automatically sent via MessageBus by AgentLoop
	_ = response // Will be sent by AgentLoop
	return "ok"
}
