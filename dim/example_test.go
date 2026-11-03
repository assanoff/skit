package dim_test

import (
	"context"
	"fmt"

	"github.com/assanoff/skit/dim"
)

type pool struct{ dsn string }

// NewResource builds the value on the first call and caches it; its cleanup
// runs the teardown the factory returned. A resource nobody asked for is
// never built, and its cleanup does nothing.
func Example() {
	ctx := context.Background()
	db, closeDB := dim.NewResource("DB",
		func(context.Context) (*pool, dim.CleanupFunc, error) {
			fmt.Println("connect")
			p := &pool{dsn: "postgres://localhost/app"}
			return p, func() error {
				fmt.Println("disconnect")
				return nil
			}, nil
		})
	_, closeCache := dim.NewResource("Cache",
		func(context.Context) (*pool, dim.CleanupFunc, error) {
			fmt.Println("connect cache")
			return &pool{}, nil, nil
		})

	first, second := db(ctx), db(ctx)
	fmt.Println(first.dsn, first == second)
	fmt.Println(closeCache(), closeDB())
	// Output:
	// connect
	// postgres://localhost/app true
	// disconnect
	// <nil> <nil>
}

// Once memoizes a factory without a teardown; the factory runs on the first
// call, not when the provider is declared.
func ExampleOnce() {
	ctx := context.Background()
	limit := dim.Once(func(context.Context) (int, error) {
		fmt.Println("load limit")
		return 100, nil
	})
	fmt.Println("declared")

	fmt.Println(limit(ctx), limit(ctx))
	// Output:
	// declared
	// load limit
	// 100 100
}
