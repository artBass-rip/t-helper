package jobs

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var secretLikeMessagePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|private[_ -]?key|authorization|bearer)\s*[:=]\s*[^,\s]+`),
	regexp.MustCompile(`(?i)(https?|ssh)://[^@\s]+@`),
	regexp.MustCompile(`(?i)secretref://[^\s,]+`),
}

var secretLikeJSONKeyPattern = regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|private[_ -]?key|authorization|bearer)`)

func safeMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	for _, pattern := range secretLikeMessagePatterns {
		message = pattern.ReplaceAllStringFunc(message, func(value string) string {
			if strings.Contains(value, "://") {
				parts := strings.SplitN(value, "://", 2)
				return parts[0] + "://[redacted]@"
			}
			if idx := strings.IndexAny(value, ":="); idx >= 0 {
				return strings.TrimSpace(value[:idx]) + value[idx:idx+1] + "[redacted]"
			}
			return "[redacted]"
		})
	}
	if len(message) > 500 {
		message = message[:500] + "..."
	}
	return message
}

func safeJSONPayload(payload json.RawMessage) json.RawMessage {
	if len(payload) == 0 {
		return payload
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return payload
	}
	sanitized := sanitizeJSONValue(value, "")
	out, err := json.Marshal(sanitized)
	if err != nil {
		return payload
	}
	return out
}

func validateSafeJobPayload(payload json.RawMessage) error {
	if len(payload) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return err
	}
	return validateSafeJSONValue(value, "")
}

func validateSafeJSONValue(value any, key string) error {
	if secretLikeJSONKeyPattern.MatchString(key) {
		return fmt.Errorf("job payload contains unsafe secret-like key %q", key)
	}
	switch typed := value.(type) {
	case map[string]any:
		for childKey, childValue := range typed {
			if err := validateSafeJSONValue(childValue, childKey); err != nil {
				return err
			}
		}
	case []any:
		for _, childValue := range typed {
			if err := validateSafeJSONValue(childValue, key); err != nil {
				return err
			}
		}
	case string:
		if containsSecretLikeString(typed) {
			return fmt.Errorf("job payload contains unsafe secret-like string")
		}
	}
	return nil
}

func containsSecretLikeString(value string) bool {
	for _, pattern := range secretLikeMessagePatterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func sanitizeJSONValue(value any, key string) any {
	if secretLikeJSONKeyPattern.MatchString(key) {
		return "[redacted]"
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			out[childKey] = sanitizeJSONValue(childValue, childKey)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, childValue := range typed {
			out[i] = sanitizeJSONValue(childValue, key)
		}
		return out
	case string:
		return safeMessage(typed)
	default:
		return typed
	}
}
