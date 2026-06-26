package eventbus

import (
	"context"
	"errors"
	"sync"
	"testing"
)

const (
	domUser       = "user"
	actionDeleted = "deleted"
)

type deletedParams struct {
	UserID string `json:"user_id"`
}

func TestCallDispatchesInOrder(t *testing.T) {
	d := New(nil)
	var order []int
	d.Register(domUser, actionDeleted, func(context.Context, Data) error { order = append(order, 1); return nil })
	d.Register(domUser, actionDeleted, func(context.Context, Data) error { order = append(order, 2); return nil })

	if err := d.Call(context.Background(), MustData(domUser, actionDeleted, nil)); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("handlers ran out of order: %v", order)
	}
}

func TestCallAbortsOnFirstError(t *testing.T) {
	d := New(nil)
	boom := errors.New("boom")
	ran := 0
	d.Register(domUser, actionDeleted, func(context.Context, Data) error { ran++; return boom })
	d.Register(domUser, actionDeleted, func(context.Context, Data) error { ran++; return nil })

	err := d.Call(context.Background(), MustData(domUser, actionDeleted, nil))
	if !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	if ran != 1 {
		t.Fatalf("Call should stop after the first error, ran=%d", ran)
	}
}

func TestPublishRunsAllAndJoinsErrors(t *testing.T) {
	d := New(nil)
	err1 := errors.New("e1")
	err2 := errors.New("e2")
	ran := 0
	d.Register(domUser, actionDeleted, func(context.Context, Data) error { ran++; return err1 })
	d.Register(domUser, actionDeleted, func(context.Context, Data) error { ran++; return nil })
	d.Register(domUser, actionDeleted, func(context.Context, Data) error { ran++; return err2 })

	err := d.Publish(context.Background(), MustData(domUser, actionDeleted, nil))
	if ran != 3 {
		t.Fatalf("Publish should run every handler, ran=%d", ran)
	}
	if !errors.Is(err, err1) || !errors.Is(err, err2) {
		t.Fatalf("Publish should join all errors, got %v", err)
	}
}

func TestPublishNilWhenAllSucceed(t *testing.T) {
	d := New(nil)
	d.Register(domUser, actionDeleted, func(context.Context, Data) error { return nil })
	if err := d.Publish(context.Background(), MustData(domUser, actionDeleted, nil)); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestNoHandlersIsNoOp(t *testing.T) {
	d := New(nil)
	if err := d.Call(context.Background(), MustData("ghost", "none", nil)); err != nil {
		t.Fatalf("Call no-op: %v", err)
	}
	if err := d.Publish(context.Background(), MustData("ghost", "none", nil)); err != nil {
		t.Fatalf("Publish no-op: %v", err)
	}
}

func TestDataRoundTrip(t *testing.T) {
	d := New(nil)
	var got deletedParams
	d.Register(domUser, actionDeleted, func(_ context.Context, data Data) error {
		p, err := Decode[deletedParams](data)
		got = p
		return err
	})

	if err := d.Call(context.Background(), MustData(domUser, actionDeleted, deletedParams{UserID: "u-1"})); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got.UserID != "u-1" {
		t.Fatalf("decoded params = %+v, want UserID u-1", got)
	}
}

func TestDecodeEmptyParamsYieldsZeroValue(t *testing.T) {
	p, err := Decode[deletedParams](MustData(domUser, actionDeleted, nil))
	if err != nil {
		t.Fatalf("Decode nil params: %v", err)
	}
	if p != (deletedParams{}) {
		t.Fatalf("want zero value, got %+v", p)
	}
}

func TestNewDataMarshalError(t *testing.T) {
	if _, err := NewData(domUser, actionDeleted, make(chan int)); err == nil {
		t.Fatal("expected a marshal error for an unsupported type")
	}
}

// TestConcurrentRegisterAndCall exercises the mutex: registrations and
// dispatches race under -race to prove the bus is safe for concurrent use.
func TestConcurrentRegisterAndCall(t *testing.T) {
	d := New(nil)
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			d.Register(domUser, actionDeleted, func(context.Context, Data) error { return nil })
		}()
		go func() { defer wg.Done(); _ = d.Call(context.Background(), MustData(domUser, actionDeleted, nil)) }()
	}
	wg.Wait()
}
