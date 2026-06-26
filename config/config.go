package config

import (
	"errors"
	"os"

	"github.com/jessevdk/go-flags"
	"github.com/joho/godotenv"
)

// LoadDotenv loads the given .env files (default ".env") into the process
// environment if present. Missing files are ignored, and existing environment
// variables are never overwritten — in production values come from the
// orchestrator, not a file.
func LoadDotenv(files ...string) error {
	if len(files) == 0 {
		files = []string{".env"}
	}
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			continue // ignore missing files
		}
		if err := godotenv.Load(f); err != nil {
			return err
		}
	}
	return nil
}

// NewParser builds a go-flags parser with a deterministic env-namespace
// delimiter ("_") so nested env names are predictable (DB_USER, HTTP_ADDR, ...).
// data is typically a pointer to an options struct, optionally containing
// `command:`-tagged subcommands that implement flags.Commander.
func NewParser(data any) *flags.Parser {
	p := flags.NewParser(data, flags.Default)
	p.NamespaceDelimiter = "."
	p.EnvNamespaceDelimiter = "_"
	return p
}

// Parse loads the dotenv files (if any) then parses os.Args into data,
// dispatching any matched subcommand's Execute. It returns the go-flags error
// unchanged so callers can detect --help via IsHelp.
func Parse(data any, dotenvFiles ...string) error {
	if err := LoadDotenv(dotenvFiles...); err != nil {
		return err
	}
	_, err := NewParser(data).Parse()
	return err
}

// IsHelp reports whether err is the benign "help requested" error, which
// callers should treat as a clean exit.
func IsHelp(err error) bool {
	var fe *flags.Error
	return errors.As(err, &fe) && fe.Type == flags.ErrHelp
}

// Reset removes variables from the environment, e.g. to drop a secret after it
// has been consumed so it cannot leak into child processes or crash dumps.
func Reset(keys ...string) {
	for _, k := range keys {
		_ = os.Unsetenv(k)
	}
}
