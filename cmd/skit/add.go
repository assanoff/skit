package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// addCommand is the parent group for `skit add <kind>`; the work is done by
// its subcommands (rest, ...).
type addCommand struct{}

// addRestCommand scaffolds a REST CRUD module (core + Postgres store + transport)
// for one entity into the current service, following the skit conventions.
type addRestCommand struct {
	Dir     string `long:"dir" default:"." description:"service root containing go.mod (default: current directory)"`
	Module  string `long:"module" description:"module path (default: read from go.mod)"`
	Plural  string `long:"plural" description:"route/table plural (default: <name>+\"s\")"`
	NoTests bool   `long:"no-tests" description:"skip generating tests (add them later with 'skit add rest-test')"`
	Args    struct {
		Name string `positional-arg-name:"name" description:"entity name, e.g. widget or order-line"`
	} `positional-args:"yes" required:"yes"`
}

func (c *addRestCommand) Execute([]string) error {
	return addREST(os.Stdout, addRESTOpts{
		Dir:     c.Dir,
		Module:  c.Module,
		Plural:  c.Plural,
		Name:    c.Args.Name,
		NoTests: c.NoTests,
	})
}

// addRestTestCommand scaffolds tests for an existing REST module: fast API tests
// over a mocked Store (moq) plus an integration suite (testcontainers) that
// drives the real application handler.
type addRestTestCommand struct {
	Dir    string `long:"dir" default:"." description:"service root containing go.mod (default: current directory)"`
	Module string `long:"module" description:"module path (default: read from go.mod)"`
	Plural string `long:"plural" description:"route/table plural (default: <name>+\"s\")"`
	Args   struct {
		Name string `positional-arg-name:"name" description:"entity name, e.g. widget or order-line"`
	} `positional-args:"yes" required:"yes"`
}

func (c *addRestTestCommand) Execute([]string) error {
	return addRESTTest(os.Stdout, addRESTOpts{
		Dir:    c.Dir,
		Module: c.Module,
		Plural: c.Plural,
		Name:   c.Args.Name,
	})
}

// addMigrationCommand scaffolds the next numbered goose migration into
// internal/migrations. The sequence number is derived by scanning existing
// NNNN_*.sql files, so migrations stay ordered and never collide.
type addMigrationCommand struct {
	Dir  string `long:"dir" default:"." description:"service root containing internal/migrations (default: current directory)"`
	Args struct {
		Name string `positional-arg-name:"name" description:"migration name, e.g. add-photos or create-adverts"`
	} `positional-args:"yes" required:"yes"`
}

func (c *addMigrationCommand) Execute([]string) error {
	return addMigration(os.Stdout, c.Dir, c.Args.Name)
}

// migrationsSubdir is where the full scaffold keeps goose SQL migrations.
const migrationsSubdir = "internal/migrations"

// seqRE matches the leading zero-padded sequence on a migration filename, e.g.
// "0007_add_photos.sql" -> "0007".
var seqRE = regexp.MustCompile(`^(\d+)_`)

// addMigration writes internal/migrations/NNNN_<slug>.sql, where NNNN is one
// past the highest existing sequence (0001 when none exist) and <slug> is name
// in lower_snake_case. The migrations directory is created if absent.
func addMigration(out io.Writer, dir, name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("invalid name %q: must start with a letter and contain only letters, digits, '-' or '_'", name)
	}
	if dir == "" {
		dir = "."
	}

	migDir := filepath.Join(dir, filepath.FromSlash(migrationsSubdir))
	seq, err := nextMigrationSeq(migDir)
	if err != nil {
		return err
	}

	slug := strings.ToLower(strings.Join(splitWords(name), "_"))
	dest := filepath.Join(migDir, fmt.Sprintf("%04d_%s.sql", seq, slug))

	if err := writeIfAbsent(out, dest, "templates/migration/migration.sql.tmpl", struct{ Name string }{Name: slug}); err != nil {
		return err
	}

	fmt.Fprintf(out, `
Scaffolded migration %s. Next:

1. Edit the file — replace the SELECT 1 placeholders with your Up/Down SQL.
2. Apply it with your migrate command (e.g. go run ./cmd/<svc> migrate).
`, dest)
	return nil
}

// nextMigrationSeq returns one past the highest NNNN_ prefix among the .sql
// files in dir, or 1 when dir is absent or holds no numbered migrations.
func nextMigrationSeq(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, err
	}
	max := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		m := seqRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max + 1, nil
}

type addRESTOpts struct {
	Dir       string // service root (holds go.mod)
	Module    string // module path; resolved from go.mod when empty
	Plural    string // override for routes/table; defaults to Pkg+"s"
	Name      string // raw entity name
	NoTests   bool   // skip generating tests
	Claim     bool   // worker: --claim (queue-backed) variant instead of periodic tick
	Broker    string // consumer: transport for the generated wiring (rmq|kafka|nats)
	WithRelay bool   // event: also scaffold the service-wide outbox relay bootstrap
}

// restData is the template payload shared by every generated REST file.
type restData struct {
	Module     string // github.com/you/svc
	Pkg        string // widget      — package name (lower, no separators)
	Type       string // Widget      — exported Go type
	Recv       string // w           — short local/var name (first letter of Pkg)
	Plural     string // widgets     — route path + table name
	UpperSnake string // ORDER_LINE  — env-namespace fragment (consumer config)
}

var nameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// addREST generates the core, store and REST transport for one entity.
func addREST(out io.Writer, opts addRESTOpts) error {
	if !nameRE.MatchString(opts.Name) {
		return fmt.Errorf("invalid name %q: must start with a letter and contain only letters, digits, '-' or '_'", opts.Name)
	}

	dir := opts.Dir
	if dir == "" {
		dir = "."
	}

	module := opts.Module
	if module == "" {
		m, err := moduleFromGoMod(dir)
		if err != nil {
			return err
		}
		module = m
	}

	words := splitWords(opts.Name)
	pkg := strings.ToLower(strings.Join(words, ""))
	plural := opts.Plural
	if plural == "" {
		plural = pkg + "s"
	}
	data := restData{
		Module: module,
		Pkg:    pkg,
		Type:   pascal(words),
		Recv:   pkg[:1],
		Plural: plural,
	}

	// dest path -> embedded template. Writing is idempotent: existing files are
	// skipped (never overwritten), so re-running fills in only what is missing.
	corePkgDir := filepath.Join(dir, "core", pkg)
	dbPkgDir := filepath.Join(corePkgDir, pkg+"db")
	apiPkgDir := filepath.Join(dir, "api", pkg)
	files := []struct{ dest, tmpl string }{
		{filepath.Join(corePkgDir, pkg+".go"), "templates/rest/core.go.tmpl"},
		{filepath.Join(corePkgDir, "model.go"), "templates/rest/core_model.go.tmpl"},
		{filepath.Join(corePkgDir, "filter.go"), "templates/rest/core_filter.go.tmpl"},
		{filepath.Join(corePkgDir, "order.go"), "templates/rest/core_order.go.tmpl"},
		{filepath.Join(dbPkgDir, pkg+"db.go"), "templates/rest/db.go.tmpl"},
		{filepath.Join(dbPkgDir, "model.go"), "templates/rest/db_model.go.tmpl"},
		{filepath.Join(dbPkgDir, "order.go"), "templates/rest/db_order.go.tmpl"},
		{filepath.Join(dbPkgDir, "filter.go"), "templates/rest/db_filter.go.tmpl"},
		{filepath.Join(apiPkgDir, pkg+".go"), "templates/rest/api.go.tmpl"},
		{filepath.Join(apiPkgDir, "model.go"), "templates/rest/api_model.go.tmpl"},
		{filepath.Join(apiPkgDir, "order.go"), "templates/rest/api_order.go.tmpl"},
		{filepath.Join(apiPkgDir, "filter.go"), "templates/rest/api_filter.go.tmpl"},
		// Declares the mocks package so its import resolves before the first
		// `make generate`; moq writes StoreMock alongside this file.
		{filepath.Join(corePkgDir, "mocks", "doc.go"), "templates/rest/mocks_doc.go.tmpl"},
	}

	for _, f := range files {
		if err := writeIfAbsent(out, f.dest, f.tmpl, data); err != nil {
			return err
		}
	}

	// Tests are generated alongside the module (API + integration) unless opted
	// out; --no-tests leaves them for a later `skit add rest-test`.
	if !opts.NoTests {
		if err := generateRESTTests(out, dir, data); err != nil {
			return err
		}
	}

	// Detect the full bootstrap (internal/app/deps present) so the next-step
	// wiring hint matches what the project actually looks like.
	_, err := os.Stat(filepath.Join(dir, "internal", "app", "deps"))
	full := err == nil

	printRESTNextSteps(out, data, full, opts.NoTests)
	return nil
}

// addRESTTest generates tests for an existing REST module: the API test (mocked
// Store) in the entity's api package, an integration suite in tests/, and a
// shared tests/ harness (created once, reused by later suites).
func addRESTTest(out io.Writer, opts addRESTOpts) error {
	if !nameRE.MatchString(opts.Name) {
		return fmt.Errorf("invalid name %q: must start with a letter and contain only letters, digits, '-' or '_'", opts.Name)
	}

	dir := opts.Dir
	if dir == "" {
		dir = "."
	}

	module := opts.Module
	if module == "" {
		m, err := moduleFromGoMod(dir)
		if err != nil {
			return err
		}
		module = m
	}

	words := splitWords(opts.Name)
	pkg := strings.ToLower(strings.Join(words, ""))
	plural := opts.Plural
	if plural == "" {
		plural = pkg + "s"
	}
	data := restData{
		Module: module,
		Pkg:    pkg,
		Type:   pascal(words),
		Recv:   pkg[:1],
		Plural: plural,
	}

	// The tests target an existing module; fail early with a clear pointer if it
	// hasn't been scaffolded yet.
	if _, err := os.Stat(filepath.Join(dir, "core", pkg)); os.IsNotExist(err) {
		return fmt.Errorf("no core/%s in %s — run `skit add rest %s` first", pkg, dir, opts.Name)
	}

	if err := generateRESTTests(out, dir, data); err != nil {
		return err
	}

	printRESTTestNextSteps(out, data)
	return nil
}

// generateRESTTests writes the test suite for one entity, idempotently (existing
// files are skipped, never overwritten). It is shared by `add rest` (which runs
// it unless --no-tests) and `add rest-test`:
//   - api/<pkg>/<pkg>_test.go   — fast API tests over a mocked Store (moq)
//   - tests/<pkg>_store_test.go — store integration tests (testcontainers)
//   - tests/<pkg>_test.go       — HTTP integration suite driving the real handler
//   - tests/harness_test.go     — shared harness, created once and reused
func generateRESTTests(out io.Writer, dir string, data restData) error {
	files := []struct{ dest, tmpl string }{
		{filepath.Join(dir, "api", data.Pkg, data.Pkg+"_test.go"), "templates/rest-test/api_test.go.tmpl"},
		{filepath.Join(dir, "tests", data.Pkg+"_store_test.go"), "templates/rest-test/store_test.go.tmpl"},
		{filepath.Join(dir, "tests", data.Pkg+"_test.go"), "templates/rest-test/integration_test.go.tmpl"},
		{filepath.Join(dir, "tests", "harness_test.go"), "templates/rest-test/harness_test.go.tmpl"},
	}
	for _, f := range files {
		if err := writeIfAbsent(out, f.dest, f.tmpl, data); err != nil {
			return err
		}
	}
	return nil
}

