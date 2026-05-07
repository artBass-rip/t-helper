package jobs

import (
	"regexp"
	"strings"
)

var secretLikeMessagePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|private[_ -]?key|authorization|bearer)\s*[:=]\s*[^,\s]+`),
	regexp.MustCompile(`(?i)(https?|ssh)://[^@\s]+@`),
	regexp.MustCompile(`(?i)secretref://[^\s,]+`),
}

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
