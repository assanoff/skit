package httpclient_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/matryer/is"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/httpclient"
	"github.com/assanoff/skit/httpmw"
	"github.com/assanoff/skit/retry"
)

// Every attempt of a retried call is a client span of the caller's trace with
// its own traceparent; retries carry their resend count, and url.full hides
// the values of signature keys.
func TestClientSpanPerAttempt(t *testing.T) {
	is := is.New(t)
	var (
		mu      sync.Mutex
		parents []string
	)
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		parents = append(parents, r.Header.Get("Traceparent"))
		if len(parents) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	c, err := httpclient.New(httpclient.Config{
		Retry: &httpmw.RetryConfig{
			Backoff: retry.Backoff{Base: time.Millisecond, MaxAttempts: 3},
		},
		Base:           srv.Client().Transport,
		TracerProvider: tp,
	})
	is.NoErr(err)
	ctx, caller := tp.Tracer("test").Start(t.Context(), "caller")

	url := srv.URL + "/x?sig=abc&x=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	is.NoErr(err)
	resp, err := c.Do(req)
	is.NoErr(err)
	_ = resp.Body.Close()
	caller.End()

	var attempts []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.SpanKind() == trace.SpanKindClient {
			attempts = append(attempts, s)
		}
	}
	is.Equal(len(attempts), 3)
	for i, s := range attempts {
		sc := s.SpanContext()
		is.Equal(s.Parent().SpanID(), caller.SpanContext().SpanID())
		is.Equal(parents[i], fmt.Sprintf("00-%s-%s-01", sc.TraceID(), sc.SpanID()))
		is.Equal(attr(s, semconv.HTTPRequestResendCountKey).AsInt64(), int64(i)) // 0: unset
		is.True(strings.HasSuffix(attr(s, semconv.URLFullKey).AsString(), "/x?sig=REDACTED&x=1"))
	}
}

// attr returns the value of the span attribute key; the zero Value when unset.
func attr(s sdktrace.ReadOnlySpan, key attribute.Key) attribute.Value {
	for _, kv := range s.Attributes() {
		if kv.Key == key {
			return kv.Value
		}
	}
	return attribute.Value{}
}