// writeIfAbsent renders tmpl to dest unless dest already exists — the idempotent
// write used across `add`: re-running fills in only what is missing and never
// clobbers a file a developer may have edited.
func writeIfAbsent(out io.Writer, dest, tmpl string, data any) error {
	switch _, err := os.Stat(dest); {
	case err == nil:
		fmt.Fprintf(out, "  skipped %s (exists)\n", dest)
		return nil
	case !os.IsNotExist(err):
		return err
	}
	if err := renderFile(dest, tmpl, data); err != nil {
		return err
	}
	fmt.Fprintf(out, "  created %s\n", dest)
	return nil
}

// printRESTTestNextSteps prints the mock-generation, tidy and run steps.
func printRESTTestNextSteps(out io.Writer, d restData) {
	fmt.Fprintf(out, `
Scaffolded tests for %[1]q. Next:

1. Resolve the new test dependencies (matryer/is, gofakeit) and the moq tool:

   go mod tidy

2. Generate the Store mock (moq) the API test needs:

   make generate        # or: go generate ./...

3. Run them:

   go test ./api/%[1]s/...   # API tests: mocked store, no docker
   go test ./tests/...       # integration (store + HTTP): needs docker, skipped under -short
`, d.Pkg, d.Type)
}

// addConsumerCommand scaffolds a broker-agnostic message consumer: a
// broker.Handler for one event stream plus the shared ConsumerOpts config.
type addConsumerCommand struct {
	Dir     string `long:"dir" default:"." description:"service root containing go.mod (default: current directory)"`
	Module  string `long:"module" description:"module path (default: read from go.mod)"`
	Broker  string `long:"broker" choice:"rmq" choice:"kafka" choice:"nats" default:"rmq" description:"transport for the generated wiring (rmq and kafka have SDK adapters; nats is handler-only for now)"`
	NoTests bool   `long:"no-tests" description:"skip generating the handler test"`
	Args    struct {
		Name string `positional-arg-name:"name" description:"consumer name, e.g. order or order-refund"`
	} `positional-args:"yes" required:"yes"`
}

func (c *addConsumerCommand) Execute([]string) error {
	return addConsumer(os.Stdout, addRESTOpts{
		Dir:     c.Dir,
		Module:  c.Module,
		Name:    c.Args.Name,
		NoTests: c.NoTests,
		Broker:  c.Broker,
	})
}

// addConsumer generates a broker-agnostic consumer: a broker.Handler in
// internal/app/consumers/<name> that depends only on skit/broker (so the
// transport is a wiring choice), plus the shared config.ConsumerOpts.
func addConsumer(out io.Writer, opts addRESTOpts) error {
	if !nameRE.MatchString(opts.Name) {
		return fmt.Errorf("invalid name %q: must start with a letter and contain only letters, digits, '-' or '_'", opts.Name)
	}

	dir := opts.Dir
	if dir == "" {
		dir = "."
	}

	module := opts.Module
	if module == "" {
		m, err := moduleFromGoMod(dir)
		if err != nil {
			return err
		}
		module = m
	}

	words := splitWords(opts.Name)
	pkg := strings.ToLower(strings.Join(words, ""))
	data := restData{
		Module:     module,
		Pkg:        pkg,
		Type:       pascal(words),
		Recv:       pkg[:1],
		UpperSnake: strings.ToUpper(strings.Join(words, "_")),
	}

	transport := opts.Broker
	if transport == "" {
		transport = "rmq"
	}

	consumerDir := filepath.Join(dir, "internal", "app", "consumers", pkg)
	files := []struct{ dest, tmpl string }{
		{filepath.Join(consumerDir, pkg+".go"), "templates/consumer/consumer.go.tmpl"},
		// Shared across consumers: generated once, skipped when it already exists.
		{filepath.Join(dir, "internal", "app", "config", "consumer.go"), "templates/consumer/config.go.tmpl"},
	}
	// The handler is broker-agnostic; the transport wiring is a separate file so a
	// broker swap replaces one file. RabbitMQ and Kafka have SDK adapters; nats
	// does not yet, so its transport is wired by hand.
	switch transport {
	case "rmq":
		files = append(files, struct{ dest, tmpl string }{
			filepath.Join(consumerDir, pkg+"_rmq.go"), "templates/consumer/rmq.go.tmpl",
		})
	case "kafka":
		files = append(files, struct{ dest, tmpl string }{
			filepath.Join(consumerDir, pkg+"_kafka.go"), "templates/consumer/kafka.go.tmpl",
		})
	}
	if !opts.NoTests {
		files = append(files, struct{ dest, tmpl string }{
			filepath.Join(consumerDir, pkg+"_test.go"), "templates/consumer/consumer_test.go.tmpl",
		})
	}

	for _, f := range files {
		if err := writeIfAbsent(out, f.dest, f.tmpl, data); err != nil {
			return err
		}
	}

	printConsumerNextSteps(out, data, transport)
	return nil
}

