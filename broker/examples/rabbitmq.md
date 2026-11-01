# broker + RabbitMQ: импорты и использование

`broker` — интерфейсы и CloudEvents; `broker/rabbitmq` и `broker/kafka` — адаптеры:
publisher и консьюмер, который сам пишет спан и метрики. Друг друга адаптеры не импортируют.

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
		Type:        "order.created",
		ContentType: "application/json",
		Data:        body,
		Topic:       "orders",
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
	conn, err := rabbitmq.Dial(cfg.Rabbit)
	if err != nil {
		return nil, err
	}

	pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "orders"})
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
	cons, err := rabbitmq.NewConsumer(log, d.Conn, rabbitmq.ConsumerConfig{
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
pub, err := kafka.NewPublisher(kafka.Config{Brokers: cfg.Brokers}, kafka.PublisherConfig{Source: "orders"})
cons, err := kafka.NewConsumer(log, kafka.Config{Brokers: cfg.Brokers},
	kafka.ConsumerConfig{Topic: "payments", Group: "orders"}, app.HandlePaid)
```

Домен не меняется — контракт (`broker.Publisher`, `broker.Handler`) живёт в `broker`.
Ключ тоже задаёт сборка: RabbitMQ по умолчанию маршрутизирует по `Type`
(`rabbitmq.WithRoutingKey` — своя маршрутизация), Kafka пишет записи без ключа, а порядок
событий одного заказа даёт `kafka.WithPartitionKey`, который берёт id заказа из `Data`
(`broker/kafka/doc.go`).

## 5. Свой конверт

По умолчанию сообщение уходит конвертом `broker.CloudEvents` (structured JSON). Другой формат —
binary mode CloudEvents, конверт старой системы, голый payload с атрибутами в заголовках —
это `broker.Envelope` клиента. Его получают через `WithEnvelope` и publisher, и консьюмеры
одного потока:

```go
// legacy — голый payload, тип и id — в заголовках.
type legacy struct{}

func (legacy) Marshal(m broker.Message) ([]byte, map[string]string, error) {
	return m.Data, map[string]string{
		"content-type": m.ContentType,
		"x-event-id":   m.ID,
		"x-event-type": m.Type,
	}, nil
}

func (legacy) Unmarshal(body []byte, h map[string]string) (broker.Message, error) {
	if h["x-event-type"] == "" {
		return broker.Message{}, errors.New("legacy: no event type")
	}
	return broker.Message{
		ID: h["x-event-id"], Type: h["x-event-type"],
		ContentType: h["content-type"], Data: body,
	}, nil
}
```

```go
pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "orders"},
	rabbitmq.WithEnvelope(legacy{}))
cons, err := rabbitmq.NewConsumer(log, conn, rabbitmq.ConsumerConfig{Queue: "orders.payments"},
	app.HandlePaid, rabbitmq.WithEnvelope(legacy{}))
```

Заголовки конверта адаптер кладёт поверх `Message.Headers`, поэтому trace context не теряется.
`content-type` RabbitMQ переносит в свойство `content_type`, Kafka оставляет заголовком.
Консьюмер получает все заголовки доставки в `Message.Headers`, а сообщение, которое конверт не
разобрал, отвергает с предупреждением в логе. Ошибку `Marshal` адаптер возвращает как есть:
если сообщение упаковать нельзя совсем, оберните её в `broker.Permanent`.
