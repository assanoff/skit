# outbox + dbx + RabbitMQ: транзакции и события в сервисе

> Пример к `research-mods.md` §7. Показано **целевое** состояние после ломающего релиза
> (`research-mods.md` §3): логгер — `*slog.Logger`, транзакции открывает store. Сегодня
> `outbox` и `dbx` принимают `*logger.Logger`. Транспорт — как в [rabbitmq.md](rabbitmq.md).

## 1. Импорты

```go
import (
	"github.com/assanoff/skit/broker/rabbitmq"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/outbox"
	"github.com/assanoff/skit/worker"
)
```

`outbox` и `broker/rabbitmq` друг о друге не знают: `outbox.NewRelay` принимает
`broker.Publisher`. Встречаются они только в сборке сервиса.

## 2. Контракт домена: транзакция — забота store

Домен не видит ни `sqlx`, ни `tx`, ни `db`: транзакцию открывает store и отдаёт в
callback **себя же, привязанного к ней**. Методов два — с событиями и без, чтобы домену,
которому событие не нужно, не приходилось принимать и игнорировать `pub`.

```go
package orderapp

import (
	"context"

	"github.com/assanoff/skit/outbox"
)

// Store — контракт хранилища заказов. Реализация — orderdb.
type Store interface {
	Create(ctx context.Context, o Order) error
	AddPayment(ctx context.Context, id string, p Payment) error
	SetStatus(ctx context.Context, id string, st Status) error
	QueryByID(ctx context.Context, id string) (Order, error)

	// WithinTran выполняет fn в одной транзакции; s привязан к ней.
	WithinTran(ctx context.Context, fn func(s Store) error) error

	// WithinTranEvents — то же, плюс pub пишет события в outbox в этой же
	// транзакции: запись и события фиксируются или откатываются вместе.
	WithinTranEvents(ctx context.Context,
		fn func(s Store, pub outbox.Publisher) error) error
}
```

## 3. Бизнес-логика

```go
package orderapp

type OrderCreated struct {
	ID string `json:"id"`
}

type Core struct {
	store Store // единственная зависимость: ни db, ни outbox, ни registry
}

func New(store Store) *Core { return &Core{store: store} }

// Create — запись + событие в одной транзакции.
func (c *Core) Create(ctx context.Context, o Order) error {
	return c.store.WithinTranEvents(ctx,
		func(s Store, pub outbox.Publisher) error {
			if err := s.Create(ctx, o); err != nil {
				return err // откатит и запись, и событие
			}
			return pub.Publish(ctx, OrderCreated{ID: o.ID})
		})
}

// Pay — две записи в одной транзакции, событие не нужно.
func (c *Core) Pay(ctx context.Context, id string, p Payment) error {
	return c.store.WithinTran(ctx, func(s Store) error {
		if err := s.AddPayment(ctx, id, p); err != nil {
			return err
		}
		return s.SetStatus(ctx, id, StatusPaid)
	})
}

// Get — без транзакции, как обычно.
func (c *Core) Get(ctx context.Context, id string) (Order, error) {
	return c.store.QueryByID(ctx, id)
}
```

Домен не знает ни топика, ни транспорта: маршрут `OrderCreated` → `orders` задан в
`Registry` при сборке. `outbox.Publisher` пишет событие в таблицу `outbox_events`;
доставку в брокер делает Relay — это разные Publisher'ы.

В тестах домена mock (moq) реализует оба метода как `return fn(mock)` /
`return fn(mock, fakePub)` — без БД и outbox; `fakePub` просто собирает события.

## 3.1. Реализация store поверх `dbx`

```go
package orderdb

import (
	"context"
	"errors"
	"log/slog"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/outbox"
	"github.com/jmoiron/sqlx"
)

var ErrOutboxNotConfigured = errors.New("orderdb: outbox not configured")

type Store struct {
	log    *slog.Logger
	db     *sqlx.DB        // пул: от него открываются транзакции
	ext    sqlx.ExtContext // куда идут запросы: db или tx
	outbox outbox.Store    // nil — сервис без брокера
	reg    *outbox.Registry
}

func New(log *slog.Logger, db *sqlx.DB, ob outbox.Store,
	reg *outbox.Registry) *Store {
	return &Store{log: log, db: db, ext: db, outbox: ob, reg: reg}
}

var _ orderapp.Store = (*Store)(nil)

// withTx — копия store, чьи запросы идут в tx.
func (s *Store) withTx(tx sqlx.ExtContext) *Store {
	c := *s
	c.ext = tx
	return &c
}

func (s *Store) WithinTran(ctx context.Context,
	fn func(s orderapp.Store) error) error {
	return dbx.WithinTran(ctx, s.log, s.db, func(tx *sqlx.Tx) error {
		return fn(s.withTx(tx))
	})
}

func (s *Store) WithinTranEvents(ctx context.Context,
	fn func(s orderapp.Store, pub outbox.Publisher) error) error {
	if s.outbox == nil {
		return ErrOutboxNotConfigured
	}
	return outbox.WithinTran(ctx, s.log, s.db, s.outbox, s.reg,
		func(tx *sqlx.Tx, pub outbox.Publisher) error {
			return fn(s.withTx(tx), pub)
		})
}

func (s *Store) Create(ctx context.Context, o orderapp.Order) error {
	const q = `INSERT INTO orders (id, total) VALUES (:id, :total)`
	return dbx.NamedExecContext(ctx, s.log, s.ext, q, toDB(o))
}

// AddPayment, SetStatus, QueryByID — так же, через s.ext.
```