// printConsumerNextSteps prints the config group and broker wiring a developer
// adds by hand — app-specific and not safe to generate blindly. For rmq the
// transport file (<pkg>_rmq.go) is generated, so wiring is a single Runnable
// call; kafka/nats have no SDK adapter yet, so the handler is generated but the
// transport must be wired by hand.
func printConsumerNextSteps(out io.Writer, d restData, transport string) {
	if transport == "rmq" {
		fmt.Fprintf(out, `
Scaffolded the %[1]q consumer (RabbitMQ). The handler (%[1]s.go) is broker-agnostic;
the transport lives in %[1]s_rmq.go. Next:

1. go mod tidy   # pulls the test dep (matryer/is)

2. Add a config group to ServerOpts (internal/app/config/opts.go):

   %[2]sConsumer config.ConsumerOpts `+"`"+`group:"%[1]s-consumer" namespace:"consumer-%[1]s" env-namespace:"CONSUMER_%[3]s"`+"`"+`

3. Wire it into brokerWorkers (internal/app/server/server.go) — the generated
   Runnable builds the RabbitMQ consumer from config:

   cons, err := %[1]s.New(d.Logger).Runnable(d.BrokerConn(ctx), d.Opts.%[2]sConsumer)
   if err != nil {
       return nil, err   // brokerWorkers returns []worker.Runnable today — thread an error, or log.Fatal on init
   }
   runnables = append(runnables, cons)
   // import: %[1]s "%[4]s/internal/app/consumers/%[1]s"

4. go test ./internal/app/consumers/%[1]s/...
`, d.Pkg, d.Type, d.UpperSnake, d.Module)
		return
	}

	if transport == "kafka" {
		fmt.Fprintf(out, `
Scaffolded the %[1]q consumer (Kafka). The handler (%[1]s.go) is broker-agnostic;
the transport lives in %[1]s_kafka.go. Next:

1. go mod tidy

2. Add a config group to ServerOpts (internal/app/config/opts.go):

   %[2]sConsumer config.ConsumerOpts `+"`"+`group:"%[1]s-consumer" namespace:"consumer-%[1]s" env-namespace:"CONSUMER_%[3]s"`+"`"+`

3. Wire it into the worker group (internal/app/server/server.go). The generated
   Runnable builds the Kafka consumer from config — supply the brokers (from your
   own config/env):

   cons, err := %[1]s.New(d.Logger).Runnable(kafka.Config{Brokers: d.Opts.Kafka.Brokers}, d.Opts.%[2]sConsumer)
   if err != nil {
       return nil, err
   }
   runnables = append(runnables, cons)
   // imports: %[1]s "%[4]s/internal/app/consumers/%[1]s", "github.com/assanoff/skit/broker/kafka"

4. go test ./internal/app/consumers/%[1]s/...
`, d.Pkg, d.Type, d.UpperSnake, d.Module)
		return
	}

	// nats (or any transport without an SDK adapter): handler generated, wire by hand.
	fmt.Fprintf(out, `
Scaffolded the %[1]q consumer handler (broker-agnostic). NOTE: skit has no %[5]s
adapter yet — use rmq or kafka. The handler (%[1]s.go) and config are ready; wire
the transport by hand once the adapter lands (mirror the rmq/kafka case: build a
broker.Subscription from config and bind it to %[1]s.New(d.Logger).Handle).

1. go mod tidy

2. Add a config group to ServerOpts (internal/app/config/opts.go):

   %[2]sConsumer config.ConsumerOpts `+"`"+`group:"%[1]s-consumer" namespace:"consumer-%[1]s" env-namespace:"CONSUMER_%[3]s"`+"`"+`

3. go test ./internal/app/consumers/%[1]s/...
`, d.Pkg, d.Type, d.UpperSnake, d.Module, transport)
}

// addWorkerCommand scaffolds a background worker: a periodic tick loop by
// default, or (with --claim) a queue-backed handler driven by a worker.Processor.
type addWorkerCommand struct {
	Dir     string `long:"dir" default:"." description:"service root containing go.mod (default: current directory)"`
	Module  string `long:"module" description:"module path (default: read from go.mod)"`
	Claim   bool   `long:"claim" description:"queue-backed worker (SDK queue: claim/lease/retry) instead of a periodic tick"`
	NoTests bool   `long:"no-tests" description:"skip generating the worker test"`
	Args    struct {
		Name string `positional-arg-name:"name" description:"worker name, e.g. sweeper or photo-import"`
	} `positional-args:"yes" required:"yes"`
}

func (c *addWorkerCommand) Execute([]string) error {
	return addWorker(os.Stdout, addRESTOpts{
		Dir:     c.Dir,
		Module:  c.Module,
		Name:    c.Args.Name,
		NoTests: c.NoTests,
		Claim:   c.Claim,
	})
}

// addWorker generates a background worker into internal/app/workers/<name>:
// a periodic worker.Loop (Tick) by default, or a queue.JobFunc (Handle) driven
// by a worker.Processor when opts.Claim is set.
func addWorker(out io.Writer, opts addRESTOpts) error {
	if !nameRE.MatchString(opts.Name) {
		return fmt.Errorf("invalid name %q: must start with a letter and contain only letters, digits, '-' or '_'", opts.Name)
	}

	dir := opts.Dir
	if dir == "" {
		dir = "."
	}

	module := opts.Module
	if module == "" {
		m, err := moduleFromGoMod(dir)
		if err != nil {
			return err
		}
		module = m
	}

	words := splitWords(opts.Name)
	pkg := strings.ToLower(strings.Join(words, ""))
	data := restData{
		Module: module,
		Pkg:    pkg,
		Type:   pascal(words),
		Recv:   pkg[:1],
	}

	mainTmpl := "templates/worker/tick.go.tmpl"
	testTmpl := "templates/worker/tick_test.go.tmpl"
	if opts.Claim {
		mainTmpl = "templates/worker/claim.go.tmpl"
		testTmpl = "templates/worker/claim_test.go.tmpl"
	}

	workerDir := filepath.Join(dir, "internal", "app", "workers", pkg)
	files := []struct{ dest, tmpl string }{
		{filepath.Join(workerDir, pkg+".go"), mainTmpl},
	}
	if !opts.NoTests {
		files = append(files, struct{ dest, tmpl string }{
			filepath.Join(workerDir, pkg+"_test.go"), testTmpl,
		})
	}

	for _, f := range files {
		if err := writeIfAbsent(out, f.dest, f.tmpl, data); err != nil {
			return err
		}
	}

	printWorkerNextSteps(out, data, opts.Claim)
	return nil
}

