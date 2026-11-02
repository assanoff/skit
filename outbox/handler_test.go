package outbox_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/matryer/is"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/otel"
	"github.com/assanoff/skit/outbox"
	"github.com/assanoff/skit/queue"
	"github.com/assanoff/skit/worker"
)

type created struct {
	ID string `json:"id"`
}

// marshalRaw encodes every event as the same bytes, not valid UTF-8.
func marshalRaw(any) ([]byte, error) { return []byte{0xff, 'R'}, nil }

// recorder records the messages it is asked to publish and fails each with
// the error of its type.
type recorder struct {
	errs map[string]error
	sent []broker.Message
}

func (r *recorder) Publish(_ context.Context, m broker.Message) error {
	r.sent = append(r.sent, m)
	return r.errs[m.Type]
}

// types returns the types of the messages sent.
func (r *recorder) types() []string {
	out := make([]string, len(r.sent))
	for i := range r.sent {
		out[i] = r.sent[i].Type
	}
	return out
}

// The message of an event task carries its envelope, the kind of the task as
// its type and the topic of the handler; an envelope that does not decode is
// not published.
func TestHandler(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	pub := &recorder{}
	handle := outbox.Handler(pub, "widgets")

	payload, err := outbox.JSON(ctx, created{ID: "42"})
	is.NoErr(err)
	is.NoErr(handle(ctx, queue.Task{Kind: "widget.created", Payload: payload}))
	raw, err := outbox.Envelope("application/x-raw", marshalRaw)(ctx, created{})
	is.NoErr(err)
	is.NoErr(handle(ctx, queue.Task{Kind: "widget.raw", Payload: raw}))
	is.True(handle(ctx, queue.Task{Kind: "widget.bad", Payload: []byte("{")}) != nil)

	is.Equal(len(pub.sent), 2) // the bad envelope is not published
	m := pub.sent[0]
	is.True(m.ID != "")
	is.True(m.ID != pub.sent[1].ID)           // every event gets an id of its own
	is.Equal(m.Type, "widget.created")        // the kind of the task
	is.Equal(m.Topic, "widgets")              // the topic of the handler
	is.True(time.Since(m.Time) < time.Minute) // the time of the schedule
	is.Equal(m.ContentType, "application/json")
	is.Equal(string(m.Data), `{"id":"42"}`)
	is.Equal(len(m.Headers), 0) // no trace context to carry
	is.Equal(pub.sent[1].ContentType, "application/x-raw")
	is.Equal(pub.sent[1].Data, []byte{0xff, 'R'}) // any bytes survive the envelope
}

// tracedPublisher records every send as the broker adapters do: a span under
// the creation context in the headers of the message.
type tracedPublisher struct{ tracer trace.Tracer }

func (p tracedPublisher) Publish(ctx context.Context, m broker.Message) error {
	_, span := p.tracer.Start(otel.ExtractFromCarrier(ctx, m.Headers), "send "+m.Topic)
	span.End()
	return nil
}

// The handler sends an event in the trace it was created in: the send is a
// child of the span that scheduled the event.
func TestHandlerSendsInCreationTrace(t *testing.T) {
	is := is.New(t)
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, request := tp.Tracer("api").Start(t.Context(), "request")
	payload, err := outbox.JSON(ctx, created{ID: "42"})
	is.NoErr(err)
	request.End()
	handle := outbox.Handler(tracedPublisher{tracer: tp.Tracer("broker")}, "widgets")
	is.NoErr(handle(t.Context(), queue.Task{Kind: "widget.created", Payload: payload}))

	spans := exp.GetSpans()
	is.Equal(len(spans), 2)
	is.Equal(spans[0].Name, "request")
	is.Equal(spans[1].Name, "send widgets")
	is.Equal(spans[1].Parent.SpanID(), spans[0].SpanContext.SpanID())
	is.Equal(spans[1].SpanContext.TraceID(), spans[0].SpanContext.TraceID())
}

// taskRow is the state of a task in queue_tasks.
type taskRow struct {
	Kind     string `db:"kind"`
	Attempts int    `db:"attempts"`
	Dead     bool   `db:"dead"`
	Leased   bool   `db:"leased"`
}

// On a queue of events an envelope that does not decode and a permanent error
// dead-letter their task, and the batch goes on; the first other error retries
// its task and stops the batch, whose rest goes back untouched.
func TestHandlerOutcomes(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := t.Context()
	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	q := queue.NewPG(slog.New(slog.DiscardHandler), pg.DB)
	is.NoErr(q.EnsureSchema(ctx))
	env, err := outbox.JSON(ctx, created{ID: "1"})
	is.NoErr(err)
	for _, p := range []queue.Params{
		{Queue: "events", Kind: "garbled", Payload: []byte("{")},
		{Queue: "events", Kind: "bad", Payload: env},
		{Queue: "events", Kind: "down", Payload: env},
		{Queue: "events", Kind: "rest", Payload: env},
	} {
		_, err := q.Schedule(ctx, p)
		is.NoErr(err)
	}

	pub := &recorder{errs: map[string]error{
		"bad":  broker.Permanent(errors.New("too large")),
		"down": errors.New("refused"),
	}}
	cfg := outbox.Config("events")
	cfg.Poll = time.Hour // one claim
	rs, err := queue.Workers(nil, q, cfg, outbox.Handler(pub, "widgets"))
	is.NoErr(err)
	g := worker.NewGroup(nil, worker.GroupConfig{ShutdownTimeout: time.Second})
	for _, r := range rs {
		g.Add(r)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- g.Run(runCtx) }()

	const query = `
SELECT kind, attempts, done_at IS NOT NULL AS dead, lease_id IS NOT NULL AS leased
FROM queue_tasks ORDER BY id`
	want := []taskRow{
		{Kind: "garbled", Attempts: 1, Dead: true},
		{Kind: "bad", Attempts: 1, Dead: true},
		{Kind: "down", Attempts: 1},
		{Kind: "rest"}, // released with no attempt counted
	}
	var rows []taskRow
	for range 100 {
		rows, err = dbx.QueryList[taskRow](ctx, pg.DB, query)
		is.NoErr(err)
		if rows[2].Attempts == 1 && !rows[3].Leased {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	is.Equal(rows, want)
	is.Equal(pub.types(), []string{"bad", "down"}) // neither garbled nor rest
}
