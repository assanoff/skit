package settings

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/internal/pgschema"
	"github.com/assanoff/skit/poller"
)

// defaultInterval is how often the settings are reread by default.
const defaultInterval = 30 * time.Second

// Settings are one document of settings of type T: stored in the table
// settings under their name, kept in memory and reread on an interval. They
// are a worker.Runnable: the group that runs them runs the rereads.
type Settings[T any] struct {
	db       *dbx.DB
	name     string
	defaults []byte // the defaults as JSON, decoded afresh by each read
	poller   *poller.Poller[T]
}

// Option configures Settings.
type Option func(*options)

type options struct {
	interval time.Duration
}

// WithInterval sets how often the settings are reread (default 30s): the
// longest a change saved on another replica takes to show here.
func WithInterval(d time.Duration) Option {
	return func(o *options) {
		o.interval = d
	}
}

// New reads the settings stored under name and keeps them. The defaults stand
// for the fields the stored document lacks, and for all of it before the first
// Save. A failed read fails New: the service does not start on the defaults in
// place of the settings it was given. log may be nil.
func New[T any](ctx context.Context, log *slog.Logger, db *dbx.DB, name string, defaults T, opts ...Option) (*Settings[T], error) {
	if name == "" {
		return nil, errors.New("settings: no name")
	}
	base, err := json.Marshal(defaults)
	if err != nil {
		return nil, fmt.Errorf("settings %s: encode the defaults: %w", name, err)
	}
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	s := &Settings[T]{db: db, name: name, defaults: base}
	v, err := s.read(ctx)
	if err != nil {
		return nil, err
	}
	interval := cmp.Or(o.interval, defaultInterval)
	s.poller = poller.New(log, v, s.read, poller.Config{
		Name:        "settings-" + name,
		Interval:    interval,
		PollTimeout: interval,
	})
	return s, nil
}

// Current returns the settings as last read. It never waits for the database
// and is safe for concurrent use.
func (s *Settings[T]) Current() T { return s.poller.Current() }

// Save stores v under the name of the settings, and this replica rereads them
// at once; the others see v on their next read. Of two concurrent Saves, the
// last one wins.
func (s *Settings[T]) Save(ctx context.Context, v T) error {
	value, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("settings %s: encode: %w", s.name, err)
	}
	const q = `
		INSERT INTO settings (name, value, updated_at) VALUES (:name, :value, now())
		ON CONFLICT (name) DO UPDATE
		SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`
	arg := map[string]any{"name": s.name, "value": value}
	if err := dbx.NamedExec(ctx, s.db, q, arg); err != nil {
		return fmt.Errorf("settings %s: save: %w", s.name, err)
	}
	s.poller.Poll(ctx)
	return nil
}

// Name names the rereads in the group and in the logs: settings-<name>.
func (s *Settings[T]) Name() string { return s.poller.Name() }

// Start rereads the settings at once and then every interval until ctx is
// canceled; it implements worker.Runnable. A failed read is logged and keeps
// the settings as they were.
func (s *Settings[T]) Start(ctx context.Context) error {
	return s.poller.Start(ctx)
}

// Stop implements worker.Runnable; Start returns when ctx is canceled.
func (s *Settings[T]) Stop(ctx context.Context) error {
	return s.poller.Stop(ctx)
}

// read returns the stored settings decoded over the defaults, or the defaults
// when none are stored.
func (s *Settings[T]) read(ctx context.Context) (T, error) {
	var v T
	if err := json.Unmarshal(s.defaults, &v); err != nil {
		return v, fmt.Errorf("settings %s: decode the defaults: %w", s.name, err)
	}
	const q = `SELECT value FROM settings WHERE name = :name`
	arg := map[string]any{"name": s.name}
	stored, err := dbx.NamedQueryOne[[]byte](ctx, s.db, q, arg)
	if errors.Is(err, dbx.ErrDBNotFound) {
		return v, nil
	}
	if err != nil {
		return v, fmt.Errorf("settings %s: read: %w", s.name, err)
	}
	if err := json.Unmarshal(stored, &v); err != nil {
		return v, fmt.Errorf("settings %s: decode: %w", s.name, err)
	}
	return v, nil
}

// Schema returns the DDL of the table settings, for a service that keeps the
// table in its own migrations.
func Schema() string {
	return `
CREATE TABLE IF NOT EXISTS settings (
    name       TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);`
}

// EnsureSchema creates the table settings if it is absent. Several replicas
// may run it at their start at once: they take their turns on an advisory
// lock. log may be nil.
func EnsureSchema(ctx context.Context, log *slog.Logger, db *dbx.DB) error {
	if err := pgschema.Ensure(ctx, log, db, "settings", Schema()); err != nil {
		return fmt.Errorf("settings: create the table: %w", err)
	}
	return nil
}