// printWorkerNextSteps prints the worker-group wiring a developer adds by hand,
// branching on the worker kind (periodic tick vs queue-backed processor).
func printWorkerNextSteps(out io.Writer, d restData, claim bool) {
	if claim {
		fmt.Fprintf(out, `
Scaffolded the %[1]q queue-backed worker (task kind %[1]s.Kind). Next:

1. go mod tidy   # test dep (matryer/is)

2. Provision the queue table once — add queue.Schema() to a migration, or call
   EnsureSchema at startup — and expose a queue.Queue in deps if absent:

   q := queue.NewPG(d.Logger, d.DB(ctx), queue.Options{})
   _ = q.EnsureSchema(ctx)

3. Wire the processor into the worker group (internal/app/server/server.go), in
   the "if !opts.Worker.Disabled" block:

   mux := queue.NewMux()
   _ = mux.Register(%[1]s.Kind, %[1]s.New(d.Logger).Handle)
   proc := worker.NewProcessor[queue.Task](d.Logger.Slog(), q, mux, q, worker.ProcessorConfig{})
   extra = append(extra, worker.NewPacedLoop(d.Logger.Slog(),
       worker.LoopConfig{Name: %[1]q + "-worker", Interval: time.Second}, proc.PacedTick()))
   // imports: %[1]s "%[2]s/internal/app/workers/%[1]s", "github.com/assanoff/skit/queue", "github.com/assanoff/skit/worker"

4. Enqueue work: q.Schedule(ctx, queue.ScheduleParams{Kind: %[1]s.Kind, Payload: ...})

5. go test ./internal/app/workers/%[1]s/...
`, d.Pkg, d.Module)
		return
	}

	fmt.Fprintf(out, `
Scaffolded the %[1]q periodic worker. Next:

1. go mod tidy   # test dep (matryer/is)

2. Wire it into the worker group (internal/app/server/server.go), in the
   "if !opts.Worker.Disabled" block:

   extra = append(extra, %[1]s.New(d.Logger).Loop(d.Opts.Worker.Interval))
   // import: %[1]s "%[2]s/internal/app/workers/%[1]s"

3. go test ./internal/app/workers/%[1]s/...
`, d.Pkg, d.Module)
}

// cronData is the template payload for a scheduled job.
type cronData struct {
	Module   string
	Pkg      string
	Type     string
	Recv     string
	Schedule string // cron spec, e.g. "@every 1m" or "0 3 * * *"
	Locked   bool   // true when --lock != none: gate each tick on a lock.Locker
	Backend  string // lock backend: "postgres" | "redis" | "" (none)
}

// addCronCommand scaffolds a scheduled job into internal/app/crons/<name>: a
// cron.Scheduler-driven job, optionally gated by a distributed lock so it fires
// on at most one replica per tick.
type addCronCommand struct {
	Dir      string `long:"dir" default:"." description:"service root containing go.mod (default: current directory)"`
	Module   string `long:"module" description:"module path (default: read from go.mod)"`
	Schedule string `long:"schedule" default:"@every 1m" description:"cron spec: 5-field cron (min hour dom month dow) or an @-descriptor (@hourly, @every 1h30m)"`
	Lock     string `long:"lock" choice:"none" choice:"postgres" choice:"redis" default:"none" description:"distributed lock so the job runs on one replica per tick: none | postgres (advisory lock) | redis (SET NX)"`
	NoTests  bool   `long:"no-tests" description:"skip generating the cron test"`
	Args     struct {
		Name string `positional-arg-name:"name" description:"cron name, e.g. cleanup or daily-report"`
	} `positional-args:"yes" required:"yes"`
}

func (c *addCronCommand) Execute([]string) error {
	return addCron(os.Stdout, c.Dir, c.Module, c.Args.Name, c.Schedule, c.Lock, c.NoTests)
}

// addCron generates a scheduled job into internal/app/crons/<name>. The job runs
// on schedule via a cron.Scheduler; with lockBackend != "none" each tick is
// gated on a lock.Locker so the job fires on one replica at a time.
func addCron(out io.Writer, dir, module, name, schedule, lockBackend string, noTests bool) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("invalid name %q: must start with a letter and contain only letters, digits, '-' or '_'", name)
	}
	if dir == "" {
		dir = "."
	}
	if module == "" {
		m, err := moduleFromGoMod(dir)
		if err != nil {
			return err
		}
		module = m
	}

	words := splitWords(name)
	pkg := strings.ToLower(strings.Join(words, ""))
	data := cronData{
		Module:   module,
		Pkg:      pkg,
		Type:     pascal(words),
		Recv:     pkg[:1],
		Schedule: schedule,
		Locked:   lockBackend != "none",
		Backend:  lockBackend,
	}
	if !data.Locked {
		data.Backend = ""
	}

	cronDir := filepath.Join(dir, "internal", "app", "crons", pkg)
	files := []struct{ dest, tmpl string }{
		{filepath.Join(cronDir, pkg+".go"), "templates/cron/cron.go.tmpl"},
	}
	if !noTests {
		files = append(files, struct{ dest, tmpl string }{
			filepath.Join(cronDir, pkg+"_test.go"), "templates/cron/cron_test.go.tmpl",
		})
	}

	for _, f := range files {
		if err := writeIfAbsent(out, f.dest, f.tmpl, data); err != nil {
			return err
		}
	}

	printCronNextSteps(out, data)
	return nil
}

