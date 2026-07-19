package main

import (
	"fmt"
	"io"
	"os"

	flags "github.com/jessevdk/go-flags"
)

// version is overridable at build time with -ldflags "-X main.version=...".
var version = "dev"

type newCommand struct {
	Dir        string `long:"dir" description:"target directory (default: last element of the module path)"`
	Template   string `long:"template" description:"gonew template module; delegates to gonew when set"`
	Full       bool   `long:"full" description:"scaffold the full HTTP service bootstrap (config/deps/server/cmd/migrations + docker-compose), not just the minimal starter"`
	Replace    string `long:"replace" description:"local skit path for a go.mod replace directive, e.g. ../skit (for co-developing the SDK alongside the service)"`
	SDKVersion string `long:"sdk-version" default:"latest" description:"skit version for go.mod (\"latest\" omits the require — run go mod tidy)"`
	Args       struct {
		Module string `positional-arg-name:"module-path" description:"new module path, e.g. github.com/you/svc"`
	} `positional-args:"yes" required:"yes"`
}

func (c *newCommand) Execute([]string) error {
	return scaffold(os.Stdout, scaffoldOpts{
		Module:     c.Args.Module,
		Dir:        c.Dir,
		SDKVersion: c.SDKVersion,
		Template:   c.Template,
		Full:       c.Full,
		Replace:    c.Replace,
	})
}

type versionCommand struct{}

func (versionCommand) Execute([]string) error {
	fmt.Println("skit", version)
	return nil
}

// printIntro is shown when skit is run with no command, so a first-time user
// sees the getting-started path rather than a terse "specify a command" error.
func printIntro(w io.Writer) {
	fmt.Fprint(w, `skit — scaffold and manage skit-based services.

Get started — scaffold a full HTTP service (config, deps, server, migrations, docker-compose):

  skit new github.com/you/svc --full
  cd svc && go mod tidy

Then add modules to the service:

  skit add rest <name>          # REST CRUD (core + store + transport + tests)
  skit add grpc <name>          # gRPC module (.proto + handler)
  skit add consumer <name>      # broker-agnostic message consumer
  skit add worker <name>        # background worker (--claim for queue-backed)
  skit add migration <name>     # next numbered goose migration (NNNN_<name>.sql)

Help:

  skit -h                       # top-level help and all commands
  skit <command> -h             # help for a command (new, add rest, add grpc, add consumer, add worker, add migration, version)
`)
}

func main() {
	// No command → show the getting-started intro instead of go-flags' terse
	// "Please specify one command" error.
	if len(os.Args) == 1 {
		printIntro(os.Stdout)
		return
	}

	var opts struct{}
	p := flags.NewParser(&opts, flags.Default)
	p.LongDescription = "skit scaffolds and manages skit-based services."

	if _, err := p.AddCommand("new", "scaffold a new service",
		"Create a new skit service module.", &newCommand{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := p.AddCommand("version", "print the skit CLI version", "", &versionCommand{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	add, err := p.AddCommand("add", "add a module to the current service",
		"Scaffold a module into the service in the current directory.", &addCommand{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := add.AddCommand("rest", "scaffold a REST CRUD module",
		"Generate core + Postgres store + REST transport for one entity.", &addRestCommand{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := add.AddCommand("rest-test", "scaffold tests for a REST module",
		"Generate API (mocked-store) tests and an integration suite for one entity.", &addRestTestCommand{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := add.AddCommand("grpc", "scaffold a gRPC module",
		"Generate a .proto contract + a gRPC handler adapting one entity's Core.", &addGRPCCommand{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := add.AddCommand("consumer", "scaffold a broker consumer",
		"Generate a broker-agnostic message consumer (broker.Handler) for one event stream.", &addConsumerCommand{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := add.AddCommand("worker", "scaffold a background worker",
		"Generate a periodic tick worker (or, with --claim, a queue-backed processor).", &addWorkerCommand{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := add.AddCommand("migration", "scaffold the next goose migration",
		"Generate internal/migrations/NNNN_<name>.sql with the next sequence number.", &addMigrationCommand{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if _, err := p.Parse(); err != nil {
		// go-flags already prints help/usage for parse errors.
		os.Exit(1)
	}
}
