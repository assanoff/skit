package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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

type addRESTOpts struct {
	Dir     string // service root (holds go.mod)
	Module  string // module path; resolved from go.mod when empty
	Plural  string // override for routes/table; defaults to Pkg+"s"
	Name    string // raw entity name
	NoTests bool   // skip generating tests
}

// restData is the template payload shared by every generated REST file.
type restData struct {
	Module string // github.com/you/svc
	Pkg    string // widget   — package name (lower, no separators)
	Type   string // Widget   — exported Go type
	Recv   string // w        — short local/var name (first letter of Pkg)
	Plural string // widgets  — route path + table name
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
		{filepath.Join(dbPkgDir, pkg+"db.go"), "templates/rest/db.go.tmpl"},
		{filepath.Join(dbPkgDir, "model.go"), "templates/rest/db_model.go.tmpl"},
		{filepath.Join(dbPkgDir, "order.go"), "templates/rest/db_order.go.tmpl"},
		{filepath.Join(dbPkgDir, "filter.go"), "templates/rest/db_filter.go.tmpl"},
		{filepath.Join(apiPkgDir, pkg+".go"), "templates/rest/api.go.tmpl"},
		{filepath.Join(apiPkgDir, "model.go"), "templates/rest/api_model.go.tmpl"},
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
