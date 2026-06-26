package logger

import (
	"runtime"
	"strconv"
	"time"
)

var zeroTime time.Time

func now() time.Time { return time.Now() }

// pc returns the program counter of the caller skip frames up, so slog can
// resolve the correct source location.
func pc(skip int) uintptr {
	var pcs [1]uintptr
	runtime.Callers(skip+1, pcs[:])
	return pcs[0]
}

func itoa(n int) string { return strconv.Itoa(n) }
