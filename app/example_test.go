package app_test

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/assanoff/skit/app"
	"github.com/assanoff/skit/closer"
	"github.com/assanoff/skit/dim"
	"github.com/assanoff/skit/worker"
)

// Deps is the dependency container of a service: a lazy provider for each
// dependency, filled by the initializers.
type Deps struct {
	Store  dim.Provider[*conn]
	Mailer dim.Provider[*conn]
}

type conn struct{ name string }

// connect is the factory of a resource: it runs on the first use of the
// provider, not when the initializer wires it.
func connect(name string) func(context.Context) (*conn, dim.CleanupFunc, error) {
	return func(context.Context) (*conn, dim.CleanupFunc, error) {
		fmt.Println("connect", name)
		return &conn{name: name}, nil, nil
	}
}

func initStore(d *Deps) (dim.CleanupFunc, error) {
	var cleanup dim.CleanupFunc
	d.Store, cleanup = dim.NewResource("store", connect("store"))
	return cleanup, nil
}

func initMailer(d *Deps) (dim.CleanupFunc, error) {
	var cleanup dim.CleanupFunc
	d.Mailer, cleanup = dim.NewResource("mailer", connect("mailer"))
	return cleanup, nil
}

// The serve command: build the container, then let Run supervise the
// Runnables until a signal and release the resources after.
func Example() {
	ctx := context.Background()
	log := slog.Default()

	d := &Deps{}
	if err := app.InitDeps(d, []app.Initializer[Deps]{initStore, initMailer}); err != nil {
		log.ErrorContext(ctx, "init", "err", err)
		return
	}
	digest := worker.NewLoop(log, worker.LoopConfig{Name: "digest", Interval: time.Hour},
		func(ctx context.Context) error {
			log.InfoContext(ctx, "digest sent", "via", d.Mailer(ctx).name)
			return nil
		})

	err := app.Run(ctx, app.RunConfig{Logger: log, ShutdownTimeout: 10 * time.Second}, digest)
	if err != nil {
		log.ErrorContext(ctx, "run", "err", err)
	}
}

// InitDeps only wires the providers: a resource connects on its first use, so
// the mailer nobody uses never connects.
func ExampleInitDeps() {
	d := &Deps{}
	if err := app.InitDeps(d, []app.Initializer[Deps]{initStore, initMailer}); err != nil {
		fmt.Println("init:", err)
		return
	}
	// The cleanups are on the global closer: release them in LIFO order.
	defer func() { _ = closer.CloseSync() }()

	fmt.Println("initialized")
	fmt.Println("query", d.Store(context.Background()).name)
	// Output:
	// initialized
	// connect store
	// query store
}

// A one-shot command passes only the initializers it needs; RunCommand
// releases what they built when the command returns.
func ExampleRunCommand() {
	err := app.RunCommand(context.Background(), app.CommandConfig{}, &Deps{},
		[]app.Initializer[Deps]{initStore},
		func(ctx context.Context, d *Deps) error {
			fmt.Println("vacuum", d.Store(ctx).name)
			return nil
		})
	fmt.Println("command:", err)
	// Output:
	// connect store
	// vacuum store
	// command: <nil>
}
