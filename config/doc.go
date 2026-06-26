// Package config standardizes 12-factor configuration on jessevdk/go-flags: a
// single options struct whose fields carry `long`/`env`/`default`/`description`
// tags, so the same definition drives CLI flags, environment variables, and
// `--help`.
//
// Nested groups use `group`/`namespace`/`env-namespace` tags; the parser uses a
// "." flag delimiter and a "_" env-namespace delimiter, so a group with
// env-namespace "DB" and a field with env "USER" reads DB_USER. For local
// development LoadDotenv seeds the environment from a .env file, but real
// environment variables always take precedence.
//
// # Usage
//
// Define one options struct, parse it, and treat a --help request as a clean
// exit:
//
//	type Config struct {
//	    Addr string `long:"addr" env:"HTTP_ADDR" default:":8080" description:"listen address"`
//	    DB   struct {
//	        User string `long:"user" env:"USER" description:"db user"`
//	    } `group:"db" namespace:"db" env-namespace:"DB"` // reads DB_USER
//	}
//
//	var cfg Config
//	if err := config.Parse(&cfg); err != nil {
//	    if config.IsHelp(err) {
//	        os.Exit(0)
//	    }
//	    log.Fatal(err)
//	}
//
// After consuming a secret, drop it from the environment so it cannot leak into
// child processes or crash dumps:
//
//	config.Reset("DB_PASSWORD")
//
// # API
//
//   - Parse(data, dotenvFiles...): load dotenv then parse os.Args into data,
//     dispatching any matched `command:`-tagged subcommand's Execute.
//   - NewParser(data): a preconfigured *flags.Parser for finer control.
//   - LoadDotenv(files...): seed env from .env files (default ".env"); missing
//     files are ignored and existing env vars are never overwritten.
//   - IsHelp(err): reports the benign "help requested" error.
//   - Reset(keys...): unset environment variables.
package config
