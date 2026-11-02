package dedup_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/assanoff/skit/dedup"
)

// The second call with the same key gets the stored result; fn runs once.
func ExampleKeeper_Do() {
	k := dedup.NewMemory()
	ctx := context.Background()

	calls := 0
	refund := func(context.Context) (string, error) {
		calls++
		return "refund-1", nil
	}
	for range 2 {
		id, err := k.Do(ctx, "refund/42", refund)
		fmt.Println(id, err)
	}
	fmt.Println("calls:", calls)
	// Output:
	// refund-1 <nil>
	// refund-1 <nil>
	// calls: 1
}

// response is what an idempotent HTTP handler stores: a retry gets it back.
type response struct {
	Status int    `json:"status"`
	Type   string `json:"type"`
	Body   []byte `json:"body"`
}

// errServerError marks a response that is not stored: a retry runs the
// handler again.
var errServerError = errors.New("server error")

// idempotent runs next once per Idempotency-Key of the caller.
func idempotent(k *dedup.Keeper, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}
		subject := r.Header.Get("X-Subject") // the authenticated caller
		resp, err := k.Do(r.Context(), subject+"/"+key, func(ctx context.Context) (response, error) {
			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, r.WithContext(ctx))
			resp := response{
				Status: rec.Code,
				Type:   rec.Header().Get("Content-Type"),
				Body:   rec.Body.Bytes(),
			}
			if rec.Code >= http.StatusInternalServerError {
				return resp, errServerError
			}
			return resp, nil
		})
		switch {
		case errors.Is(err, dedup.ErrInProgress):
			w.Header().Set("Retry-After", "1")
			http.Error(w, "the request is in progress", http.StatusConflict)
			return
		case errors.Is(err, dedup.ErrUnavailable):
			http.Error(w, "try again later", http.StatusServiceUnavailable)
			return
		case err != nil && !errors.Is(err, errServerError):
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", resp.Type)
		w.WriteHeader(resp.Status)
		_, _ = w.Write(resp.Body)
	})
}

// A retry of a POST with the same Idempotency-Key gets the same status and
// body, and the order is created once.
func ExampleKeeper_Do_http() {
	k := dedup.NewMemory()
	orders := 0
	create := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		orders++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"id":%d}`, orders)
	})
	h := idempotent(k, create)

	for range 2 {
		r := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader("{}"))
		r.Header.Set("X-Subject", "user-7")
		r.Header.Set("Idempotency-Key", "8e0f")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		fmt.Println(w.Code, w.Body.String())
	}
	fmt.Println("orders:", orders)
	// Output:
	// 201 {"id":1}
	// 201 {"id":1}
	// orders: 1
}

// An effect where a loss is better than a duplicate: the mark goes first, and
// only the call that set it sends.
func ExampleKeeper_Do_atMostOnce() {
	k := dedup.NewMemory()
	ctx := context.Background()

	send := func(id, text string) error {
		first := false
		_, err := k.Do(ctx, "sms/"+id, func(context.Context) (struct{}, error) {
			first = true
			return struct{}{}, nil
		})
		if err != nil || !first {
			return err
		}
		fmt.Println("sent:", text)
		return nil
	}
	fmt.Println(send("n-1", "your code is 1234"))
	fmt.Println(send("n-1", "your code is 1234"))
	// Output:
	// sent: your code is 1234
	// <nil>
	// <nil>
}

// Two replicas fire on the same slot of a schedule: one runs the job.
func ExampleKeeper_Do_cron() {
	k := dedup.NewMemory() // NewRedis in production: the replicas share it
	ctx := context.Background()

	// time.Now().UTC().Truncate(time.Minute) on every replica.
	slot := time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)
	key := "daily-report/" + slot.Format(time.RFC3339)
	for _, replica := range []string{"a", "b"} {
		_, err := k.Do(ctx, key, func(context.Context) (struct{}, error) {
			fmt.Println("replica", replica, "runs the report")
			return struct{}{}, nil
		})
		if errors.Is(err, dedup.ErrInProgress) {
			continue // another replica runs it now
		}
		if err != nil {
			fmt.Println(err)
		}
	}
	// Output:
	// replica a runs the report
}

// The service names its store in its config: mem:// for one replica,
// redis://host:port/db for many.
func ExampleNew() {
	k, err := dedup.New("mem://", slog.Default(), dedup.WithTTL(time.Hour))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = k.Close() }()

	v, err := k.Do(context.Background(), "greeting/1", func(context.Context) (string, error) {
		return "hello", nil
	})
	fmt.Println(v, err)
	// Output: hello <nil>
}

// The replicas of a service share their records through Redis.
func ExampleNewRedis() {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer func() { _ = client.Close() }()

	k := dedup.NewRedis(client, slog.Default(),
		dedup.WithTTL(time.Hour), dedup.WithLease(30*time.Second))
	_ = k
}
