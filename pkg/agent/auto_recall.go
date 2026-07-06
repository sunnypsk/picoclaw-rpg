package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
)

const (
	autoRecallKeywordExtractorPromptTag   = "AUTO_RECALL_KEYWORD_EXTRACTOR_V1"
	autoRecallKeywordExtractionTimeout    = 1 * time.Minute
	autoRecallKeywordExtractionMaxTokens  = 128
	autoRecallKeywordExtractionMaxResults = 8
)

type autoRecallKeywordResponse struct {
	Keywords []string `json:"keywords"`
}

func (al *AgentLoop) buildAutoRecallHints(
	ctx context.Context,
	agent *AgentInstance,
	userMessage string,
) string {
	if al == nil || al.cfg == nil || agent == nil || agent.MemoryIndex == nil {
		return ""
	}

	autoCfg := al.cfg.Agents.Defaults.MemorySearch.AutoRecall
	if !autoCfg.Enabled {
		return ""
	}

	message := strings.TrimSpace(userMessage)
	if message == "" || strings.HasPrefix(message, "/") {
		return ""
	}

	keywordQuery := al.extractAutoRecallQuery(ctx, agent, message)
	if keywordQuery == "" {
		return ""
	}

	results, err := agent.MemoryIndex.Search(ctx, keywordQuery, autoCfg.EffectiveTopK(), "")
	if err != nil {
		logger.DebugCF("agent", "Auto memory recall search failed",
			map[string]any{"agent_id": agent.ID, "error": err.Error(), "keyword_query": keywordQuery})
		return ""
	}
	if len(results) == 0 {
		return ""
	}

	maxChars := autoCfg.EffectiveMaxChars()
	var sb strings.Builder
	sb.WriteString("RELEVANT_MEMORY (keyword recall)\n")

	for i, r := range results {
		snippet := strings.TrimSpace(r.Snippet)
		if snippet == "" {
			continue
		}
		entry := fmt.Sprintf("%d. %s\n%s\n", i+1, r.Path, snippet)
		if sb.Len()+len(entry) > maxChars {
			break
		}
		sb.WriteString(entry)
	}

	if sb.Len() == 0 {
		return ""
	}

	return strings.TrimSpace(sb.String())
}

func (al *AgentLoop) extractAutoRecallQuery(ctx context.Context, agent *AgentInstance, userMessage string) string {
	status := "skipped"
	keywords := []string{}
	keywordQuery := ""
	logErr := ""
	logPreview := ""
	fallbackReason := ""
	start := time.Now()

	defer func() {
		fields := map[string]any{
			"agent_id":      "",
			"status":        status,
			"query_preview": autoRecallPreviewForLog(userMessage, 120),
			"query_len":     len([]rune(strings.TrimSpace(userMessage))),
			"duration_ms":   time.Since(start).Milliseconds(),
		}
		if agent != nil {
			fields["agent_id"] = agent.ID
		}
		if len(keywords) > 0 {
			fields["keywords_count"] = len(keywords)
			fields["keywords_preview"] = autoRecallPreviewForLog(strings.Join(keywords, " | "), 160)
			fields["keyword_query_preview"] = autoRecallPreviewForLog(keywordQuery, 120)
		}
		if logErr != "" {
			fields["error"] = logErr
		}
		if logPreview != "" {
			fields["response_preview"] = logPreview
		}
		if fallbackReason != "" {
			fields["fallback_reason"] = fallbackReason
		}
		logger.InfoCF("memory", "Auto recall keyword extraction completed", fields)
	}()

	if agent == nil || agent.Provider == nil {
		return ""
	}

	useFallback := func(reason string) string {
		fallbackReason = reason
		keywords = fallbackAutoRecallKeywords(userMessage)
		if len(keywords) == 0 {
			return ""
		}
		keywordQuery = strings.Join(keywords, " ")
		status = "fallback"
		return keywordQuery
	}

	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	extractCtx, cancel := context.WithTimeout(baseCtx, autoRecallKeywordExtractionTimeout)
	defer cancel()

	systemPrompt := fmt.Sprintf(`%s
You extract search keywords for local memory recall.
Return JSON only, no markdown, no explanations.

Output shape:
{"keywords":["string"]}

Goal:
- Extract short search terms that could help retrieve relevant memory, even when the user speaks casually, indirectly, or in a different language from the stored memory.

Rules:
- Return 1 to %d concise keywords or short phrases when the message contains any retrievable subject.
- Treat recall-style questions as retrievable even when they are short or conversational.
- Prefer names, nicknames, identity clues, places, dates, plans, reminders, preferences, projects, products, travel destinations, commitments, failures, reasons, and prior-discussion topics.
- Keep the user's original language when possible.
- Add likely English aliases or translations when they could help cross-language retrieval.
- Mixed-language keywords are allowed.
- Exclude greetings, filler, and pure sentiment with no retrievable subject.
- Return {"keywords":[]} only when there is truly no retrievable subject.
- Return valid JSON object only.

Examples:
- User: 記唔記得我個名
  Output: {"keywords":["個名","名字","name","called"]}
- User: 你講下今次失敗嘅原因
  Output: {"keywords":["失敗","原因","failure","reason"]}
- User: 飛鼠仔好關心你北海道嘅行程
  Output: {"keywords":["北海道","行程","Hokkaido","trip itinerary"]}`, autoRecallKeywordExtractorPromptTag, autoRecallKeywordExtractionMaxResults)

	response, err := agent.Provider.Chat(
		extractCtx,
		[]providers.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
		},
		nil,
		agent.Model,
		map[string]any{
			"max_tokens":       autoRecallKeywordExtractionMaxTokens,
			"temperature":      0.1,
			"prompt_cache_key": agent.ID + ":auto-recall-keywords",
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded), errors.Is(extractCtx.Err(), context.DeadlineExceeded):
			status = "timeout"
		default:
			status = "provider_error"
		}
		logErr = maintenancePreviewForLog(err.Error(), 160)
		return useFallback(status)
	}
	if response == nil {
		status = "provider_error"
		logErr = "nil keyword extraction response"
		return useFallback(status)
	}

	var payload autoRecallKeywordResponse
	if err := decodeMaintenanceJSON(response.Content, &payload); err != nil {
		status = maintenanceJSONStatusString(err)
		logErr = err.Error()
		logPreview = maintenanceJSONPreview(err)
		return useFallback(status)
	}

	keywords = normalizeAutoRecallKeywords(payload.Keywords)
	if len(keywords) == 0 {
		status = "empty"
		return ""
	}

	keywordQuery = strings.Join(keywords, " ")
	status = "hit"
	return keywordQuery
}

