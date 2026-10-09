# broker + RabbitMQ: импорты и использование

> Показано **целевое** состояние после ломающего релиза (`research-mods.md` §3): логгер —
> `*slog.Logger`. Сегодня адаптеры принимают `*logger.Logger`.

`broker` — интерфейсы, CloudEvents, `Guard`/`Process`; `broker/rabbitmq` и `broker/kafka` —
адаптеры. Друг друга адаптеры не импортируют.

## 1. Импорты

```go
import (
	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/rabbitmq"
	"github.com/assanoff/skit/worker"
)
```

## 2. Домен знает только `broker`

```go
package orderapp

import (
	"context"

	"github.com/assanoff/skit/broker"
)

type App struct {
	Pub broker.Publisher // интерфейс, транспорт неизвестен
}

func (a *App) Created(ctx context.Context, id string, body []byte) error {
	return a.Pub.Publish(ctx, broker.Message{
		Type:            "order.created",
		DataContentType: "application/json",
		Data:            body,
		Topic:           "orders",
		Key:             id,
	})
}

// HandlePaid — обработчик входящих событий (broker.Handler).
func (a *App) HandlePaid(ctx context.Context, m broker.Message) broker.Action {
	if err := a.markPaid(ctx, m.Data); err != nil {
		return broker.Requeue
	}
	return broker.Ack
}
```

## 3. Сборка — единственное место, где появляется RabbitMQ

Сборка разделена, как в шаблонах `skit new` (`server.New` + `initWorkers`): `deps.New`
только создаёт и возвращает зависимости, ничего не запуская; регистрацию consumer'а в
группе делает отдельная функция с говорящим именем.

```go
package deps

import (
	"log/slog"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/rabbitmq"
)

// Deps — собранные зависимости, без запуска чего-либо.
type Deps struct {
	Conn      *rabbitmq.Conn
	Publisher broker.Publisher // *rabbitmq.Publisher за интерфейсом broker
}

func New(log *slog.Logger, cfg Config) (*Deps, error) {
	conn, err := rabbitmq.Dial(log, cfg.Rabbit)
	if err != nil {
		return nil, err
	}

	pub, err := rabbitmq.NewPublisher(conn, "orders", log)
	if err != nil {
		return nil, err
	}

	return &Deps{Conn: conn, Publisher: pub}, nil
}
```

```go
// addConsumers регистрирует consumer'ов входящих событий.
func addConsumers(log *slog.Logger, g *worker.Group, d *deps.Deps,
	app *orderapp.App) error {
	cons, err := rabbitmq.NewConsumer(d.Conn, log, rabbitmq.ConsumerConfig{
		Queue:    "orders.payments",
		Exchange: "payments",
	}, app.HandlePaid) // broker.Handler
	if err != nil {
		return err
	}

	g.Add(cons) // worker.Runnable
	return nil
}
```

В `main` видно, что собирается и что запускается:

```go
d, err := deps.New(log, cfg)
if err != nil {
	return err
}
app := &orderapp.App{Pub: d.Publisher}
if err := addConsumers(log, g, d, app); err != nil {
	return err
}
```

## 4. Смена транспорта на Kafka

Меняется только сборка — `Deps` вместо `Conn` держит `kafka.Config`, а `deps.New` и
`addConsumers` создают kafka-адаптеры:

```go
pub, err := kafka.NewPublisher(kafka.Config{Brokers: cfg.Brokers}, "orders", log)
cons, err := kafka.NewConsumer(kafka.ConsumerConfig{ /* … */ }, log, app.HandlePaid)
```

Домен не меняется — контракт (`broker.Publisher`, `broker.Handler`) живёт в `broker`.
