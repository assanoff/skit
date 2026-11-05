package partition

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/internal/pgschema"
)

// Period is the range of time one partition holds.
type Period int

const (
	// Day is a partition per calendar day in UTC, such as messages_2026_11_05.
	Day Period = iota + 1
	// Month is a partition per calendar month in UTC, such as messages_2026_11.
	Month
)

// Config says which tables a Keeper maintains and how.
type Config struct {
	// Tables are partitioned by range of a time column, all by the same
	// Period: plain lowercase names, with a schema or without (messages,
	// audit.log). Their partitions are created in this order and dropped in
	// reverse, so a table that references another goes after it.
	Tables []string
	// Period is the range of one partition: Day or Month.
	Period Period
	// Ahead is how many periods after the current one have their partitions
	// (default 1).
	Ahead int
	// Retention is how long rows are kept: a partition is dropped once its
	// end is that old, so no row younger than Retention is. 0 keeps every
	// partition.
	Retention time.Duration
}

// Keeper creates the partitions of its tables ahead and drops the expired
// ones. It is safe for concurrent use: the runs of all Keepers of a database,
// on every replica, take their turns.
type Keeper struct {
	log *slog.Logger
	db  *dbx.DB
	cfg Config
	now func() time.Time
}

// Option configures a Keeper.
type Option func(*Keeper)

// WithNow sets the clock of the Keeper (tests); time.Now by default.
func WithNow(now func() time.Time) Option {
	return func(k *Keeper) {
		k.now = now
	}
}

// New builds a Keeper of the tables of cfg over db. It checks cfg and does not
// touch the database: the first Maintain creates the partitions. log may be
// nil.
func New(log *slog.Logger, db *dbx.DB, cfg Config, opts ...Option) (*Keeper, error) {
	cfg.Ahead = cmp.Or(cfg.Ahead, 1)
	if err := cfg.check(); err != nil {
		return nil, err
	}
	cfg.Tables = slices.Clone(cfg.Tables)
	k := &Keeper{
		log: cmp.Or(log, slog.Default()),
		db:  db,
		cfg: cfg,
		now: time.Now,
	}
	for _, opt := range opts {
		opt(k)
	}
	return k, nil
}

// lockKey is the advisory lock of a run, one for every Keeper of a database:
// the runs never change the same tables at once.
var lockKey = pgschema.Key("skit.partition")

// lockTimeout bounds how long a statement waits for the lock of a table, with
// the inserts into the table waiting behind it. On the timeout the run fails,
// and the next one does the work.
const lockTimeout = `SET LOCAL lock_timeout = '10s'`

// Maintain creates the missing partitions of the current period and of Ahead
// periods after it, and drops the partitions whose end is Retention old, in
// one transaction. A second run changes nothing. Maintain waits for the run
// of another Keeper of the database to end, and its own statements wait for
// the lock of a table no longer than 10s.
func (k *Keeper) Maintain(ctx context.Context) error {
	now := k.now()
	var create, drop []part
	err := dbx.WithinTran(ctx, k.log, k.db, func(tx *dbx.Tx) error {
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
			return fmt.Errorf("partition: take the lock: %w", err)
		}
		if err := dbx.Exec(ctx, tx, lockTimeout); err != nil {
			return fmt.Errorf("partition: set the lock timeout: %w", err)
		}
		existing, err := k.partitions(ctx, tx)
		if err != nil {
			return err
		}
		create, drop = k.plan(now, existing)
		return apply(ctx, tx, create, drop)
	})
	if err != nil {
		return err
	}
	for _, p := range create {
		k.log.InfoContext(ctx, "partition created", "partition", p.name)
	}
	for _, p := range drop {
		k.log.InfoContext(ctx, "partition dropped", "partition", p.name)
	}
	return nil
}

// part is a partition of a table: the name of the partition, in the schema of
// the table, and its range [from, to).
type part struct {
	table string
	name  string
	from  time.Time
	to    time.Time
}

// part returns the partition of the table for the period that starts at from.
func (k *Keeper) part(table string, from time.Time) part {
	return part{
		table: table,
		name:  table + "_" + from.Format(k.cfg.Period.layout()),
		from:  from,
		to:    k.cfg.Period.add(from, 1),
	}
}

// parse reads the partition of the table from the name of its relation, such
// as messages_2026_11; false when the name is not of a partition of the
// period.
func (k *Keeper) parse(table, relname string) (part, bool) {
	suffix, ok := strings.CutPrefix(relname, baseName(table)+"_")
	if !ok {
		return part{}, false
	}
	from, err := time.Parse(k.cfg.Period.layout(), suffix)
	if err != nil {
		return part{}, false
	}
	return k.part(table, from), true
}

// selectPartitions returns the names of the partitions of a table.
const selectPartitions = `SELECT c.relname
FROM pg_inherits i
	JOIN pg_class c ON c.oid = i.inhrelid
WHERE i.inhparent = to_regclass(:table)
ORDER BY c.relname`

