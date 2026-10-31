# outbox + dbx + RabbitMQ: транзакции и события в сервисе

> Пример к `research-mods.md` §6. Показано **целевое** состояние после ломающего релиза
> (`research-mods.md` §3): транзакцию открывает store (§6.2), ключ события выводит транспорт
> (§6.5), доставку собирает `outbox.Workers` (§6.6), метрики — реестр `metrics.New()` в `Deps`
> (§7). Транспорт — как в [rabbitmq.md](rabbitmq.md).

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

`outbox` и `broker/rabbitmq` друг о друге не знают: relay принимает `broker.Publisher`.
Встречаются они только в сборке сервиса.

## 2. Контракт домена: транзакция — забота store

Домен не видит ни `dbx`, ни `tx`, ни `outbox`: транзакцию открывает store и отдаёт в callback
**репозиторий, привязанный к ней**. Методов два — с событиями и без, чтобы домену, которому
событие не нужно, не приходилось принимать и игнорировать `pub`. Через `ctx` не идёт ничего: ни
транзакция, ни соединение, ни метка транзакции.

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

// Publisher пишет событие в той же транзакции. Свой в домене — outbox домен
// не импортирует.
type Publisher interface {
	Publish(ctx context.Context, event any) error
}

// Store — CRUD и транзакции. fn может выполниться повторно (повтор при
// deadlock): внутри — только r и pub, без состояния между запусками и без
// вызовов Store. Store, собранный над чужой транзакцией, входит в неё и не
// коммитит.
type Store interface {
	Repo

	// WithinTran выполняет fn в одной транзакции; r привязан к ней.
	WithinTran(ctx context.Context, fn func(r Repo) error) error

	// WithinTranEvents — то же, плюс pub пишет события в outbox в этой же
	// транзакции: запись и события фиксируются или откатываются вместе.
	WithinTranEvents(ctx context.Context,
		fn func(r Repo, pub Publisher) error) error
}
```

Вложенность закрыта типами: у `Repo` нет `WithinTran*`, поэтому callback получает только `r` и
`pub`; у `*dbx.Tx` нет `Begin`, `Commit` и `Rollback`, поэтому собранное над транзакцией свою
не откроет — `WithinTran` на нём входит в транзакцию вызывающего. Callback без `ctx`: внутри fn
используется `ctx` метода.

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
	return c.store.WithinTranEvents(ctx, func(r Repo, pub Publisher) error {
		if err := r.Create(ctx, o); err != nil {
			return err // откатит и запись, и событие
		}
		return pub.Publish(ctx, OrderCreated{ID: o.ID})
	})
}

// Pay — две записи в одной транзакции, событие не нужно.
func (c *Core) Pay(ctx context.Context, id string, p Payment) error {
	return c.store.WithinTran(ctx, func(r Repo) error {
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

Домен не знает ни топика, ни ключа, ни транспорта: топик `OrderCreated` задан в `Registry`
при сборке, событие выбирается Go-типом, ключ выводит транспорт. `Publisher` пишет событие в
таблицу `outbox_events`; доставку в брокер делает relay — это разные Publisher'ы.

Тип события (`OrderCreated`) живёт в домене и ничего не импортирует; CloudEvents-тип, топик и
регистрация — в сборке (§4).

В тестах домена моки (moq) `Store` и `Repo`: `return fn(repoMock)` /
`return fn(repoMock, pubMock)` — без БД и outbox; `pubMock` просто собирает события.

## 3.1. Реализация store поверх `dbx`

```go
package orderdb

import (
	"context"
	"log/slog"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/outbox"
)

// repo — CRUD поверх пула или tx.
type repo struct {
	log *slog.Logger
	q   dbx.Querier // *dbx.DB или *dbx.Tx
}

// withTx — копия repo, привязанная к tx; остальные поля переносятся сами.
func (r *repo) withTx(tx *dbx.Tx) *repo {
	c := *r
	c.q = tx
	return &c
}

