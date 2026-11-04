# outbox-dbx: a write and its event in one transaction

An orders service whose write and event commit together in Postgres, while
the event reaches RabbitMQ afterwards and survives a broker that is down. The
store opens the transaction; the event is a task of the queue of events, which
`outbox.Handler` publishes; the transport picks the key of the message.

| file | what it holds |
|---|---|
| [`orderapp/store.go`](orderapp/store.go) | the contract of the domain: `Repo`, `Scheduler` and `Store` with `WithinTran` and `WithinTranTasks` |
| [`orderapp/orderapp.go`](orderapp/orderapp.go) | the business logic: `Create` writes the order and schedules `OrderCreated` in one transaction, `Pay` makes two writes without an event, `Get` reads without a transaction. It imports no package of skit |
| [`orderdb/store.go`](orderdb/store.go) | the store on `dbx`: over the pool it opens a transaction, over a `*dbx.Tx` it joins one; `WithTasks` turns the tasks on |
| [`main.go`](main.go) | the assembly: `newDeps` opens the pool, the queue, its `Registry` with the route of `OrderCreated` and the publisher; `Tasks` binds the scheduler to a transaction; `addEventWorkers` runs the publishing |

The domain declares what it needs and no more: `Repo` has no `WithinTran*`, so
the callback gets only `r` and `s`; `*dbx.Tx` has no `Begin`, `Commit` or
`Rollback`, so a store over a transaction cannot open its own — `WithinTran`
on it joins the transaction of the caller. Nothing travels in `ctx`: neither
the transaction nor the connection nor a mark of the transaction.

## Run it

Start Postgres and RabbitMQ with its management API:

```sh
docker run -d --name skit-pg -e POSTGRES_PASSWORD=postgres -p 5432:5432 \
  postgres:17-alpine
docker run -d --name skit-rabbit -p 5672:5672 -p 15672:15672 \
  rabbitmq:3.13-management-alpine
```

Declare the exchange `orders` and a queue to watch it; a publisher declares no
exchange:

```sh
api=localhost:15672/api
curl -u guest:guest -H 'content-type: application/json' \
  -X PUT $api/exchanges/%2F/orders -d '{"type":"topic","durable":true}'
curl -u guest:guest -H 'content-type: application/json' \
  -X PUT $api/queues/%2F/orders.audit -d '{"durable":true}'
curl -u guest:guest -H 'content-type: application/json' \
  -X POST $api/bindings/%2F/e/orders/q/orders.audit -d '{"routing_key":"#"}'
```

Run the service. It creates the tables, creates one order, pays it, reads it
back, then runs the workers of the queue `events` until Ctrl+C:

```sh
go run ./examples/outbox-dbx
# level=INFO msg="order paid" order_id=4CR74O3PEZ6PKODN4A4DRYMEHB status=paid
```

The workers publish `order.created`. Read it:

```sh
curl -u guest:guest -H 'content-type: application/json' \
  -X POST $api/queues/%2F/orders.audit/get \
  -d '{"count":10,"ackmode":"ack_requeue_false","encoding":"auto"}'
```

The id of the CloudEvent is the id the event got when it was scheduled, so a
consumer can drop a repeated delivery by it.

Another host or port: `DB_HOST`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`,
`BROKER_HOST`, `BROKER_PORT`, `BROKER_USER`, `BROKER_PASSWORD` or the flags of
`go run ./examples/outbox-dbx --help`. Clean up with
`docker rm -f skit-pg skit-rabbit`.

## When the broker refuses

Run the service before the exchange `orders` exists. The order commits
anyway; its event stays in the table `queue_tasks`:

```sh
docker exec skit-pg psql -U postgres \
  -c 'SELECT kind, attempts, last_error FROM queue_tasks'
#      kind      | attempts |                 last_error
# ---------------+----------+---------------------------------------------
#  order.created |        3 | rabbitmq publish: broker nacked the message
```

`outbox.Handler` returns `queue.Halt`: one failed publish per poll, the rest of
the batch goes back untouched, and the task is retried with a pause of up to 5
minutes and no limit of attempts. Declare the exchange and the event goes out
on the next attempt. An event goes to the dead letters only when the publisher
marks its error `broker.Permanent` (the envelope cannot be built, the message
is over the limit of the broker) or its envelope does not unpack. With
`queue.WithMetrics` in `addEventWorkers`, alert on
`skit_queue_pending{queue="events"}` and `skit_queue_dead{queue="events"}`.

## Rules inside a transaction

- **Do not call `Core` inside the callback.** Types do not forbid it: the store
  of `Core` in `fn` opens a second transaction on the pool, which waits for the
  locks of the first until the deadline of `ctx`. Inside the callback use only
  `r` and `s`; logic inside a transaction is an unexported function over them,
  and the exported method wraps it.
- **`fn` may run again** — on a deadlock or a serialization failure: keep no
  state between the runs, and assign a result from the closure instead of
  appending to it.
- **Joining a transaction is not a savepoint:** always return the error of the
  joined call from `fn`.

The domain tests without a database: moq mocks of `Store` and `Repo` call
`return fn(repoMock)` or `return fn(repoMock, schedMock)`, and `schedMock`
collects the scheduled values.

## Several stores and the logic of another domain

When one operation writes through several stores, or calls the business logic
of another domain in the same transaction, the domain declares what a
transaction shows with its own types — a `Unit` of `Repo`, `Scheduler` and,
say, the `Stock` of the warehouse — and a `Tran` that runs `fn(u Unit)`. The
assembly builds the `Unit` from the transaction on every attempt, and
`dbx.Tran[U]` satisfies the `Tran` of the domain without an adapter — the
[`ExampleNewTran`](../../dbx/example_test.go) of `dbx`. The warehouse calls its
own `WithinTranTasks`, and its store over the transaction joins the one of the
order: its rules, its tasks and the rollback are shared, and its code does not
change.

## Without a broker, or with Kafka

The broker is off: route the event with `queue.Drop()` instead of
`queue.WithEncoder(outbox.JSON)` and do not call `addEventWorkers`. The write
goes through, the event goes nowhere, and the domain does not branch. A
migration command or a test that needs the pool and the queue without the
publishing calls `newDeps` alone.

Kafka instead of RabbitMQ: `newDeps` builds `kafka.NewPublisher`; `queue`,
`outbox`, `dbx`, the store, `addEventWorkers` and the domain stay as they are.