// partitions returns the partitions the tables have, table by table in the
// order of their periods. A partition of another name is not among them.
func (k *Keeper) partitions(ctx context.Context, tx *dbx.Tx) ([]part, error) {
	var existing []part
	for _, table := range k.cfg.Tables {
		arg := map[string]any{"table": table}
		names, err := dbx.NamedQueryList[string](ctx, tx, selectPartitions, arg)
		if err != nil {
			return nil, fmt.Errorf("partition: read the partitions of %s: %w", table, err)
		}
		for _, name := range names {
			if p, ok := k.parse(table, name); ok {
				existing = append(existing, p)
			}
		}
	}
	return existing, nil
}

// plan returns the partitions to create and to drop at now. The current
// period and Ahead after it get the partitions they miss, table by table in
// the order of the tables; the partitions whose end is no later than now -
// Retention go in the reverse order.
func (k *Keeper) plan(now time.Time, existing []part) (create, drop []part) {
	have := make(map[string]bool, len(existing))
	for _, p := range existing {
		have[p.name] = true
	}
	current := k.cfg.Period.start(now)
	for i := range k.cfg.Ahead + 1 {
		for _, table := range k.cfg.Tables {
			if p := k.part(table, k.cfg.Period.add(current, i)); !have[p.name] {
				create = append(create, p)
			}
		}
	}
	if k.cfg.Retention == 0 {
		return create, nil
	}
	cutoff := now.Add(-k.cfg.Retention)
	for _, table := range slices.Backward(k.cfg.Tables) {
		for _, p := range existing {
			if p.table == table && !p.to.After(cutoff) {
				drop = append(drop, p)
			}
		}
	}
	return create, drop
}

// bound is a bound of a partition in its DDL; columns of timestamptz,
// timestamp and date all take it.
const bound = "2006-01-02 15:04:05-07"

// apply creates the partitions create, then detaches and drops the partitions
// drop. DDL takes no parameters: New has checked the names of the tables, and
// the bounds are times formatted here.
func apply(ctx context.Context, tx *dbx.Tx, create, drop []part) error {
	for _, p := range create {
		ddl := fmt.Sprintf("CREATE TABLE %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')",
			p.name, p.table, p.from.Format(bound), p.to.Format(bound))
		if err := dbx.Exec(ctx, tx, ddl); err != nil {
			return fmt.Errorf("partition: create %s: %w", p.name, err)
		}
	}
	for _, p := range drop {
		if err := dbx.Exec(ctx, tx, "ALTER TABLE "+p.table+" DETACH PARTITION "+p.name); err != nil {
			return fmt.Errorf("partition: detach %s: %w", p.name, err)
		}
		if err := dbx.Exec(ctx, tx, "DROP TABLE "+p.name); err != nil {
			return fmt.Errorf("partition: drop %s: %w", p.name, err)
		}
	}
	return nil
}

// layout is the time in the name of a partition of the period.
func (p Period) layout() string {
	if p == Day {
		return "2006_01_02"
	}
	return "2006_01"
}

// start returns the start of the period that holds t, in UTC.
func (p Period) start(t time.Time) time.Time {
	t = t.UTC()
	if p == Day {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// add returns the start of the period n periods after the one that starts at
// start. The start comes first: 31 October plus a month by AddDate would be
// 1 December.
func (p Period) add(start time.Time, n int) time.Time {
	if p == Day {
		return start.AddDate(0, 0, n)
	}
	return start.AddDate(0, n, 0)
}

// tableName is a plain lowercase name of a table, with a schema or without.
var tableName = regexp.MustCompile(`^([a-z_][a-z0-9_]*\.)?[a-z_][a-z0-9_]*$`)

// maxName is the longest name of a relation Postgres keeps (NAMEDATALEN - 1).
const maxName = 63

// check reports the first setting of cfg a Keeper cannot work with.
func (cfg Config) check() error {
	if cfg.Period != Day && cfg.Period != Month {
		return fmt.Errorf("partition: unknown period %d", cfg.Period)
	}
	if cfg.Ahead < 0 || cfg.Retention < 0 {
		return fmt.Errorf("partition: negative ahead %d or retention %s", cfg.Ahead, cfg.Retention)
	}
	if len(cfg.Tables) == 0 {
		return errors.New("partition: no tables")
	}
	seen := make(map[string]bool, len(cfg.Tables))
	for _, table := range cfg.Tables {
		if !tableName.MatchString(table) {
			return fmt.Errorf("partition: table %q is not a plain lowercase name", table)
		}
		if len(baseName(table))+len("_")+len(cfg.Period.layout()) > maxName {
			return fmt.Errorf("partition: the partitions of %s get names longer than %d bytes", table, maxName)
		}
		if seen[table] {
			return fmt.Errorf("partition: table %s is listed twice", table)
		}
		seen[table] = true
	}
	return nil
}

// baseName returns the name of the table without its schema.
func baseName(table string) string {
	if _, name, ok := strings.Cut(table, "."); ok {
		return name
	}
	return table
}
