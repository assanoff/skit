package auditlog_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/audittest"
	"github.com/assanoff/skit/worker"
)

// widget is the model the examples audit.
type widget struct {
	Name  string `json:"name"`
	Price int    `json:"price"`
}

// Create stores the snapshot of the model as its next version, unless it
// equals the latest one: then nothing is stored and the version is 0. The
// history lists the versions in order, the changes the fields each version
// changed.
func Example() {
	ctx := context.Background()
	core := auditlog.NewCore(nil, audittest.NewStore())

	for _, w := range []widget{{"gear", 10}, {"gear", 10}, {"gear", 12}, {"big gear", 12}} {
		al, err := core.Create(ctx, auditlog.NewAuditLog{
			ModelType: "widget", ModelID: "w-1", CreatedBy: "ann", Payload: w,
		})
		fmt.Println("create:", al.Version, err)
	}

	hist, err := core.QueryHistoryByModelID(ctx, "widget", "w-1")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, al := range hist {
		fmt.Println(al.Version, al.CreatedBy, string(bytes.TrimSpace(al.Payload)))
	}

	recs, err := core.QueryDiffAllVersionByModelID(ctx, "widget", "w-1")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, rec := range recs {
		for _, f := range rec.ChangedFields {
			fmt.Printf("v%d %s: %v -> %v\n", rec.Version, f.Key, f.OldValue, f.NewValue)
		}
	}
	// Output:
	// create: 1 <nil>
	// create: 0 <nil>
	// create: 2 <nil>
	// create: 3 <nil>
	// 1 ann {"name":"gear","price":10}
	// 2 ann {"name":"gear","price":12}
	// 3 ann {"name":"big gear","price":12}
	// v1 price: <nil> -> 10
	// v2 price: 10 -> 12
	// v3 name: gear -> big gear
}

// CompactBatch thins the history of the models over a threshold of versions
// and keeps the first and the last version of each. Run it from a worker.Loop,
// a command or an admin endpoint; loop until Models is 0 to drain a backlog.
func ExampleCore_CompactBatch() {
	ctx := context.Background()
	core := auditlog.NewCore(nil, audittest.NewStore())
	for price := range 10 {
		core.Record(ctx, auditlog.NewAuditLog{
			ModelType: "widget", ModelID: "w-1", Payload: widget{"gear", 10 + price},
		})
	}

	res, err := core.CompactBatch(ctx, auditlog.CompactBatchOptions{
		Threshold: 5, // the models with more than 5 versions
		Limit:     50,
		Compact:   auditlog.CompactOptions{Factor: 3, KeepRecent: 2},
	})
	fmt.Println(res.Models, res.Deleted, err)

	hist, err := core.QueryHistoryByModelID(ctx, "widget", "w-1")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, al := range hist {
		fmt.Print(al.Version, " ")
	}
	fmt.Println()
	// Output:
	// 1 5 <nil>
	// 1 4 7 9 10
}

// WithAutoCompact compacts the model Create has just written once every N
// versions, so its history stays bounded without a scheduled run.
func ExampleWithAutoCompact() {
	ctx := context.Background()
	core := auditlog.NewCore(nil, audittest.NewStore(),
		auditlog.WithAutoCompact(5, auditlog.CompactOptions{Factor: 2}))
	for price := range 10 {
		core.Record(ctx, auditlog.NewAuditLog{
			ModelType: "widget", ModelID: "w-1", Payload: widget{"gear", 10 + price},
		})
	}

	hist, err := core.QueryHistoryByModelID(ctx, "widget", "w-1")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, al := range hist {
		fmt.Print(al.Version, " ")
	}
	fmt.Println()
	// Output:
	// 1 5 7 9 10
}

// The AsyncRecorder writes on its workers, off the path of the request:
// Record only enqueues, and the entries of one model keep their order. In a
// worker.Group, Start writes what is buffered before it returns.
func ExampleNewAsyncRecorder() {
	ctx, cancel := context.WithCancel(context.Background())
	core := auditlog.NewCore(nil, audittest.NewStore())
	rec := auditlog.NewAsyncRecorder(core, auditlog.AsyncConfig{Workers: 2})

	g := worker.NewGroup(slog.New(slog.DiscardHandler), worker.GroupConfig{})
	g.Add(rec)
	done := make(chan error, 1)
	go func() { done <- g.Run(ctx) }()

	for _, name := range []string{"gear", "big gear", "huge gear"} {
		rec.Record(ctx, auditlog.NewAuditLog{
			ModelType: "widget", ModelID: "w-1", CreatedBy: "ann",
			Payload: widget{name, 10},
		})
	}
	cancel() // shutdown: the recorder writes its buffer and stops
	fmt.Println("group:", <-done)

	hist, err := core.QueryHistoryByModelID(context.Background(), "widget", "w-1")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, al := range hist {
		fmt.Println(al.Version, string(bytes.TrimSpace(al.Payload)))
	}
	// Output:
	// group: <nil>
	// 1 {"name":"gear","price":10}
	// 2 {"name":"big gear","price":10}
	// 3 {"name":"huge gear","price":10}
}
