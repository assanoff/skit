// Package dedup runs an operation once per key and hands the result of its
// first success to the repeats, the way a cache hands out a value:
//
//	id, err := k.Do(ctx, "refund/"+orderID, func(ctx context.Context) (string, error) {
//	    return payments.Refund(ctx, orderID)
//	})
//
// The package is universal: it knows no other package of skit. The service
// wraps its own HTTP handlers, consumers, jobs and functions in Do; the
// recipes below show how. The records live in memory or in Redis, and the
// service names the store explicitly.
//
// # Outcomes
//
// Do looks the key up first:
//
//   - free or expired: fn runs under the lease as its deadline. A success is
//     encoded with encoding/json and stored for the TTL. An error of fn is
//     returned as it is, with the value of fn, and frees the key, as a panic
//     of fn does, so a repeat runs fn again.
//   - done: Do decodes the stored success and returns it; fn does not run.
//   - running: Do returns ErrInProgress at once, without waiting. The caller
//     decides: an HTTP handler answers 409, a consumer requeues, a job fails
//     and retries, a scheduled run skips.
//   - store down: Do returns an error wrapping ErrUnavailable; fn does not
//     run.
//
// When the success cannot be stored — the store fails, or fn outlived the
// lease and lost the key — Do still returns it and logs a warning; a repeat may
// run fn again. Do never runs inside a transaction: the record in memory or
// Redis does not roll back with it (see the recipe "An effect in your own
// database").
//
// # Keys
//
// A key names one operation and is the same on every repeat of it: the
// Idempotency-Key of a request, the id of a message, a slot of a schedule.
// Put the owner in front of it ("user-7/" + header) so that callers never share
// keys, and join the parts with a separator none of them contains. The stored
// success must decode into the T of every repeat: an incompatible change of T
// goes under a new prefix ("refund.v2/" + id).
//
// # Lease and TTL
//
// WithLease (default 1m) is the deadline of fn and the life of the record of a
// running call: a crashed holder blocks its key no longer than that. Long work
// takes a longer lease. Each call holds its key with a token of its own, so a
// call whose lease ended neither overwrites nor frees the record of the next.
// WithTTL (default 24h) is how long a stored success is handed to repeats,
// counted from its completion.
//
// # Stores
//
// New takes the store from the scheme of a uri, so the config of the service
// names it: mem:// or redis://host:port/db (rediss:// for TLS, unix:// for a
// socket, the query parameters of redis.ParseURL). An empty or unknown uri is
// an error: there is no store by default. Close closes the Redis client New
// opened. NewMemory and NewRedis build the same stores in code; NewRedis takes
// a client the caller owns and closes.
//
// The memory store keeps the records in the process: tests and a service of
// one replica. Redis shares them between replicas: a record is the string key
// "dedup:"+key, one script per operation. A new key costs two round trips, a
// repeat one. Run Redis with maxmemory-policy noeviction or give dedup an
// instance of its own: an evicted record runs fn again. The memory it takes is
// about calls per second × TTL × the size of a result.
//
// # Recipes
//
// An HTTP handler: the key is the Idempotency-Key of the request behind the
// subject of the caller, and T is the response — status, content type and
// body — so a retry gets the same answer (see the example of Keeper.Do). An
// error response is an error of fn: it is not stored, and a retry runs the
// handler again. A final error that the retry must see again, such as 422, is
// a value of fn. ErrInProgress answers 409 with Retry-After, ErrUnavailable
// 503. To reject another request under the same key, put a hash of its body in
// T and compare it after Do.
//
// A message of a broker: the key is the group of the subscription, the source
// and the id of the message, so the replicas of a group share the records and
// different groups do not. A verdict other than Ack is an error of fn: the key
// is freed and the redelivery runs the handler again. ErrInProgress and
// ErrUnavailable requeue the message. A stable message id comes from the queue
// of events; a producer that publishes directly sets a deterministic one.
//
// A schedule on every replica: the key is the name of the job and its slot,
// name + "/" + time.Now().UTC().Truncate(time.Minute). A schedule of five
// fields fires at second zero, so the replicas count the same slot; one runs
// the job, the others get ErrInProgress or the stored success and skip. An
// error frees the slot for a replica that fires later. @every does not fit:
// the replicas fire at different moments.
//
// At most once, for an effect where a loss is better than a duplicate (SMS,
// email, push): mark the key with an empty fn first, and run the effect only
// if this call set the mark. A crash or an error after the mark loses the
// effect:
//
//	first := false
//	_, err := k.Do(ctx, "sms/"+id, func(context.Context) (struct{}, error) {
//	    first = true
//	    return struct{}{}, nil
//	})
//	if err != nil || !first {
//	    return err
//	}
//	return sms.Send(ctx, phone, text)
//
// An effect in your own database needs no dedup: exactly once comes from a
// mark that commits with the data, in the same transaction — a unique index of
// the domain or an inbox table of your own with INSERT ... ON CONFLICT DO
// NOTHING. Zero rows means done; the repeat reads what the first one made. A
// concurrent duplicate waits on the index for the first transaction.
//
// A task of a queue lives longer than a lease: when a worker crashes, the
// record of dedup expires before the queue hands the task out again. Keep its
// effect in your own database with the id of the task as the key, as above,
// and pass a key down to an external call.
//
// A key down: an external system that takes an idempotency key closes the gap
// between an effect and its record. Derive the key from what is stable between
// repeats — the id of a message, of a task, of the domain — and send
// hex(sha256(...)) of it: a fixed length that does not expose the data.
package dedup
