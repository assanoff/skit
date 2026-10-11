# outbox + dbx + RabbitMQ: транзакции и события в сервисе

> Пример к `research-mods.md` §6. Показано **целевое** состояние после ломающего релиза
> (`research-mods.md` §3): логгер — `*slog.Logger`, транзакции открывает store, метрики —
> реестр `metrics.New()` в `Deps` и поле `Metrics` в `Config` (§7). Сегодня `outbox` и `dbx`
> принимают `*logger.Logger`, а outbox — набор `outbox.NewMetrics` и
> `outbox.NewBacklogCollector`. Транспорт — как в [rabbitmq.md](rabbitmq.md).

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

Домен не видит ни `sqlx`, ни `tx`, ни `db`, ни `outbox`: транзакцию открывает store и
отдаёт в callback **репозиторий, привязанный к ней**. Методов два — с событиями и без,
чтобы домену, которому событие не нужно, не приходилось принимать и игнорировать `pub`.

```go
package orderapp

import "context"

// Repo — CRUD заказов. Его же получает callback транзакции.
type Repo interface {
	Create(ctx context.Context, o Order) error
	AddPayment(ctx context.Context, id string, p Payment) error
	SetStatus(ctx context.Context, id string, st Status) error
	QueryByID(ctx context.Context, id string) (Order, error)
}

// Publisher пишет событие в той же транзакции.
type Publisher interface {
	Publish(ctx context.Context, event any) error
}

// Store — контракт хранилища заказов. Реализация — orderdb.
type Store interface {
	Repo

	// WithinTran выполняет fn в одной транзакции; ctx и r привязаны к ней.
	WithinTran(ctx context.Context,
		fn func(ctx context.Context, r Repo) error) error

	// WithinTranEvents — то же, плюс pub пишет события в outbox в этой же
	// транзакции: запись и события фиксируются или откатываются вместе.
	WithinTranEvents(ctx context.Context,
		fn func(ctx context.Context, r Repo, pub Publisher) error) error
}
```

Через `r` вложенную транзакцию не открыть: у `Repo` нет `WithinTran*`. Обращение к
`c.store` из замыкания ловит `dbx` (`research-mods.md` §6.2): `ctx` из callback несёт флаг
открытой транзакции, и второй `WithinTran` возвращает `dbx.ErrNestedTx`, а запрос на пуле —
`dbx.ErrPoolInTx`.

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
		func(ctx context.Context, r Repo, pub Publisher) error {
			if err := r.Create(ctx, o); err != nil {
				return err // откатит и запись, и событие
			}
			return pub.Publish(ctx, OrderCreated{ID: o.ID})
		})
}

// Pay — две записи в одной транзакции, событие не нужно.
func (c *Core) Pay(ctx context.Context, id string, p Payment) error {
	return c.store.WithinTran(ctx, func(ctx context.Context, r Repo) error {
		if err := r.AddPayment(ctx, id, p); err != nil {
			return err
		}
		return r.SetStatus(ctx, id, StatusPaid)
	})
}

// Get — без транзакции, как обычно.
func (c *Core) Get(ctx context.Context, id string) (Order, error) {
	return c.store.QueryByID(ctx, id)
}
```

Домен не знает ни топика, ни ключа, ни транспорта: маршрут `OrderCreated` → `orders` и
ключ заданы в `Registry` при сборке, событие выбирается Go-типом. `Publisher`
пишет событие в таблицу `outbox_events`; доставку в брокер делает Relay — это разные
Publisher'ы.

Тип события (`OrderCreated`) живёт в домене и ничего не импортирует; CloudEvents-тип, топик
и регистрация — в сборке (§4).

В тестах домена моки (moq) `Store` и `Repo`: `return fn(ctx, repoMock)` /
`return fn(ctx, repoMock, pubMock)` — без БД и outbox; `pubMock` просто собирает события.

## 3.1. Реализация store поверх `dbx`

```go
package orderdb

import (
	"cmp"
	"context"
	"log/slog"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/dialect"
	"github.com/assanoff/skit/outbox"
	"github.com/jmoiron/sqlx"
)

// repo — CRUD поверх пула или tx.
type repo struct {
	log     *slog.Logger
	ext     sqlx.ExtContext // куда идут запросы: db или tx
	dialect dialect.Dialect
}

// withTx — копия repo, привязанная к tx; остальные поля переносятся сами.
func (r *repo) withTx(tx *sqlx.Tx) *repo {
	c := *r
	c.ext = tx
	return &c
}

// Config — настройки store.
type Config struct {
	Dialect  dialect.Dialect  // nil — dialect.Postgres{}
	Outbox   outbox.Store     // nil — сервис без брокера: события отбрасываются
	Registry *outbox.Registry // нужен вместе с Outbox
}

type Store struct {
	*repo                  // CRUD вне транзакции — на пуле
	db     *sqlx.DB        // пул: от него открываются транзакции
	outbox outbox.Store
	reg    *outbox.Registry
}

func New(log *slog.Logger, db *sqlx.DB, cfg Config) *Store {
	return &Store{
		repo: &repo{
			log:     log,
			ext:     db,
			dialect: cmp.Or[dialect.Dialect](cfg.Dialect, dialect.Postgres{}),
		},
		db:     db,
		outbox: cfg.Outbox,
		reg:    cfg.Registry,
	}
}

