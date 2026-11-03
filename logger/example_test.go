package logger_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/buildinfo"
	"github.com/assanoff/skit/logger"
)

// noTime drops the time of a record, so the output of an example is stable.
func noTime(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}

// Every record carries the identity of the service and, when the context has
// a span, the ids of its trace; a child logger adds its own attributes.
func Example() {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: noTime})
	log := logger.NewWithHandler(h, logger.Config{Service: "orders", Version: "v1.2.3"})

	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	ctx := trace.ContextWithSpanContext(context.Background(),
		trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
		}))

	log.InfoContext(ctx, "order created", "order_id", 42)
	log.With("logger", "access").InfoContext(context.Background(), "request", "path", "/")
	// Output:
	// {"level":"INFO","msg":"order created","service.name":"orders","service.version":"v1.2.3","order_id":42,"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7","trace_flags":"01"}
	// {"level":"INFO","msg":"request","service.name":"orders","service.version":"v1.2.3","logger":"access","path":"/"}
}

// A service builds one logger at startup, named after its build, and injects
// it everywhere.
func ExampleNew() {
	b := buildinfo.Read()
	log := logger.New(os.Stdout, logger.Config{
		Service:   "orders",
		Version:   b.Version,
		Level:     logger.LevelInfo,
		AddSource: true,
	})
	log.InfoContext(context.Background(), "starting service", "build", b)
}

// An Events hook runs in addition to the sink, just before the write, such as
// an alert on every error; the record carries the identity of the service and
// the attributes. The hook sees only the levels the sink is enabled for.
func ExampleEvents() {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: noTime})
	log := logger.NewWithHandler(h, logger.Config{
		Service: "orders",
		Events: logger.Events{
			Error: func(_ context.Context, r slog.Record) {
				fmt.Print("alert: ", r.Message)
				r.Attrs(func(a slog.Attr) bool {
					fmt.Print(" ", a)
					return true
				})
				fmt.Println()
			},
		},
	})

	ctx := context.Background()
	log.InfoContext(ctx, "order created")
	log.ErrorContext(ctx, "payment failed", "err", errors.New("card declined"))
	// Output:
	// {"level":"INFO","msg":"order created","service.name":"orders"}
	// alert: payment failed service.name=orders err=card declined
	// {"level":"ERROR","msg":"payment failed","service.name":"orders","err":"card declined"}
}

// PanicArgs logs a recovered panic under err, with the stack of the goroutine
// as exception.stacktrace.
func ExamplePanicArgs() {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == "exception.stacktrace" {
				a.Value = slog.StringValue("...")
			}
			return noTime(groups, a)
		},
	})
	log := logger.NewWithHandler(h, logger.Config{Service: "orders"})

	func() {
		defer func() {
			if p := recover(); p != nil {
				log.ErrorContext(context.Background(), "panic recovered",
					logger.PanicArgs(p, "path", "/orders")...)
			}
		}()
		panic("nil map")
	}()
	// Output:
	// {"level":"ERROR","msg":"panic recovered","service.name":"orders","err":"panic: nil map","exception.stacktrace":"...","path":"/orders"}
}
