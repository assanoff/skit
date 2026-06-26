package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jessevdk/go-flags"
	"github.com/matryer/is"

	"github.com/assanoff/skit/config"
)

func TestLoadDotenvLoadsPresentAndIgnoresMissing(t *testing.T) {
	is := is.New(t)

	dir := t.TempDir()
	envFile := filepath.Join(dir, "test.env")
	if err := os.WriteFile(envFile, []byte("SK_TEST_FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_ = os.Unsetenv("SK_TEST_FOO")
	t.Cleanup(func() {
		_ = os.Unsetenv("SK_TEST_FOO")
	})

	// A missing file is silently skipped; the present file is loaded.
	err := config.LoadDotenv(filepath.Join(dir, "nope.env"), envFile)
	is.NoErr(err)
	is.Equal(os.Getenv("SK_TEST_FOO"), "bar")
}

func TestLoadDotenvDoesNotOverwriteExisting(t *testing.T) {
	is := is.New(t)

	dir := t.TempDir()
	envFile := filepath.Join(dir, "test.env")
	if err := os.WriteFile(envFile, []byte("SK_TEST_KEEP=fromfile\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SK_TEST_KEEP", "fromenv") // already set in the environment

	err := config.LoadDotenv(envFile)
	is.NoErr(err)
	is.Equal(os.Getenv("SK_TEST_KEEP"), "fromenv") // the environment wins over the file
}

func TestNewParserDelimiters(t *testing.T) {
	is := is.New(t)

	var opts struct{}
	p := config.NewParser(&opts)

	is.Equal(p.NamespaceDelimiter, ".")    // nested flags joined with "."
	is.Equal(p.EnvNamespaceDelimiter, "_") // nested env names joined with "_"
}

func TestParseReadsEnvWithNoArgs(t *testing.T) {
	is := is.New(t)

	type opts struct {
		Name string `long:"name" env:"SK_TEST_NAME"`
	}

	t.Setenv("SK_TEST_NAME", "fromenv")

	oldArgs := os.Args
	t.Cleanup(func() {
		os.Args = oldArgs
	})
	os.Args = []string{"testbin"} // no flags -> the env value supplies Name

	var o opts
	err := config.Parse(&o) // no dotenv files; cwd has no .env
	is.NoErr(err)
	is.Equal(o.Name, "fromenv")
}

func TestIsHelp(t *testing.T) {
	is := is.New(t)

	is.True(config.IsHelp(&flags.Error{Type: flags.ErrHelp}))         // the benign help error
	is.True(!config.IsHelp(&flags.Error{Type: flags.ErrUnknownFlag})) // other flags errors are not help
	is.True(!config.IsHelp(errors.New("other")))                      // unrelated errors are not help
	is.True(!config.IsHelp(nil))                                      // nil is not help
}

func TestReset(t *testing.T) {
	is := is.New(t)

	t.Setenv("SK_TEST_SECRET", "shh")
	config.Reset("SK_TEST_SECRET")

	_, ok := os.LookupEnv("SK_TEST_SECRET")
	is.True(!ok) // removed from the environment
}