var _ orderapp.Store = (*Store)(nil)

func (s *Store) WithinTran(ctx context.Context,
	fn func(ctx context.Context, r orderapp.Repo) error) error {
	return dbx.WithinTran(ctx, s.log, s.db,
		func(ctx context.Context, tx *sqlx.Tx) error {
			return fn(ctx, s.repo.withTx(tx))
		})
}

func (s *Store) WithinTranEvents(ctx context.Context,
	fn func(ctx context.Context, r orderapp.Repo, pub orderapp.Publisher) error) error {
	if s.outbox == nil {
		// Без брокера: запись проходит, событие отбрасывается.
		return s.WithinTran(ctx, func(ctx context.Context, r orderapp.Repo) error {
			return fn(ctx, r, discard{})
		})
	}
	return outbox.WithinTran(ctx, s.log, s.db, s.outbox, s.reg,
		func(ctx context.Context, tx *sqlx.Tx, pub outbox.Publisher) error {
			// Plain — адаптер: Publish(ctx, event) без опций маршрута.
			return fn(ctx, s.repo.withTx(tx), outbox.Plain(pub))
		})
}

// discard — Publisher сервиса без брокера.
type discard struct{}

func (discard) Publish(context.Context, any) error { return nil }

func (r *repo) Create(ctx context.Context, o orderapp.Order) error {
	const q = `INSERT INTO orders (id, total) VALUES (:id, :total)`
	return dbx.NamedExecContext(ctx, r.log, r.ext, q, toDB(o))
}

// AddPayment, SetStatus, QueryByID — так же, через r.ext.
```

Ограничения:

* **`c.store` внутри callback ловится в рантайме, не компилятором:** `ErrNestedTx` /
  `ErrPoolInTx` на первом вызове с настоящей БД; моки домена её не увидят. Внутри callback —
  только `r` и `pub`.
* **Один store на транзакцию.** callback получает только свой `Repo`. Если в одной
  транзакции нужны два (`orders` + `stock`), сборка или координирующий слой вызывает
  `outbox.WithinTran` / `dbx.WithinTran` напрямую и привязывает оба через `tx`.

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
	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/outbox"
	"github.com/jmoiron/sqlx"
)

// Deps — собранные зависимости, без запуска чего-либо.
type Deps struct {
	Metrics   *metrics.Metrics // реестр процесса, один на сервис
	DB        *sqlx.DB
	Registry  *outbox.Registry
	Publisher broker.Publisher // *rabbitmq.Publisher за интерфейсом broker
	Outbox    *outbox.PG       // Store + StatsReader для опроса backlog
}

func New(ctx context.Context, log *slog.Logger, cfg Config) (*Deps, error) {
	m := metrics.New()

	cfg.DB.Metrics = m // пул БД: go_sql_*{db_name,host}
	db, err := dbx.Open(cfg.DB)
	if err != nil {
		return nil, err
	}

	// Таблица outbox_events — под advisory lock пакета outbox, безопасно для
	// нескольких реплик.
	ob := outbox.NewPG(log, db, outbox.Options{})
	if err := ob.EnsureSchema(ctx); err != nil {
		return nil, err
	}

	reg := outbox.NewRegistry()
	// Ключ события — см. research-mods.md §6.5: для RabbitMQ это routing key.
	err = reg.Register[orderapp.OrderCreated]("order.created", "orders",
		outbox.WithKey("order.created"))
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
		Metrics:   m,
		DB:        db,
		Registry:  reg,
		Publisher: pub,
		Outbox:    ob,
	}, nil
}
```

```go
// addOutboxWorkers регистрирует relay, sweeper и cleaner, которые доставляют
// события, записанные в outbox внутри бизнес-транзакции, и опрос backlog.
func addOutboxWorkers(log *slog.Logger, g *worker.Group, d *deps.Deps) {
	m := d.Metrics // реестр процесса, всегда создан в deps.New (research-mods.md §7)
	g.Add(
		// outbox видит только broker.Publisher, о RabbitMQ он не знает.
		outbox.NewRelay(log, d.Outbox, d.Publisher, outbox.RelayConfig{Metrics: m}),
		outbox.NewSweeper(log, d.Outbox, outbox.SweeperConfig{Metrics: m}),
		outbox.NewCleaner(log, d.Outbox, outbox.CleanerConfig{Metrics: m}),
		// backlog — опрос таблицы раз в 30 с в группе воркеров.
		outbox.NewBacklog(log, d.Outbox, m),
	)
}
```

В `main` видно, что собирается и что запускается:

```go
d, err := deps.New(ctx, log, cfg)
if err != nil {
	return err
}
app := orderapp.New(orderdb.New(log, d.DB, orderdb.Config{
	Outbox:   d.Outbox,
	Registry: d.Registry,
}))
addOutboxWorkers(log, g, d)
```

Команде миграций или тесту, которым нужны `DB` и `Outbox` без relay, достаточно
`deps.New` — `addOutboxWorkers` они не вызывают.

Kafka вместо RabbitMQ — замена `pub` в `deps.New` на `kafka.NewPublisher(…)`; `outbox`,
`dbx`, store, `addOutboxWorkers` и домен не меняются.
