package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type maintenanceJSONStatus string

const (
	maintenanceJSONStatusEmpty         maintenanceJSONStatus = "empty"
	maintenanceJSONStatusInvalidJSON   maintenanceJSONStatus = "invalid_json"
	maintenanceJSONStatusTruncatedJSON maintenanceJSONStatus = "truncated_json"
	maintenanceJSONStatusProviderError maintenanceJSONStatus = "provider_error"
	maintenanceJSONStatusTimeout       maintenanceJSONStatus = "timeout"
)

type maintenanceJSONError struct {
	status  maintenanceJSONStatus
	message string
	preview string
	err     error
}

func (e *maintenanceJSONError) Error() string {
	if e == nil {
		return ""
	}
	if e.message != "" {
		return e.message
	}
	return string(e.status)
}

func (e *maintenanceJSONError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func newMaintenanceJSONProviderError(message string, err error) error {
	status := maintenanceJSONStatusProviderError
	if errors.Is(err, context.DeadlineExceeded) {
		status = maintenanceJSONStatusTimeout
	}
	return &maintenanceJSONError{
		status:  status,
		message: message,
		err:     err,
	}
}

func decodeMaintenanceJSON(content string, target any) error {
	raw := extractJSONObjectFromContent(content)
	if strings.TrimSpace(raw) == "" {
		trimmed := strings.TrimSpace(content)
		status := maintenanceJSONStatusInvalidJSON
		message := "no JSON object in response"
		if trimmed == "" {
			status = maintenanceJSONStatusEmpty
			message = "empty JSON response"
		} else if looksLikeTruncatedJSONObject(trimmed) {
			status = maintenanceJSONStatusTruncatedJSON
			message = "truncated JSON response"
		}
		return &maintenanceJSONError{
			status:  status,
			message: message,
			preview: maintenancePreviewForLog(trimmed, 160),
		}
	}

	if err := json.Unmarshal([]byte(raw), target); err != nil {
		status := maintenanceJSONStatusInvalidJSON
		if isTruncatedJSONError(err) || looksLikeTruncatedJSONObject(raw) {
			status = maintenanceJSONStatusTruncatedJSON
		}
		return &maintenanceJSONError{
			status:  status,
			message: err.Error(),
			preview: maintenancePreviewForLog(raw, 160),
			err:     err,
		}
	}

	return nil
}

func maintenanceJSONStatusOf(err error) maintenanceJSONStatus {
	var jsonErr *maintenanceJSONError
	if errors.As(err, &jsonErr) && jsonErr != nil {
		return jsonErr.status
	}
	if err == nil {
		return ""
	}
	return maintenanceJSONStatusProviderError
}

func maintenanceJSONStatusString(err error) string {
	if status := maintenanceJSONStatusOf(err); status != "" {
		return string(status)
	}
	return ""
}

func maintenanceJSONPreview(err error) string {
	var jsonErr *maintenanceJSONError
	if errors.As(err, &jsonErr) && jsonErr != nil {
		return jsonErr.preview
	}
	return ""
}

func maintenancePreviewForLog(value string, maxRunes int) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	trimmed = redactSensitiveValuesForLog(trimmed)
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

var maintenanceURLPattern = regexp.MustCompile(`https?://[^\s\])}>,]+`)
var maintenanceAuthBearerPattern = regexp.MustCompile(`(?i)\bauthorization\b\s*[:=]\s*bearer\s+[^"',\s\])}]+`)
var maintenanceBearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
var maintenanceSecretPattern = regexp.MustCompile(`(?i)\b(api[-_ ]?key|authorization|token|secret|password)\b\s*[:=]\s*["']?[^"',\s\])}]+`)

func redactSensitiveValuesForLog(value string) string {
	value = maintenanceURLPattern.ReplaceAllString(value, "<url>")
	value = maintenanceAuthBearerPattern.ReplaceAllString(value, "authorization=<redacted>")
	value = maintenanceBearerPattern.ReplaceAllString(value, "bearer <redacted>")
	return maintenanceSecretPattern.ReplaceAllString(value, "$1=<redacted>")
}

func looksLikeTruncatedJSONObject(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "{") && !strings.HasSuffix(trimmed, "}") {
		return true
	}

	depth := 0
	inString := false
	escaped := false
	seenOpen := false
	for _, r := range trimmed {
		if escaped {
			escaped = false
			continue
		}
		if inString {
			switch r {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			depth++
			seenOpen = true
		case '}':
			if depth > 0 {
				depth--
			}
		}
	}
	return seenOpen && (depth > 0 || inString)
}

func isTruncatedJSONError(err error) bool {
	if err == nil {
		return false
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return strings.Contains(strings.ToLower(syntaxErr.Error()), "unexpected end")
	}
	return strings.Contains(strings.ToLower(fmt.Sprint(err)), "unexpected end")
}
