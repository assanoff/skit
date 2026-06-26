package errs

import (
	"regexp"
	"unicode/utf8"
)

// MaxMessageLen bounds the length of a sanitized message to keep logs and API
// responses tidy.
const MaxMessageLen = 1024

var (
	// secretKVRE matches key=value pairs such as password=hunter2 or api_key=abc.
	secretKVRE = regexp.MustCompile(`(?i)\b(password|passwd|pwd|token|secret|authorization|api[-_]?key)=\S+`)
	// secretJSONRE matches JSON fields such as "token":"abc".
	secretJSONRE = regexp.MustCompile(`(?i)"(password|passwd|pwd|token|secret|authorization|api[-_]?key)"\s*:\s*"[^"]*"`)
)

// Sanitize redacts common secret patterns and truncates the message to
// MaxMessageLen on a UTF-8 boundary. It is safe to call on any string.
func Sanitize(msg string) string {
	msg = secretKVRE.ReplaceAllString(msg, "$1=***")
	msg = secretJSONRE.ReplaceAllString(msg, `"$1":"***"`)
	if len(msg) > MaxMessageLen {
		msg = truncateUTF8(msg, MaxMessageLen)
	}
	return msg
}

// truncateUTF8 truncates s to at most maxLen bytes without splitting a rune.
func truncateUTF8(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	for maxLen > 0 && !utf8.RuneStart(s[maxLen]) {
		maxLen--
	}
	return s[:maxLen]
}