// Option настраивает Store.
type Option func(*Store)

// WithEvents включает события: WithinTranEvents отдаёт публикатор fn(tx),
// привязанный к транзакции. Без опции события отбрасываются.
func WithEvents(fn func(tx *dbx.Tx) *outbox.Publisher) Option {
	return func(s *Store) { s.events = fn }
}

// Store над пулом открывает транзакции, над транзакцией — входит в неё.
type Store struct {
	*repo  // CRUD на q
	events func(tx *dbx.Tx) *outbox.Publisher
}

// NewStore — store над q: пулом (*dbx.DB) или транзакцией (*dbx.Tx).
func NewStore(log *slog.Logger, q dbx.Querier, opts ...Option) *Store {
	s := &Store{repo: &repo{log: log, q: q}, events: discardEvents}
	for _, o := range opts {
		o(s)
	}
	return s
}

var _ orderapp.Store = (*Store)(nil)

func (s *Store) WithinTran(ctx context.Context,
	fn func(r orderapp.Repo) error) error {
	return dbx.WithinTran(ctx, s.log, s.q, func(tx *dbx.Tx) error {
		return fn(s.repo.withTx(tx))
	})
}

func (s *Store) WithinTranEvents(ctx context.Context,
	fn func(r orderapp.Repo, pub orderapp.Publisher) error) error {
	return dbx.WithinTran(ctx, s.log, s.q, func(tx *dbx.Tx) error {
		return fn(s.repo.withTx(tx), s.events(tx))
	})
}

// discardEvents — сборка без событий: запись проходит, событие отбрасывается.
func discardEvents(*dbx.Tx) *outbox.Publisher { return outbox.Discard() }

func (r *repo) Create(ctx context.Context, o orderapp.Order) error {
	const q = `INSERT INTO orders (id, total) VALUES (:id, :total)`
	return dbx.NamedExec(ctx, r.q, q, toDB(o))
}

// AddPayment, SetStatus, QueryByID — так же, через r.q. Сентинелы dbx
// (ErrDBNotFound) store переводит в доменные (orderapp.ErrNotFound).
```

`*outbox.Publisher` присваивается доменному `Publisher` без адаптера, поэтому одна функция
`events` подходит store всех доменов (§4).

Ограничения (`research-mods.md` §6.4):

* **Вызов `c.*` внутри callback типами не закрыт:** `c.store` в fn открывает вторую транзакцию
  на пуле, она ждёт блокировку внешней до дедлайна ctx. Внутри callback — только `r` и `pub`;
  логика внутри транзакции — неэкспортируемая функция над ними, публичный метод — обёртка.
* **fn может выполниться повторно** — при deadlock и ошибке сериализации: без состояния между
  запусками, результат из замыкания присваивать, а не дописывать.
* **Вход в транзакцию — не savepoint:** ошибку вошедшего вызова всегда возвращать из fn.

## 3.2. Несколько репозиториев и чужая бизнес-логика

Когда операция пишет через несколько store или вызывает бизнес-логику другого домена в одной
транзакции, домен объявляет, что видно внутри неё, своими типами. Код для одного репозитория
не меняется.

```go
package orderapp

// Stock — то, что заказу нужно от бизнес-логики склада.
type Stock interface {
	Reserve(ctx context.Context, sku string, qty int) error
}

// Unit — то, что видно внутри одной транзакции.
type Unit struct {
	Repo  Repo
	Pub   Publisher
	Stock Stock // бизнес-логика склада над той же транзакцией
}

