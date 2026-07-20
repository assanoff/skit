package logger

import (
	"context"
	"runtime/debug"
	"strconv"
	"strings"
)

// BuildInfo logs the build metadata embedded in the Go binary (VCS revision,
// dirty flag, build settings, Go and module versions) at info level. Call it
// once at startup so every process records exactly what is running. When the
// info is unavailable (e.g. `go run` without VCS stamping) it logs a warning
// instead.
func (l *Logger) BuildInfo(ctx context.Context) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		l.Warn(ctx, "build info not available")
		return
	}

	var values []any
	for _, s := range info.Settings {
		key := s.Key
		if quoteKey(key) {
			key = strconv.Quote(key)
		}

		value := s.Value
		if quoteValue(value) {
			value = strconv.Quote(value)
		}

		values = append(values, key, value)
	}

	values = append(values, "goversion", info.GoVersion, "modversion", info.Main.Version)

	l.Info(ctx, "build info", values...)
}

// quoteKey reports whether key must be quoted to be a valid log key.
func quoteKey(key string) bool {
	return key == "" || strings.ContainsAny(key, "= \t\r\n\"`")
}

// quoteValue reports whether value must be quoted.
func quoteValue(value string) bool {
	return strings.ContainsAny(value, " \t\r\n\"`")
}