Ограничения:

* **Один store на транзакцию.** callback получает только свой store. Если в одной
  транзакции нужны два (`orders` + `stock`), сборка или координирующий слой вызывает
  `outbox.WithinTran` / `dbx.WithinTran` напрямую и привязывает оба через `tx`.
* **Не вкладывать.** `WithinTran` на store внутри callback откроет **вторую** транзакцию
  от пула, а не вложенную. Внутри callback работать только с `s`.

## 4. Сборка

Сборка разделена, как в шаблонах `skit new` (`server.New` + `initWorkers` /
`addOutboxWorkers`): `deps.New` только создаёт и возвращает зависимости, ничего не
запуская; воркеры outbox регистрирует отдельная функция с говорящим именем.

```go
package deps

import (
	"context"
	"log/slog"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/rabbitmq"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/outbox"
	"github.com/jmoiron/sqlx"
)

// Deps — собранные зависимости, без запуска чего-либо.
type Deps struct {
	DB        *sqlx.DB
	Registry  *outbox.Registry
	Publisher broker.Publisher // *rabbitmq.Publisher за интерфейсом broker
	Outbox    outbox.Store
}

func New(ctx context.Context, log *slog.Logger, cfg Config) (*Deps, error) {
	db, err := dbx.Open(cfg.DB)
	if err != nil {
		return nil, err
	}

	// Таблица outbox_events — под advisory lock, безопасно для нескольких реплик.
	key := dbx.AdvisoryKey("outbox")
	if err := dbx.EnsureSchema(ctx, log, db, key, outbox.Schema()); err != nil {
		return nil, err
	}

	reg := outbox.NewRegistry()
	err = reg.Register[orderapp.OrderCreated]("order.created", "orders")
	if err != nil {
		return nil, err
	}

	conn, err := rabbitmq.Dial(log, cfg.Rabbit)
	if err != nil {
		return nil, err
	}
	pub, err := rabbitmq.NewPublisher(conn, "orders", log)
	if err != nil {
		return nil, err
	}

	return &Deps{
		DB:        db,
		Registry:  reg,
		Publisher: pub,
		Outbox:    outbox.NewPG(log, db, outbox.Options{}),
	}, nil
}
```

```go
// addOutboxWorkers регистрирует relay, sweeper и cleaner, которые доставляют
// события, записанные в outbox внутри бизнес-транзакции.
func addOutboxWorkers(log *slog.Logger, g *worker.Group, d *deps.Deps,
	m *metrics.Metrics) {
	om := outbox.NewMetrics(m.Registry)
	g.Add(
		// outbox видит только broker.Publisher, о RabbitMQ он не знает.
		outbox.NewRelay(log, d.Outbox, d.Publisher, outbox.RelayConfig{Metrics: om}),
		outbox.NewSweeper(log, d.Outbox, outbox.SweeperConfig{Metrics: om}),
		outbox.NewCleaner(log, d.Outbox, outbox.CleanerConfig{Metrics: om}),
	)
}
```

В `main` видно, что собирается и что запускается:

```go
d, err := deps.New(ctx, log, cfg)
if err != nil {
	return err
}
app := orderapp.New(orderdb.New(log, d.DB, d.Outbox, d.Registry))
addOutboxWorkers(log, g, d, m)
```

Команде миграций или тесту, которым нужны `DB` и `Outbox` без relay, достаточно
`deps.New` — `addOutboxWorkers` они не вызывают.

Kafka вместо RabbitMQ — замена `pub` в `deps.New` на `kafka.NewPublisher(…)`; `outbox`,
`dbx`, store, `addOutboxWorkers` и домен не меняются.
