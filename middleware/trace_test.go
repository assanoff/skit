package middleware_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/matryer/is"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/middleware"
	"github.com/assanoff/skit/otel/setup"
)

// incoming is a traceparent from a caller's trace, unrelated to any span the
// test starts.
const incoming = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

// hex32 matches a W3C trace id.
var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

// traced serves pattern on a mux, with Trace after routing as Install puts it.
func traced(cfg middleware.TraceConfig, pattern string, h http.HandlerFunc) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(pattern, middleware.Trace(cfg)(h))
	return mux
}

func TestTraceOpensServerSpan(t *testing.T) {
	is := is.New(t)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	var inHandler trace.SpanContext
	h := traced(middleware.TraceConfig{TracerProvider: tp}, "GET /widgets/{id}",
		func(_ http.ResponseWriter, r *http.Request) {
			inHandler = trace.SpanContextFromContext(r.Context())
		})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/widgets/42", nil)
	req.Header.Set("Traceparent", incoming)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	spans := rec.Ended()
	is.Equal(len(spans), 1)
	span := spans[0]
	is.Equal(span.Name(), "GET /widgets/{id}")
	is.Equal(span.SpanKind(), trace.SpanKindServer)
	is.Equal(span.SpanContext().TraceID().String(), "0af7651916cd43dd8448eb211c80319c")
	is.Equal(span.Parent().SpanID().String(), "b7ad6b7169203331") // continues the caller
	is.Equal(inHandler.SpanID(), span.SpanContext().SpanID())     // the handler runs in it
	sc := span.SpanContext()
	is.Equal(w.Header().Get("Traceparent"), fmt.Sprintf("00-%s-%s-01", sc.TraceID(), sc.SpanID()))
}

// A public endpoint starts a new trace and links the caller's.
func TestTracePublicStartsNewTrace(t *testing.T) {
	is := is.New(t)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	cfg := middleware.TraceConfig{
		TracerProvider: tp,
		Public:         func(*http.Request) bool { return true },
	}
	h := traced(cfg, "GET /x", func(http.ResponseWriter, *http.Request) {})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
	req.Header.Set("Traceparent", incoming)
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := rec.Ended()
	is.Equal(len(spans), 1)
	span := spans[0]
	is.True(span.SpanContext().TraceID().String() != "0af7651916cd43dd8448eb211c80319c")
	is.Equal(len(span.Links()), 1)
	is.Equal(span.Links()[0].SpanContext.TraceID().String(), "0af7651916cd43dd8448eb211c80319c")
}

// With tracing not exported, log lines still carry a W3C trace id with trace
// flags 00, and RequestID falls back to it.
func TestTraceLogsTraceIDWithoutExport(t *testing.T) {
	is := is.New(t)
	tel, err := setup.New(t.Context(), slog.New(slog.DiscardHandler), setup.Config{})
	is.NoErr(err)
	defer func() { _ = tel.Shutdown() }()
	var buf bytes.Buffer
	log := logger.New(&buf, logger.Config{Level: logger.LevelInfo})
	cfg := middleware.TraceConfig{TracerProvider: tel.TracerProvider()}
	h := traced(cfg, "GET /x", func(w http.ResponseWriter, r *http.Request) {
		middleware.RequestID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			log.InfoContext(r.Context(), "handled")
		})).ServeHTTP(w, r)
	})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))

	var line map[string]any
	is.NoErr(json.Unmarshal(buf.Bytes(), &line))
	traceID, _ := line["trace_id"].(string)
	spanID, ok := line["span_id"].(string)
	is.True(ok)
	is.True(hex32.MatchString(traceID))
	is.Equal(line["trace_flags"], "00")
	is.Equal(w.Header().Get(middleware.RequestIDHeader), traceID)
	is.Equal(w.Header().Get("Traceparent"), fmt.Sprintf("00-%s-%s-00", traceID, spanID))
}

// A handler that routes by itself below a prefix names the span after its own
// route: otelhttp would put the prefix back after the handler otherwise.
func TestSetRouteNamesSpanUnderPrefix(t *testing.T) {
	is := is.New(t)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	cfg := middleware.TraceConfig{TracerProvider: tp}
	h := traced(cfg, "/v1/", func(_ http.ResponseWriter, r *http.Request) {
		middleware.SetRoute(r, "/v1/widgets/{id}")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/widgets/42", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := rec.Ended()
	is.Equal(len(spans), 1)
	is.Equal(spans[0].Name(), "GET /v1/widgets/{id}")
	is.Equal(attr(spans[0], semconv.HTTPRouteKey), "/v1/widgets/{id}")
}

// A method the HTTP semantic conventions do not know is named HTTP, as
// otelhttp names it.
func TestTraceNamesUnknownMethod(t *testing.T) {
	is := is.New(t)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	cfg := middleware.TraceConfig{TracerProvider: tp}
	h := traced(cfg, "/x", func(http.ResponseWriter, *http.Request) {})

	req := httptest.NewRequestWithContext(t.Context(), "PURGE", "/x", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := rec.Ended()
	is.Equal(len(spans), 1)
	is.Equal(spans[0].Name(), "HTTP /x")
}

// attr returns the string value of the span attribute key, or "".
func attr(s sdktrace.ReadOnlySpan, key attribute.Key) string {
	for _, kv := range s.Attributes() {
		if kv.Key == key {
			return kv.Value.AsString()
		}
	}
	return ""
}