// printCronNextSteps prints the worker-group wiring, branching on the lock
// backend (none / postgres / redis) since each builds its Locker differently.
func printCronNextSteps(out io.Writer, d cronData) {
	fmt.Fprintf(out, `
Scaffolded the %[1]q cron (schedule %[2]q). Next:

1. go mod tidy   # deps: robfig/cron, matryer/is
`, d.Pkg, d.Schedule)

	switch d.Backend {
	case "postgres":
		fmt.Fprintf(out, `
2. Wire it into the worker group (internal/app/server/server.go), building a
   Postgres advisory-lock Locker:

   locker := lock.NewPG(d.DB(ctx), d.Logger.Slog())
   sched, err := %[1]s.New(d.Logger, locker).Scheduler()
   if err != nil { return err }
   extra = append(extra, sched)
   // imports: %[1]s "%[2]s/internal/app/crons/%[1]s", "github.com/assanoff/skit/lock"

3. go test ./internal/app/crons/%[1]s/...
`, d.Pkg, d.Module)
	case "redis":
		fmt.Fprintf(out, `
2. Enable Redis (REDIS_ENABLED=true) and wire it into the worker group
   (internal/app/server/server.go), building a Redis Locker:

   locker := lock.NewRedis(d.Redis(ctx), d.Logger.Slog())
   sched, err := %[1]s.New(d.Logger, locker).Scheduler()
   if err != nil { return err }
   extra = append(extra, sched)
   // imports: %[1]s "%[2]s/internal/app/crons/%[1]s", "github.com/assanoff/skit/lock"

3. go test ./internal/app/crons/%[1]s/...
`, d.Pkg, d.Module)
	default:
		fmt.Fprintf(out, `
2. Wire it into the worker group (internal/app/server/server.go):

   sched, err := %[1]s.New(d.Logger).Scheduler()
   if err != nil { return err }
   extra = append(extra, sched)
   // import: %[1]s "%[2]s/internal/app/crons/%[1]s"

   Note: without a lock this fires on EVERY replica. Use --lock postgres|redis
   to run it on one replica per tick.

3. go test ./internal/app/crons/%[1]s/...
`, d.Pkg, d.Module)
	}
}

// addEventCommand scaffolds a domain event: a typed payload plus its outbox
// route registration — the producing side that complements `add consumer`.
type addEventCommand struct {
	Dir       string `long:"dir" default:"." description:"service root containing go.mod (default: current directory)"`
	Module    string `long:"module" description:"module path (default: read from go.mod)"`
	Plural    string `long:"plural" description:"event/topic plural (default: <name>+\"s\")"`
	WithRelay bool   `long:"with-relay" description:"also scaffold the service-wide outbox relay bootstrap (Store + Registry + Relay); created once, skipped if present"`
	NoTests   bool   `long:"no-tests" description:"skip generating the event test"`
	Args      struct {
		Name string `positional-arg-name:"name" description:"event name, e.g. advert-created or order-paid"`
	} `positional-args:"yes" required:"yes"`
}

func (c *addEventCommand) Execute([]string) error {
	return addEvent(os.Stdout, addRESTOpts{
		Dir:       c.Dir,
		Module:    c.Module,
		Plural:    c.Plural,
		Name:      c.Args.Name,
		NoTests:   c.NoTests,
		WithRelay: c.WithRelay,
	})
}

// addEvent generates a domain event into internal/app/events/<name>: a typed
// payload + Register (its outbox route). With opts.WithRelay it also writes the
// service-wide, event-agnostic relay bootstrap (internal/app/events/relay.go),
// created once and skipped on re-run.
func addEvent(out io.Writer, opts addRESTOpts) error {
	if !nameRE.MatchString(opts.Name) {
		return fmt.Errorf("invalid name %q: must start with a letter and contain only letters, digits, '-' or '_'", opts.Name)
	}

	dir := opts.Dir
	if dir == "" {
		dir = "."
	}

	module := opts.Module
	if module == "" {
		m, err := moduleFromGoMod(dir)
		if err != nil {
			return err
		}
		module = m
	}

	words := splitWords(opts.Name)
	pkg := strings.ToLower(strings.Join(words, ""))
	plural := opts.Plural
	if plural == "" {
		plural = pkg + "s"
	}
	data := restData{
		Module: module,
		Pkg:    pkg,
		Type:   pascal(words),
		Recv:   pkg[:1],
		Plural: plural,
	}

	eventsDir := filepath.Join(dir, "internal", "app", "events")
	pkgDir := filepath.Join(eventsDir, pkg)
	files := []struct{ dest, tmpl string }{
		{filepath.Join(pkgDir, pkg+".go"), "templates/event/event.go.tmpl"},
	}
	if !opts.NoTests {
		files = append(files, struct{ dest, tmpl string }{
			filepath.Join(pkgDir, pkg+"_test.go"), "templates/event/event_test.go.tmpl",
		})
	}
	if opts.WithRelay {
		// Service-wide, event-agnostic: one relay per service. writeIfAbsent keeps
		// it idempotent — created on the first --with-relay, skipped thereafter.
		files = append(files, struct{ dest, tmpl string }{
			filepath.Join(eventsDir, "relay.go"), "templates/event/relay.go.tmpl",
		})
	}

	for _, f := range files {
		if err := writeIfAbsent(out, f.dest, f.tmpl, data); err != nil {
			return err
		}
	}

	printEventNextSteps(out, data, opts.WithRelay)
	return nil
}

