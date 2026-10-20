package chx_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/assanoff/skit/chx"
)

// event is a row of the events table the migrations create.
type event struct {
	ID   string    `ch:"id"`
	Name string    `ch:"name"`
	At   time.Time `ch:"at"`
}

// Migrate once at startup, open the client, write a batch and read it back.
// The example needs a running ClickHouse, so go test builds it but does not
// run it.
func ExampleOpen() {
	ctx := context.Background()
	cfg := chx.Config{
		Addrs:    []string{"localhost:9000"},
		Database: "analytics",
		Username: "default",
	}

	if err := chx.Migrate(ctx, cfg, os.DirFS("migrations")); err != nil {
		log.Fatal(err)
	}
	cl, err := chx.Open(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}

	batch, err := cl.PrepareBatch(ctx, "INSERT INTO events")
	if err != nil {
		log.Fatal(err)
	}
	if err := batch.Append("e-1", "signup", time.Now()); err != nil {
		log.Fatal(err)
	}
	if err := batch.Send(); err != nil {
		log.Fatal(err)
	}

	var events []event
	err = cl.Select(ctx, &events, "SELECT id, name, at FROM events")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(events))

	if err := cl.Close(); err != nil {
		log.Fatal(err)
	}
}
