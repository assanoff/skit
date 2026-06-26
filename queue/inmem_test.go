package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInMemScheduleDedup(t *testing.T) {
	q := NewInMem(time.Minute)
	ctx := context.Background()

	ins, err := q.Schedule(ctx, ScheduleParams{Name: "job-1", Kind: "x"})
	if err != nil || !ins {
		t.Fatalf("first schedule: inserted=%v err=%v", ins, err)
	}
	ins, err = q.Schedule(ctx, ScheduleParams{Name: "job-1", Kind: "x"})
	if err != nil || ins {
		t.Fatalf("duplicate schedule: inserted=%v err=%v (want false,nil)", ins, err)
	}

	// Empty name is always unique.
	ins, _ = q.Schedule(ctx, ScheduleParams{Kind: "x"})
	ins2, _ := q.Schedule(ctx, ScheduleParams{Kind: "x"})
	if !ins || !ins2 {
		t.Fatalf("empty-name schedules should both insert, got %v %v", ins, ins2)
	}
}

func TestInMemClaimLeasesExclusively(t *testing.T) {
	q := NewInMem(time.Minute)
	ctx := context.Background()

	for range 3 {
		if _, err := q.Schedule(ctx, ScheduleParams{Kind: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC() // after scheduling, so the tasks are ready

	first, err := q.Claim(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 {
		t.Fatalf("expected to claim 3, got %d", len(first))
	}
	for _, task := range first {
		if task.LeaseID == "" {
			t.Error("claimed task missing lease id")
		}
		if task.Attempts != 1 {
			t.Errorf("expected attempts=1, got %d", task.Attempts)
		}
	}

	// A second claim before the lease expires sees nothing.
	second, err := q.Claim(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Errorf("expected no tasks while leased, got %d", len(second))
	}
}

func TestInMemLeaseExpiryAllowsReclaim(t *testing.T) {
	q := NewInMem(time.Minute)
	ctx := context.Background()

	q.Schedule(ctx, ScheduleParams{Name: "j", Kind: "x"})
	now := time.Now().UTC()
	if _, err := q.Claim(ctx, now, 1); err != nil {
		t.Fatal(err)
	}

	// Still leased now; reclaimable after the lease timeout.
	if got, _ := q.Claim(ctx, now.Add(30*time.Second), 1); len(got) != 0 {
		t.Fatalf("expected no reclaim before expiry, got %d", len(got))
	}
	reclaimed, _ := q.Claim(ctx, now.Add(90*time.Second), 1)
	if len(reclaimed) != 1 {
		t.Fatalf("expected reclaim after expiry, got %d", len(reclaimed))
	}
	if reclaimed[0].Attempts != 2 {
		t.Errorf("expected attempts=2 after reclaim, got %d", reclaimed[0].Attempts)
	}
}

func TestInMemMarkDone(t *testing.T) {
	q := NewInMem(time.Minute)
	ctx := context.Background()

	q.Schedule(ctx, ScheduleParams{Name: "j", Kind: "x"})
	now := time.Now().UTC()
	claimed, _ := q.Claim(ctx, now, 1)
	if err := q.MarkDone(ctx, claimed[0], now); err != nil {
		t.Fatalf("mark done: %v", err)
	}
	if q.Len() != 0 {
		t.Errorf("expected empty queue after done, got %d", q.Len())
	}

	// Marking with a stale lease is a lease-lost no-op.
	if err := q.MarkDone(ctx, claimed[0], now); !errors.Is(err, ErrLeaseLost) {
		t.Errorf("expected ErrLeaseLost on stale mark, got %v", err)
	}
}

func TestInMemMarkFailedRetryable(t *testing.T) {
	q := NewInMem(time.Minute)
	ctx := context.Background()

	q.Schedule(ctx, ScheduleParams{Name: "j", Kind: "x"})
	now := time.Now().UTC()
	claimed, _ := q.Claim(ctx, now, 1)
	if err := q.MarkFailed(ctx, claimed[0], "boom", false, now); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	// Released: immediately claimable again.
	again, _ := q.Claim(ctx, now, 1)
	if len(again) != 1 {
		t.Fatalf("expected retryable task to be reclaimable, got %d", len(again))
	}
	if again[0].LastError != "boom" {
		t.Errorf("expected last_error preserved, got %q", again[0].LastError)
	}
}

func TestInMemMarkFailedTerminal(t *testing.T) {
	q := NewInMem(time.Minute)
	ctx := context.Background()

	q.Schedule(ctx, ScheduleParams{Name: "j", Kind: "x"})
	now := time.Now().UTC()
	claimed, _ := q.Claim(ctx, now, 1)
	if err := q.MarkFailed(ctx, claimed[0], "fatal", true, now); err != nil {
		t.Fatalf("mark failed terminal: %v", err)
	}

	// Dead-lettered: row remains but is never claimed again.
	if q.Len() != 1 {
		t.Errorf("expected dead-letter row to remain, len=%d", q.Len())
	}
	if got, _ := q.Claim(ctx, now.Add(time.Hour), 10); len(got) != 0 {
		t.Errorf("dead-lettered task must not be reclaimed, got %d", len(got))
	}
}

func TestInMemDelayedTaskNotReady(t *testing.T) {
	q := NewInMem(time.Minute)
	ctx := context.Background()
	now := time.Now().UTC()

	q.Schedule(ctx, ScheduleParams{Name: "later", Kind: "x", Delay: time.Hour})
	if got, _ := q.Claim(ctx, now, 10); len(got) != 0 {
		t.Fatalf("delayed task should not be ready, got %d", len(got))
	}
	if got, _ := q.Claim(ctx, now.Add(2*time.Hour), 10); len(got) != 1 {
		t.Errorf("delayed task should be ready after delay, got %d", len(got))
	}
}