// printEventNextSteps prints the outbox wiring a developer adds by hand:
// register the route, run the relay, and publish transactionally.
func printEventNextSteps(out io.Writer, d restData, withRelay bool) {
	fmt.Fprintf(out, `
Scaffolded the %[1]q event (CloudEvents type %[1]s.EventType, topic %[1]s.Topic). Next:

1. go mod tidy   # test dep (matryer/is)
`, d.Pkg)

	if withRelay {
		fmt.Fprintf(out, `
2. Register the route in internal/app/events/relay.go (NewRegistry):

   if err := %[1]s.Register(reg); err != nil { return nil, err }
   // import: %[1]s "%[2]s/internal/app/events/%[1]s"

3. Wire the outbox at startup (deps/server), passing your broker publisher:

   store, err := events.NewStore(ctx, d.Logger, d.DB(ctx))
   reg, err  := events.NewRegistry()
   extra = append(extra, events.NewRelay(d.Logger, store, pub)) // pub = rabbitmq/kafka NewPublisher
   // import: "%[2]s/internal/app/events"

4. Publish transactionally from your core (atomic with the domain write):

   outbox.WithinTran(ctx, log, db, store, reg, func(tx *sqlx.Tx, pub outbox.Publisher) error {
       // ... domain writes on tx ...
       return pub.Publish(ctx, %[1]s.%[3]s{ID: id}, outbox.WithRouteKey(id.String()))
   })

5. go test ./internal/app/events/%[1]s/...
`, d.Pkg, d.Module, d.Type)
		return
	}

	fmt.Fprintf(out, `
2. Build the outbox once at startup (or generate it with --with-relay):

   store := outbox.NewPG(d.Logger, d.DB(ctx), outbox.Options{}); _ = store.EnsureSchema(ctx)
   reg := outbox.NewRegistry(); _ = %[1]s.Register(reg)
   extra = append(extra, outbox.NewRelay(d.Logger, store, pub, outbox.RelayConfig{})) // pub = broker publisher

3. Publish transactionally from your core (atomic with the domain write):

   outbox.WithinTran(ctx, log, db, store, reg, func(tx *sqlx.Tx, pub outbox.Publisher) error {
       // ... domain writes on tx ...
       return pub.Publish(ctx, %[1]s.%[3]s{ID: id}, outbox.WithRouteKey(id.String()))
   })

4. go test ./internal/app/events/%[1]s/...
`, d.Pkg, d.Module, d.Type)
}

// addGRPCCommand scaffolds a gRPC module for one entity: a .proto contract plus
// a thin handler that adapts the generated service to the entity's Core.
type addGRPCCommand struct {
	Dir    string `long:"dir" default:"." description:"service root containing go.mod (default: current directory)"`
	Module string `long:"module" description:"module path (default: read from go.mod)"`
	Plural string `long:"plural" description:"List RPC / list-field plural (default: <name>+\"s\")"`
	Args   struct {
		Name string `positional-arg-name:"name" description:"entity name, e.g. widget or order-line"`
	} `positional-args:"yes" required:"yes"`
}

func (c *addGRPCCommand) Execute([]string) error {
	return addGRPC(os.Stdout, addRESTOpts{
		Dir:    c.Dir,
		Module: c.Module,
		Plural: c.Plural,
		Name:   c.Args.Name,
	})
}

// grpcData is the template payload for the generated proto + handler.
type grpcData struct {
	Module     string // github.com/you/svc
	Pkg        string // widget    — package name + proto package (lower, no separators)
	Type       string // Widget    — exported Go type / message name
	Recv       string // w         — short local/var name
	Snake      string // widget / order_line — proto singular field (pascalizes back to Type)
	Plural     string // widgets   — proto list field
	PluralType string // Widgets   — List RPC / list builder field (pascal of Plural)
}

