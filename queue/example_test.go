package queue_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/pg"
	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/queue"
	"github.com/assanoff/skit/worker"
)

type welcomeEmail struct {
	UserID int64 `json:"user_id"`
}

type passwordReset struct {
	UserID int64 `json:"user_id"`
}

// A producer schedules a task on the pool, and the workers of its queue run
// it on every replica. The example needs Postgres, so go test builds it but
// does not run it.
func Example() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.Default()

	db, err := pg.Open(pg.Config{Host: "localhost:5432", Name: "shop", DisableTLS: true})
	if err != nil {
		log.ErrorContext(ctx, "open", "err", err)
		return
	}
	defer func() { _ = db.Close() }()
	q := queue.NewPG(log, db)
	if err := q.EnsureSchema(ctx); err != nil {
		log.ErrorContext(ctx, "schema", "err", err)
		return
	}

	// Producer: the Key makes the enqueue idempotent within the queue.
	payload, err := json.Marshal(welcomeEmail{UserID: 42})
	if err != nil {
		log.ErrorContext(ctx, "encode", "err", err)
		return
	}
	_, err = q.Schedule(ctx, queue.Params{
		Queue: "email", Kind: "email.welcome", Key: "welcome:42", Payload: payload,
	})
	if err != nil {
		log.ErrorContext(ctx, "schedule", "err", err)
		return
	}

	// Consumer: the workers of the queue claim its tasks and run the Mux.
	mux := queue.NewMux()
	err = mux.Register("email.welcome", queue.Decode(
		func(ctx context.Context, _ queue.Task, m welcomeEmail) error {
			log.InfoContext(ctx, "welcome sent", "user_id", m.UserID)
			return nil
		}))
	if err != nil {
		log.ErrorContext(ctx, "register", "err", err)
		return
	}
	cfg := queue.Config{Queue: "email", Batch: 10, Timeout: 10 * time.Second}
	workers, err := queue.Workers(log, q, cfg, mux.Handle, queue.WithMetrics(metrics.New()))
	if err != nil {
		log.ErrorContext(ctx, "workers", "err", err)
		return
	}

	g := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	g.Add(workers...)
	if err := g.Run(ctx); err != nil {
		log.ErrorContext(ctx, "group stopped", "err", err)
	}
}

// Bind schedules a task in the transaction of the domain change, routed by the
// Go type of the value: the task commits or rolls back with the change.
func ExampleBind() {
	ctx := context.Background()
	log := slog.Default()
	db, err := pg.Open(pg.Config{Host: "localhost:5432", Name: "shop", DisableTLS: true})
	if err != nil {
		log.ErrorContext(ctx, "open", "err", err)
		return
	}
	defer func() { _ = db.Close() }()
	q := queue.NewPG(log, db)

	reg := queue.NewRegistry()
	if err := reg.Register[welcomeEmail]("email", "email.welcome"); err != nil {
		log.ErrorContext(ctx, "register", "err", err)
		return
	}

	err = dbx.WithinTran(ctx, log, db, func(tx *dbx.Tx) error {
		const ins = `INSERT INTO users (id, email) VALUES (:id, :email)`
		err := dbx.NamedExec(ctx, tx, ins, map[string]any{"id": 42, "email": "ann@example.com"})
		if err != nil {
			return err
		}
		return queue.Bind(tx, q, reg).Schedule(ctx, welcomeEmail{UserID: 42})
	})
	if err != nil {
		log.ErrorContext(ctx, "sign up", "err", err)
	}
}

// A Mux routes each task of a queue to the handler of its kind. A kind with no
// handler fails for good: the task goes to the dead letters.
func ExampleMux() {
	mux := queue.NewMux()
	err := mux.Register("email.welcome", func(_ context.Context, t queue.Task) error {
		fmt.Println("send", t.Kind, t.ID)
		return nil
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	ctx := context.Background()
	fmt.Println(mux.Handle(ctx, queue.Task{ID: 1, Kind: "email.welcome"}))
	err = mux.Handle(ctx, queue.Task{ID: 2, Kind: "email.digest"})
	fmt.Println(err)
	fmt.Println(errors.Is(err, queue.ErrUnknownKind))
	// Output:
	// send email.welcome 1
	// <nil>
	// queue: no handler registered for kind: "email.digest"
	// true
}

// Decode hands the handler the payload as a T. A payload that does not decode
// never will, so its error is Permanent.
func ExampleDecode() {
	h := queue.Decode(func(_ context.Context, t queue.Task, m welcomeEmail) error {
		fmt.Println("welcome", m.UserID)
		return nil
	})

	ctx := context.Background()
	fmt.Println(h(ctx, queue.Task{Kind: "email.welcome", Payload: []byte(`{"user_id":42}`)}))
	fmt.Println(h(ctx, queue.Task{Kind: "email.welcome", Payload: []byte(`{"user_id":"42"}`)}))
	// Output:
	// welcome 42
	// <nil>
	// queue: decode email.welcome: json: cannot unmarshal JSON string into Go int64 within "/user_id"
}

// Check, at startup, finds the kinds the Registry sends to a queue with no
// handler in its Mux; a route turned off with Drop sends nothing.
func ExampleRegistry_Check() {
	reg := queue.NewRegistry()
	err := errors.Join(
		reg.Register[welcomeEmail]("email", "email.welcome"),
		reg.Register[passwordReset]("email", "email.reset"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	mux := queue.NewMux()
	err = mux.Register("email.welcome", queue.Decode(
		func(context.Context, queue.Task, welcomeEmail) error { return nil }))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(reg.Check("email", mux))

	// The reset emails are off in this environment: Schedule drops them.
	reg = queue.NewRegistry()
	err = errors.Join(
		reg.Register[welcomeEmail]("email", "email.welcome"),
		reg.Register[passwordReset]("email", "email.reset", queue.Drop()),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(reg.Check("email", mux))
	// Output:
	// queue: check email: queue: no handler registered for kind: ["email.reset"]
	// <nil>
}