func fallbackAutoRecallKeywords(userMessage string) []string {
	source := fallbackAutoRecallSource(userMessage)
	if source == "" {
		return nil
	}

	candidates := make([]string, 0, autoRecallKeywordExtractionMaxResults)
	addCandidate := func(value string) {
		value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		if value == "" {
			return
		}
		for _, existing := range candidates {
			if strings.EqualFold(existing, value) {
				return
			}
		}
		candidates = append(candidates, value)
	}

	if len([]rune(source)) <= 80 {
		addCandidate(source)
	}

	tokens := fallbackAutoRecallTokens(source)
	for _, token := range tokens {
		addCandidate(token)
		if len(candidates) >= autoRecallKeywordExtractionMaxResults {
			return candidates
		}
	}
	for i := 0; i+1 < len(tokens); i++ {
		addCandidate(tokens[i] + " " + tokens[i+1])
		if len(candidates) >= autoRecallKeywordExtractionMaxResults {
			return candidates
		}
	}

	return candidates
}

func fallbackAutoRecallSource(userMessage string) string {
	message := strings.Join(strings.Fields(strings.TrimSpace(userMessage)), " ")
	if message == "" {
		return ""
	}
	if strings.HasPrefix(message, "# Proactive Outreach Check") || strings.HasPrefix(message, "# Heartbeat Check") {
		return fallbackPromptField(message, "Latest user turn preview:")
	}

	runes := []rune(message)
	if len(runes) > 320 {
		return string(runes[:320])
	}
	return message
}

func fallbackPromptField(message, label string) string {
	idx := strings.Index(message, label)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(message[idx+len(label):])
	if rest == "" {
		return ""
	}
	end := len(rest)
	for _, marker := range []string{" Latest assistant turn preview:", " Relationship snapshot:", " Rules:"} {
		if markerIdx := strings.Index(rest, marker); markerIdx >= 0 && markerIdx < end {
			end = markerIdx
		}
	}
	value := strings.TrimSpace(rest[:end])
	if value == "" || strings.EqualFold(value, "none") {
		return ""
	}
	return value
}

func fallbackAutoRecallTokens(source string) []string {
	rawTokens := strings.FieldsFunc(source, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	tokens := make([]string, 0, len(rawTokens))
	for _, token := range rawTokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if isAutoRecallFallbackStopWord(token) {
			continue
		}
		if len([]rune(token)) < 2 {
			continue
		}
		tokens = append(tokens, token)
		if len(tokens) >= autoRecallKeywordExtractionMaxResults {
			break
		}
	}
	return tokens
}

func isAutoRecallFallbackStopWord(token string) bool {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "the", "and", "for", "with", "that", "this", "what", "when", "where", "which", "who", "why",
		"how", "are", "was", "were", "is", "my", "your", "you", "me", "please", "about", "有冇", "係咪":
		return true
	default:
		return false
	}
}

func normalizeAutoRecallKeywords(keywords []string) []string {
	seen := make(map[string]struct{})
	normalized := make([]string, 0, min(len(keywords), autoRecallKeywordExtractionMaxResults))
	for _, keyword := range keywords {
		keyword = strings.Join(strings.Fields(strings.TrimSpace(keyword)), " ")
		if keyword == "" {
			continue
		}

		key := strings.ToLower(keyword)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, keyword)
		if len(normalized) >= autoRecallKeywordExtractionMaxResults {
			break
		}
	}
	return normalized
}

func autoRecallPreviewForLog(value string, maxRunes int) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if maxRunes <= 0 {
		return ""
	}

	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func injectAutoRecallHints(messages []providers.Message, hints string) []providers.Message {
	hints = strings.TrimSpace(hints)
	if hints == "" || len(messages) == 0 || messages[0].Role != "system" {
		return messages
	}

	hintBlock := hints
	messages[0].Content = messages[0].Content + "\n\n---\n\n" + hintBlock
	messages[0].SystemParts = append(messages[0].SystemParts, providers.ContentBlock{
		Type: "text",
		Text: hintBlock,
	})

	return messages
}