// addGRPC generates a .proto contract and a gRPC handler adapting it to the Core.
func addGRPC(out io.Writer, opts addRESTOpts) error {
	if !nameRE.MatchString(opts.Name) {
		return fmt.Errorf("invalid name %q: must start with a letter and contain only letters, digits, '-' or '_'", opts.Name)
	}

	dir := opts.Dir
	if dir == "" {
		dir = "."
	}

	module := opts.Module
	if module == "" {
		m, err := moduleFromGoMod(dir)
		if err != nil {
			return err
		}
		module = m
	}

	words := splitWords(opts.Name)
	pkg := strings.ToLower(strings.Join(words, ""))
	plural := opts.Plural
	if plural == "" {
		plural = pkg + "s"
	}
	data := grpcData{
		Module:     module,
		Pkg:        pkg,
		Type:       pascal(words),
		Recv:       pkg[:1],
		Snake:      strings.ToLower(strings.Join(words, "_")),
		Plural:     plural,
		PluralType: pascal(splitWords(plural)),
	}

	protoFile := filepath.Join(dir, "proto", pkg, "v1", pkg+".proto")
	handlerFile := filepath.Join(dir, "internal", "app", "handlers", pkg+"grpc", pkg+"grpc.go")
	files := []struct{ dest, tmpl string }{
		{protoFile, "templates/grpc/proto.tmpl"},
		{handlerFile, "templates/grpc/handler.go.tmpl"},
	}
	for _, f := range files {
		if _, err := os.Stat(f.dest); err == nil {
			return fmt.Errorf("%s already exists — refusing to overwrite", f.dest)
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	for _, f := range files {
		if err := renderFile(f.dest, f.tmpl, data); err != nil {
			return err
		}
		fmt.Fprintf(out, "  created %s\n", f.dest)
	}

	printGRPCNextSteps(out, data)
	return nil
}

// printGRPCNextSteps prints the codegen and wiring a developer must run by hand:
// the generated gen/ code and the server registration are app-specific.
func printGRPCNextSteps(out io.Writer, d grpcData) {
	fmt.Fprintf(out, `
Scaffolded the %q gRPC module. The handler adapts %s.Core, so run `+"`skit add rest %s`"+` first if that module does not exist yet. Next:

1. Generate the protobuf + gRPC code (commits to gen/%s/v1/):

   make proto      # buf lint proto && buf generate proto

2. Register the service where you build the gRPC server (e.g. app/server):

   gs.Install(%sgrpc.New(core))   // the handler's Register owns the generated %sv1 call
   // import: %sgrpc "%s/internal/app/handlers/%sgrpc"

3. go build ./...
`, d.Pkg, d.Pkg, d.Pkg, d.Pkg, d.Pkg, d.Pkg, d.Pkg, d.Module, d.Pkg)
}

// printRESTNextSteps prints the migration and wiring a developer must add by
// hand — they are app-specific (migration numbering, the deps container) and not
// safe to generate blindly. The wiring hint (step 2) adapts to the project shape:
// the full bootstrap wires through internal/app/deps + internal/app/server, the
// minimal starter directly where the router is built.
func printRESTNextSteps(out io.Writer, d restData, full, noTests bool) {
	// Step 1 — migration (same for both project shapes).
	fmt.Fprintf(out, `
Scaffolded the %[1]q module. Next:

1. Add a migration for the table (e.g. internal/migrations/NNNN_%[2]s.sql). The
   composite index backs the keyset (cursor) listing — GET /%[2]s/cursor:

   CREATE TABLE %[2]s (
       id          UUID PRIMARY KEY,
       name        TEXT NOT NULL,
       description TEXT NOT NULL DEFAULT '',
       created_at  TIMESTAMPTZ NOT NULL,
       updated_at  TIMESTAMPTZ NOT NULL
   );
   CREATE INDEX %[2]s_created_at_id_desc_idx ON %[2]s (created_at DESC, id DESC);
`, d.Pkg, d.Plural)

	// Step 2 — wiring, tailored to the project shape.
	if full {
		// [1]=Pkg [2]=Type [3]=Module
		fmt.Fprintf(out, `
2. Wire it into the full bootstrap:

   a) internal/app/deps/deps.go — add providers to Deps and register the initializer:

      %[2]sCore    dim.Provider[*%[1]s.Core]
      %[2]sHandler dim.Provider[*%[1]sapi.Handler]
      // ... add init%[2]s to the Initializers slice

   b) new file internal/app/deps/%[1]s.go:

      var init%[2]s = func(c *Deps) (dim.CleanupFunc, error) {
          c.%[2]sCore = dim.Once(func(ctx context.Context) (*%[1]s.Core, error) {
              return %[1]s.NewCore(c.Logger, %[1]sdb.NewStore(c.Logger, c.DB(ctx))), nil
          })
          c.%[2]sHandler = dim.Once(func(ctx context.Context) (*%[1]sapi.Handler, error) {
              return %[1]sapi.New(c.%[2]sCore(ctx)), nil
          })
          return nil, nil
      }
      // imports: "%[3]s/core/%[1]s", "%[3]s/core/%[1]s/%[1]sdb", %[1]sapi "%[3]s/api/%[1]s"

   c) internal/app/server (Install) — register the routes on the handle seam:

      d.%[2]sHandler(ctx).Routes(handle)
`, d.Pkg, d.Type, d.Module)
	} else {
		// [1]=Pkg [2]=Module
		fmt.Fprintf(out, `
2. Wire it where you build the router:

   store := %[1]sdb.NewStore(log, db)
   core  := %[1]s.NewCore(log, store)
   %[1]sapi.New(core).Routes(r.HandleApp)   // import alias: %[1]sapi "%[2]s/api/%[1]s"
`, d.Pkg, d.Module)
	}

	// Step 3 — tidy/build (same for both).
	fmt.Fprintf(out, `
3. go mod tidy && go build ./...
`)

	// Step 4 — the generated tests, or a pointer to add them when skipped.
	if noTests {
		fmt.Fprintf(out, `
4. Tests were skipped (--no-tests). Add API + integration tests with:

   skit add rest-test %[1]s
`, d.Pkg)
	} else {
		fmt.Fprintf(out, `
4. Tests were generated: api/%[1]s (mocked store, no docker) and tests/ (store +
   HTTP integration against a real Postgres via testcontainers, skipped under
   -short). After 'go mod tidy' and 'make generate' (for the moq mock):

   go test ./api/%[1]s/...   # no docker
   go test ./tests/...       # needs docker
`, d.Pkg)
	}
}

// moduleFromGoMod reads the module path from dir/go.mod.
func moduleFromGoMod(dir string) (string, error) {
	path := filepath.Join(dir, "go.mod")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no go.mod in %s — run this from a service root or pass --module", dir)
		}
		return "", err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no module declaration found in %s", path)
}

// splitWords splits an identifier into its words on separators (-, _, space, .)
// and camelCase boundaries, so "order-line", "order_line" and "orderLine" all
// yield ["order", "line"].
func splitWords(s string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = nil
		}
	}
	var prev rune
	for i, r := range s {
		switch {
		case r == '-' || r == '_' || r == ' ' || r == '.':
			flush()
		case unicode.IsUpper(r) && i > 0 && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
		prev = r
	}
	flush()
	return words
}

// pascal joins words into an exported Go identifier (PascalCase).
func pascal(words []string) string {
	var b strings.Builder
	for _, w := range words {
		if w == "" {
			continue
		}
		b.WriteString(strings.ToUpper(w[:1]))
		b.WriteString(strings.ToLower(w[1:]))
	}
	return b.String()
}
