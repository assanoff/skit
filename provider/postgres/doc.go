// Package postgres provides a ready Postgres connection pool for dim: Resource
// opens it with dbx.Open, checks it is reachable and closes it on shutdown.
//
// # Usage
//
//	// in an app initializer (app.Initializer[Deps]):
//	c.DB, cleanup = dim.NewResource("Store", postgres.Resource(dbx.Config{
//	    User:           opts.DB.User,
//	    Password:       opts.DB.Password,
//	    Host:           opts.DB.Host,
//	    Name:           opts.DB.Name,
//	    Metrics:        c.Metrics,
//	    TracerProvider: c.TracerProvider,
//	}))
//	return cleanup, nil
//
// The pool is opened on first use of c.DB. dbx.Config is passed through
// unchanged: see the dbx package for its fields.
package postgres
