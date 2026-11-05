package settings_test

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assanoff/skit/dbx/pg"
	"github.com/assanoff/skit/settings"
	"github.com/assanoff/skit/worker"
)

// Limits are the settings of the examples, which an admin changes while the
// service runs.
type Limits struct {
	MaxWithdrawal int64 `json:"max_withdrawal"`
	Maintenance   bool  `json:"maintenance"`
}

// The limits are read at the start, with the defaults for what is not stored,
// and the group rereads them every 30s next to the HTTP server: a hot path
// reads them from memory, the admin saves new ones. The example needs
// Postgres, so go test builds it but does not run it.
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

	if err := settings.EnsureSchema(ctx, log, db); err != nil {
		log.ErrorContext(ctx, "settings table", "err", err)
		return
	}
	limits, err := settings.New(ctx, log, db, "limits", Limits{MaxWithdrawal: 100_000})
	if err != nil {
		log.ErrorContext(ctx, "limits", "err", err)
		return
	}

	mux := http.NewServeMux()
	// A hot path reads the limits from memory, without the database.
	mux.HandleFunc("POST /withdrawals", func(w http.ResponseWriter, _ *http.Request) {
		if limits.Current().Maintenance {
			http.Error(w, "withdrawals are paused", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	// The admin checks the new limits and saves them. The replica that saved
	// them uses them at once, the others after their next reread.
	mux.HandleFunc("PUT /admin/limits", func(w http.ResponseWriter, r *http.Request) {
		var v Limits
		if err := json.UnmarshalRead(r.Body, &v); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if v.MaxWithdrawal <= 0 {
			http.Error(w, "max_withdrawal must be positive", http.StatusUnprocessableEntity)
			return
		}
		if err := limits.Save(r.Context(), v); err != nil {
			http.Error(w, "the limits are not saved", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	g := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	g.Add(limits)
	g.Add(worker.RunnableFunc{
		NameValue: "http",
		StartFn:   func(context.Context) error { return srv.ListenAndServe() },
		StopFn:    srv.Shutdown,
	})
	if err := g.Run(ctx); err != nil {
		log.ErrorContext(ctx, "group stopped", "err", err)
	}
}
