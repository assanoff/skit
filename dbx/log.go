package dbx

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"time"
)

// caller returns the program counter of the function that called the exported
// dbx helper (frames: runtime.Callers, caller, the helper). Call it directly in
// the helper's body, so a failed query is logged at the store method that ran it.
func caller() uintptr {
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	return pcs[0]
}

// logQuery logs a failed statement with the expanded query and returns err
// unchanged; see logFailure.
func logQuery(ctx context.Context, log *slog.Logger, pc uintptr, msg, query string, data any, err error) error {
	if !loggable(ctx, log, err) {
		return err
	}
	logFailure(ctx, log, pc, msg, err, slog.String("query", queryString(query, data)))
	return err
}

// loggable reports whether err is a failure worth logging: successful
// statements and ErrDBNotFound — an ordinary "no rows" outcome — are not.
func loggable(ctx context.Context, log *slog.Logger, err error) bool {
	return err != nil && log != nil && !errors.Is(err, ErrDBNotFound) &&
		log.Enabled(ctx, slog.LevelInfo)
}

// logFailure writes a failed statement at info level with source pc, the
// caller's program counter (see caller). slog has no frame-skip parameter, so
// the record is built by hand as in the log/slog "Wrapping output methods"
// example.
func logFailure(ctx context.Context, log *slog.Logger, pc uintptr, msg string, err error, attrs ...slog.Attr) {
	r := slog.NewRecord(time.Now(), slog.LevelInfo, msg, pc)
	r.AddAttrs(attrs...)
	r.AddAttrs(slog.Any("err", err))
	_ = log.Handler().Handle(ctx, r)
}