// Tran выполняет fn в одной транзакции. fn может выполниться повторно:
// внутри — только u, без состояния между запусками и без вызовов Core.
type Tran interface {
	WithinTran(ctx context.Context, fn func(u Unit) error) error
}
```

`Tran` — поле и параметр `New` рядом со `Store`. Сборка строит `Unit` из транзакции на каждой
попытке, `dbx.Tran[U]` удовлетворяет доменному `Tran` без адаптера:

```go
tran := dbx.NewTran(log, db, func(tx *dbx.Tx) orderapp.Unit {
	return orderapp.Unit{
		Repo:  orderdb.NewStore(log, tx, orderdb.WithEvents(events)),
		Pub:   events(tx),
		Stock: stockapp.New(stockdb.NewStore(log, tx, stockdb.WithEvents(events))),
	}
})
```

`Reserve` склада вызывает свой `WithinTranEvents`; store над `tx` входит в транзакцию заказа:
правила склада, его событие и откат — общие, код склада не меняется.

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
	"github.com/assanoff/skit/dbx/pg"
	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/outbox"
)

// Deps — собранные зависимости, без запуска чего-либо.
type Deps struct {
	Metrics   *metrics.Metrics // реестр процесса, один на сервис
	DB        *dbx.DB
	Registry  *outbox.Registry
	Publisher broker.Publisher // *rabbitmq.Publisher за интерфейсом broker
	Outbox    *outbox.PG
}

func New(ctx context.Context, log *slog.Logger, cfg Config) (*Deps, error) {
	m := metrics.New()

	cfg.DB.Metrics = m // пул БД: go_sql_*{db_name,host}
	db, err := pg.Open(cfg.DB)
	if err != nil {
		return nil, err
	}

	// Таблица outbox_events — под advisory lock пакета outbox, безопасно для
	// нескольких реплик.
	ob := outbox.NewPG(log, db)
	if err := ob.EnsureSchema(ctx); err != nil {
		return nil, err
	}

	reg := outbox.NewRegistry()
	err = reg.Register[orderapp.OrderCreated]("order.created", "orders")
	if err != nil {
		return nil, err
	}

	conn, err := rabbitmq.Dial(cfg.Rabbit)
	if err != nil {
		return nil, err
	}
	// exchange — топик (orders), routing key — тип события (order.created);
	// своя маршрутизация — rabbitmq.WithRoutingKey (research-mods.md §6.5).
	pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "orders"})
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

// Events — единственное место, где сборка решает, куда идут события: в outbox
// транзакции tx.
func (d *Deps) Events(tx *dbx.Tx) *outbox.Publisher {
	return outbox.Bind(tx, d.Outbox, d.Registry)
}
```

```go
// addOutboxWorkers регистрирует доставку событий, записанных в outbox внутри
// бизнес-транзакции: relay, sweeper, cleaner и опрос backlog.
func addOutboxWorkers(log *slog.Logger, g *worker.Group, d *deps.Deps) {
	// outbox видит только broker.Publisher, о RabbitMQ он не знает.
	g.Add(outbox.Workers(log, d.Outbox, d.Publisher, outbox.WithMetrics(d.Metrics))...)
}
```

Брокер недоступен — relay повторяет публикацию без лимита попыток, с паузой до 5 минут, и
ничего не теряет: первая ошибка останавливает пачку, остальные события возвращаются в очередь
нетронутыми. В `failed` событие уходит, только если publisher пометил ошибку
`broker.Permanent`: конверт не собирается или сообщение больше предела брокера. Тревога —
по `skit_outbox_oldest_pending_seconds` и `skit_outbox_failed`.

В `main` видно, что собирается и что запускается:

```go
d, err := deps.New(ctx, log, cfg)
if err != nil {
	return err
}
app := orderapp.New(orderdb.NewStore(log, d.DB, orderdb.WithEvents(d.Events)))
addOutboxWorkers(log, g, d)
```

Брокер выключен — store без `WithEvents`: `orderdb.NewStore(log, d.DB)`, запись проходит,
события отбрасываются, домен не ветвится.

Команде миграций или тесту, которым нужны `DB` и `Outbox` без relay, достаточно
`deps.New` — `addOutboxWorkers` они не вызывают.

Kafka вместо RabbitMQ — замена `pub` в `deps.New` на `kafka.NewPublisher(…)`; `outbox`,
`dbx`, store, `addOutboxWorkers` и домен не меняются.
