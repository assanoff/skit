package config_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/assanoff/skit/config"
)

type options struct {
	Addr string `long:"addr" env:"HTTP_ADDR" default:":8080" description:"listen address"`
	DB   struct {
		User string `long:"user" env:"USER" default:"app" description:"db user"`
	} `group:"db" namespace:"db" env-namespace:"DB"`
}

// One struct drives the flags, the environment and --help. The group with
// env-namespace DB reads DB_USER; a flag wins over the environment, the
// environment over the default.
func Example() {
	_ = os.Setenv("DB_USER", "reporter")
	defer os.Unsetenv("DB_USER")

	var opts options
	_, err := config.NewParser(&opts).ParseArgs([]string{"--addr", ":9090"})
	fmt.Println(opts.Addr, opts.DB.User, err)
	// Output:
	// :9090 reporter <nil>
}

// LoadDotenv seeds the environment from a .env file; a variable already set
// keeps its value.
func ExampleLoadDotenv() {
	dir, _ := os.MkdirTemp("", "dotenv")
	defer os.RemoveAll(dir)
	env := filepath.Join(dir, ".env")
	_ = os.WriteFile(env, []byte("HTTP_ADDR=:7070\nDB_USER=dev\n"), 0o600)
	_ = os.Setenv("DB_USER", "ci")
	defer os.Unsetenv("DB_USER")
	defer os.Unsetenv("HTTP_ADDR")

	if err := config.LoadDotenv(env, filepath.Join(dir, "missing.env")); err != nil {
		fmt.Println(err)
	}
	fmt.Println(os.Getenv("HTTP_ADDR"), os.Getenv("DB_USER"))
	// Output:
	// :7070 ci
}

// Reset drops a secret from the environment once it is read, so it does not
// leak into child processes.
func ExampleReset() {
	_ = os.Setenv("DB_PASSWORD", "s3cret")

	config.Reset("DB_PASSWORD")
	_, ok := os.LookupEnv("DB_PASSWORD")
	fmt.Println(ok)
	// Output:
	// false
}
