package main

import (
	"fmt"
	"os"

	flags "github.com/jessevdk/go-flags"
)

// version is overridable at build time with -ldflags "-X main.version=...".
var version = "dev"

type newCommand struct {
	Dir        string `long:"dir" description:"target directory (default: last element of the module path)"`
	Template   string `long:"template" description:"gonew template module; delegates to gonew when set"`
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
	})
}

type versionCommand struct{}

func (versionCommand) Execute([]string) error {
	fmt.Println("skit", version)
	return nil
}

func main() {
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
	if _, err := add.AddCommand("grpc", "scaffold a gRPC module",
		"Generate a .proto contract + a gRPC handler adapting one entity's Core.", &addGRPCCommand{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if _, err := p.Parse(); err != nil {
		// go-flags already prints help/usage for parse errors.
		os.Exit(1)
	}
}
