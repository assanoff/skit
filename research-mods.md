# research-mods.md — гранулярность модулей и версионирование skit

> Статус: **исследование + план**. Ничего не переносим, пока решение не принято.
> Дата: 2026-09-13. Базис: `master`, последний тег `v0.11.0`, 57 пакетов, один `go.mod`.

---

## 0. Короткий ответ

**Разбивать репозиторий на отдельные `go.mod` прямо сейчас — не надо.** Главная боль,
ради которой обычно дробят (тяжёлые транзитивные зависимости у потребителя), в Go 1.17+
уже решена **module graph pruning** — и замеры это подтверждают: потребитель, который
импортирует только `skit/page`, получает `go.sum` из **2 строк** и **0 indirect**
зависимостей (§3).

Настоящая цель — *«после 1.0.0 не ломать пользовательский API и при этом выпускать
обновления»* — **достигается не разбиением на модули, а дисциплиной API**: `internal/`,
add-only-правило, type alias'ы, CI-гейт на `apidiff`. Сейчас в SDK **нет ни одного
`internal/` пакета** — то есть все ~752 экспортированных объявления станут вечным
контрактом (§6.1). Это риск на порядок больше, чем гранулярность модулей.

Разбиение всё же даёт одну вещь, которую дисциплиной не заменить: **радиус взрыва
мажорной версии**. При одном `go.mod` любой breaking change в `rest` уводит в `/v2`
*весь* SDK — включая `errs`, `page`, `logger`, которые не менялись. Поэтому план такой:

| Этап | Когда | Что делаем |
|---|---|---|
| **A. Подготовка** | сейчас, до 1.0.0 | чистим границы пакетов, вводим `internal/`, чиним аномалии (§2.4), готовим однонаправленный DAG модулей |
| **B. Заморозка API** | перед 1.0.0 | add-only-правило, alias-слой, `apidiff`-гейт в CI, политика deprecation |
| **C. Разделение** | в момент 1.0.0 | **4 модуля** (не 15 и не 57): `skit`, `skit/db`, `skit/msg`, `skit/grpckit` |
| **D. Опционально** | по факту churn'а | выделить `skit/auditlog`, `skit/translation`, если у них своя каденция |

Ключевые правила, из которых всё остальное выводится:

1. **Пакет заслуживает отдельного модуля только если у него есть *хотя бы два* из:**
   тяжёлая/волатильная внешняя зависимость, своя каденция релизов, опциональность для
   большинства пользователей. Хелперы (`safetick`, `retry`, `httpw`, `to`, `apitest`)
   не проходят ни по одному пункту — им место в ядре.
2. **Типы, которые ходят через границы пакетов в публичном API, живут в одном модуле,
   который мы обязуемся никогда не мажорить.** Это `errs`, `logger`, `rest`, `page`,
   `order`. Иначе `errors.As(err, &*errs.Error)` начнёт молча возвращать `false` (§4.3).
3. **Рёбра между модулями — только вниз.** Ядро никогда не импортирует спутники.

---

## 1. Методика

Всё, что ниже, получено из репозитория, а не из общих соображений:

```bash
# граф импортов (прод + тесты, отдельно)
go list -f '{{$p := .ImportPath}}{{range .Imports}}{{$p}}|{{.}}{{end}}' ./...
go list -f '{{$p := .ImportPath}}{{range .TestImports}}...{{range .XTestImports}}...' ./...

# footprint потребителя: пустой модуль, импортирующий ровно один пакет skit
mkdir probe && cd probe
printf 'module probe\n\ngo 1.26\n' > go.mod
printf 'package main\nimport _ "github.com/assanoff/skit/<pkg>"\nfunc main(){}\n' > main.go
go get github.com/assanoff/skit@v0.11.0 && go mod tidy
grep -c '// indirect' go.mod; wc -l go.sum; go list -m all | wc -l

# churn по релизам
git diff --name-only <tagA>..<tagB> -- '*.go' | awk -F/ '{print $1}' | sort -u
```

---

## 2. Карта зависимостей

### 2.1. Слои

Граф **чистый ациклический, глубиной 4** — это нетипично хорошо и означает, что
репозиторий в принципе *готов* к разбиению (нет клубка взаимных ссылок).

```
L0  (0 внутренних зависимостей)
    logger  errs  retry  safetick  httpw  dim  closer  page  order
    config  health  httpserver  migrate  otel  translation  grpcgateway
    apitest  dbx/dialect
                    │
L1  ────────────────┼──────────────────────────────────────────────
    dbx(logger,retry)        rest(errs)         worker(errs,retry,safetick)
    broker(safetick)         i18n(errs)         httpmw(retry)
    metrics(httpw)           eventbus(logger)   query(errs,page)
    poller(safetick)         cron(safetick)     chx(migrate)
    debugsrv(httpserver)     to(rest)           rest/router(rest)
    httplog(httpw,logger)    middleware(httpw,logger,otel)
    grpcserver(errs,logger,otel)
                    │
L2  ────────────────┼──────────────────────────────────────────────
    queue(dbx,logger,retry,worker)      lock(dbx)      dbtest(dbx,migrate)
    auditlog(dbx,errs,logger,safetick)  auth(errs,logger,rest)
    broker/kafka(broker,logger)         broker/rabbitmq(broker,logger)
    httpclient(httpmw)                  rest/mid(errs,i18n,logger,rest)
    translation/postgres(dbx,logger,translation)
    translation/translationrest(logger,rest,translation)
    grpctest(grpcserver,logger)         app(closer,dim,worker)
                    │
L3  ────────────────┼──────────────────────────────────────────────
    outbox(broker,dbx,logger,metrics,otel,retry,worker)
    auditlog/auditbus(auditlog,eventbus)   auditlog/db(auditlog,dbx,logger)
    auditlog/auditrest(auditlog,errs,rest) auditlog/mocks(auditlog)
    provider(broker/rabbitmq,dbx,dim,i18n,logger,otel)
                    │
L4  ────────────────┼──────────────────────────────────────────────
    auditlog/auditqueue(auditlog,auditlog/auditbus,logger,queue,worker)
```

### 2.2. Полностью независимые пакеты

19 пакетов не импортируют ничего из skit — их можно вынести куда угодно без каскада:

`apitest` · `closer` · `cmd/skit` · `config` · `dbx/dialect` · `dim` · `errs` ·
`grpcgateway` · `health` · `httpserver` · `httpw` · `logger` · `migrate` · `order` ·
`otel` · `page` · `retry` · `safetick` · `translation`

Из них **полностью без внешних зависимостей** (чистый stdlib): `logger`, `retry`,
`safetick`, `httpw`, `dim`, `closer`, `page`, `order`, `health`, `httpserver`,
`apitest`, `dbx/dialect`.

### 2.3. Ядро графа: кто держит всех

| пакет | fan-in (транзитивный) | внешние зависимости |
|---|---|---|
| `logger` | **23** | нет (чистый stdlib) |
| `errs` | **21** | go-playground/validator |
| `retry` | **17** | нет |
| `safetick` | **16** | нет |
| `dbx` | **12** | pgx, sqlx |
| `rest` | 6 | (через `errs`) |
| `auditlog` | 5 | go-cmp, sqlx, pgx |
| `otel` | 5 | go.opentelemetry.io/otel + **sdk + otlp-grpc exporter** |
| `worker` | 4 | uuid |
| `broker` | 4 | нет |
| `httpw` | 4 | нет |

**Вывод:** четыре пакета с наибольшим fan-in (`logger`, `errs`, `retry`, `safetick`)
почти не имеют внешних зависимостей. Это идеальное ядро: оно ничего не тянет, но его
типы ходят везде. Его нельзя разносить по модулям и нельзя мажорить.

### 2.4. Аномалии, которые стоит починить независимо от решения по модулям

| # | Находка | Почему важно | Что делать |
|---|---|---|---|
| **A1** | `otel` смешивает **span-хелперы** (`GetTraceID`, `InjectTracing`, `ExtractFromRequest`, `Carrier`) и **бутстрап экспортёра** (`otlptracegrpc` + `sdk/trace` + `sdk/resource`, всё в `otel/otel.go`) | из-за этого `middleware` — обычный net/http middleware — тянет **21 indirect + весь google.golang.org/grpc**. Замер: `middleware` → 21 indirect, `grpc` в графе. Хелперам нужен только trace **API** | разделить: `otel` (только API, ~2 indirect) + `otel/otlp` (бутстрап). Самый выгодный рефакторинг из всего списка |
| **A2** | `provider` (256 LOC) импортирует `broker/rabbitmq` + `dbx` + `dim` + `i18n` + `otel` и тянет sentry, redis, amqp, pgx, go-i18n | это **единственный** пакет, который связывает все «тяжёлые» ветки в одну точку. Любое разбиение на модули он ломает: он зависит от всех сразу. Используется только в шаблонах CLI (3 ссылки) | перенести в `cmd/skit/templates/full/internal/app/deps/` — там ему и место (это wiring приложения, а не SDK) |
| **A3** | `to` (72 LOC) — **0 импортёров**, дублирует `rest.Respond`/JSON:API | лишняя публичная поверхность, которую придётся поддерживать вечно после 1.0.0 | влить в `rest` или удалить **до** 1.0.0 |
| **A4** | `dbx/dialect` (72 LOC) используется только из одного `.tmpl` | отдельный подпакет ради 72 строк | влить в `dbx` |
| **A5** | `httpmw` (319 LOC) импортируется **только** из `httpclient` | fan-in 1, отдельный пакет не оправдан | влить в `httpclient` (или оставить, но осознанно) |
| **A6** | `auditlog/mocks` — **475 LOC моков в публичном API** | моки станут частью контракта 1.0.0 | переименовать в `auditlog/auditmock` и явно задокументировать как «не под гарантией», либо перенести под `internal/` + экспортировать через тонкий фасад |
| **A7** | Непоследовательный нейминг PG-сторов: `auditlog/db` vs `translation/postgres` | после 1.0.0 переименовать нельзя | привести к одному: `auditlog/auditpg` + `translation/translationpg` (или оба `.../postgres`) |
| **A8** | `chx`, `poller`, `to` не заведены ни в один шаблон CLI и не имеют внутренних потребителей | непроверенная поверхность API | либо покрыть шаблоном/примером, либо держать в `x/` со статусом experimental |
| **A9** | `errs` тянет `go-playground/validator` (fan-in 21 → validator попадает к 21 пакету) | `errs.Check` — единственное место. Замер: `errs` → 8 indirect, и **все 8 от validator** (mimetype, locales, universal-translator, go-urn, x/crypto, x/sys, x/text) | вынести `errs.Check`/`FieldError` в отдельный `validate` поверх `errs`; тогда `errs` = 0 indirect, как `logger` |

### 2.5. Про идею «перенести `page` внутрь `rest`»

**Не стоит.** `page` и `order` используются не транспортом, а **доменом и стором**:

```
cmd/skit/templates/rest/core.go.tmpl     ← доменный слой
cmd/skit/templates/rest/db.go.tmpl       ← слой хранилища
cmd/skit/templates/rest/db_order.go.tmpl
cmd/skit/templates/grpc/handler.go.tmpl  ← и gRPC тоже
```

Положить их в `rest/` — значит заставить доменный слой импортировать HTTP-транспорт.
Это ровно та связность, от которой двухслойная архитектура `rest` и защищает.

Правильная группировка, если хочется меньше мелких пакетов: **`order` + `page` + `query`
→ один пакет `list`** (allowlist-сортировка + валидированный paging-вход + envelope
результата — это одна связная тема «листинг»). 101 + 210 + 244 = 555 LOC, нормальный
размер. Делать это можно **только до 1.0.0** — либо после, но через alias-слой (§6.3).

---

## 3. Что реально болит, а что нет — замеры

Собрано на реальном теге `v0.11.0`. Потребитель = пустой модуль, импортирующий **ровно
один** пакет skit.

| импортируется | `// indirect` в go.mod | строк в go.sum | grpc в графе |
|---|---|---|---|
| `logger` | **0** | **2** | нет |
| `page` | **0** | **2** | нет |
| `translation` | 1 | 6 | нет |
| `broker/rabbitmq` | 3 | 12 | нет |
| `migrate` | 5 | 40 | нет |
| `errs` | 8 | 28 | нет |
| `rest` | 8 | 30 | нет |
| `worker` / `auth` | 9 | 32 | нет |
| `i18n` | 9 | 34 | нет |
| `lock` | 10 | 161 | нет |
| `metrics` | 11 | 50 | нет |
| `queue` / `auditlog` | 15 | 59 | нет |
| `otel` / `middleware` | **21** | 65 | **да** ← A1 |
| `chx` | 21 | 74 | нет |
| `outbox` | 40 | 132 | да |
| `grpcserver` | 40 | 120 | да |
| `dbtest` | 63 | 195 | да |

**Что это значит:**

* ✅ **Аргумент «один go.mod раздувает зависимости потребителю» — не подтвердился.**
  Pruning работает: `page` не приносит ни pgx, ни testcontainers, ни ClickHouse, хотя
  все они есть в `go.mod` самого skit (228 модулей в build list SDK, 965 рёбер в
  `go mod graph`). В бинарь неиспользуемое тоже не попадает.
* ⚠️ **Но `go list -m all` у потребителя всё равно показывает 123 модуля** даже для
  `page`. Это задевает SCA-сканеры, Dependabot и `go get -u ./...` — они видят граф
  целиком. Реальная, но умеренная боль.
* ❌ **A1 — настоящая боль и она про пакеты, а не про модули**: `middleware` тащит gRPC.
  Разбиение на go.mod это *не* чинит. Чинит разделение `otel` на два пакета.

### 3.1. Как выглядит churn по релизам

```
v0.3.0 → v0.4.0   8 пакетов:  auditlog dbx httpmw outbox queue retry translation worker
v0.4.0 → v0.5.0   1 пакет:    rest
v0.5.0 → v0.6.0  15 пакетов
v0.6.0 → v0.7.0  18 пакетов
v0.7.0 → v0.8.0  14 пакетов
v0.8.1 → v0.9.0   1 пакет:    translation
v0.9.0 → v0.10.0  2 пакета:   otel translation
v0.10.0 → v0.11.0 1 пакет:    dbx
```

Два режима. Широкие «подметающие» релизы v0.5–v0.8 — это консолидация, нормальная для
v0.x. **Последние четыре релиза узкие (1–2 пакета)** — и это как раз те, где
многомодульность дала бы выигрыш: пользователь `rest` не получал бы правки `translation`.

Но обратите внимание, *какие* пакеты трогали: `dbx` (fan-in 12), `otel` (fan-in 5),
`translation` (fan-in 2). У `dbx` и `otel` разбиение всё равно означало бы, что
зависимые модули захотят новую версию — хотя каскад **не обязателен** (§4.2).

### 3.2. Реальный потребитель

`skit-x` (showcase): `go.mod` — 120 строк, **93 `// indirect`**, `go.sum` — 307 строк.
Но он импортирует почти весь SDK, так что это честная цена, а не следствие
монорепозитория.

---

## 4. Один `go.mod` vs много: за и против

### 4.1. За один модуль (status quo)

| + | Пояснение |
|---|---|
| **Рефакторинг за один коммит** | поменять `logger` и все 23 потребителя — один PR, один CI-прогон. При разбиении это танец: добавить API в core → выпустить core → поднять require в db → выпустить db → … |
| **Невозможен version skew** | одна версия — одна копия каждого типа в бинаре. `errors.As`, type assertion, `dim.Provider[T]` работают всегда |
| **Один тег, один релиз, один `gorelease`** | текущий `make release-auto` работает как есть |
| **Потребитель пишет один `require`** | `skit new` генерирует одну строчку |
| **Pruning уже решает проблему веса** (§3) | ради чего чаще всего и дробят |
| **Атомарный CI** | одна матрица, нет комбинаторики версий |

### 4.2. За много модулей

| + | Пояснение |
|---|---|
| **Радиус взрыва мажора** ⭐ | breaking change в `rest` → `skit/rest/v2`, а не `skit/v2`. Импорты `errs`, `logger`, `page` у пользователя не меняются. **Это единственный аргумент, который прямо бьёт в поставленную цель** |
| **Независимая каденция** | `translation` правился 3 раза за 4 релиза; пользователю `rest` эти правки не нужны |
| **Нет каскада при непроходящем изменении** | MVS: `msg@v1.0` требует `db>=v1.0`; потребитель ставит `db@v1.4` — работает, перевыпускать `msg` не надо. Каскад нужен только когда `msg` захочет *новый* API из `db` |
| **Чище `go list -m all` / SCA** | аудит-инструменты видят только то, что взято |
| **Честная маркировка «эксперимент»** | `skit/x@v0.x` рядом со стабильным `skit@v1` — без риска утянуть весь SDK в v0 |

### 4.3. Против многих модулей (риски, которые надо считать)

| − | Пояснение |
|---|---|
| **Две копии типа в бинаре** ⚠️⚠️ | если `errs` — отдельный модуль и когда-нибудь уйдёт в `/v2`: потребитель с `skit/rest@v1` (→`errs` v1) и `skit/grpckit@v2` (→`errs/v2`) получит **два разных** `*errs.Error`. `errors.As` молча вернёт `false`. Ошибка без сообщения компилятора. **Отсюда правило 2 из §0** |
| **Стоимость релиза × N** | N тегов вида `db/v1.2.0`, N прогонов `gorelease`, N наборов release notes. Для одного мейнтейнера — главный аргумент против |
| **Сквозные изменения становятся многорелизными** | пока API активно двигается (а он двигается: 18 пакетов за релиз v0.6→v0.7), это резко тормозит. **Поэтому разбивать сейчас — рано** |
| **`go.work` обязателен** | иначе локальная разработка через `replace` × N |
| **Потребитель ведёт N `require`** | и может собрать несовместимый набор версий |
| **Skew — не теория** | прямо в `go.mod` skit: `testcontainers-go v0.43.0` + `testcontainers-go/modules/postgres v0.42.0`. Разные версии одного проекта |

### 4.4. Урок из OpenTelemetry (он прямо в нашем `go.mod`)

otel — ближайший аналог: SDK, разбитый на модули по пакетам. Смотрим, что из этого
вышло, по нашему собственному `go.mod`:

```
go.opentelemetry.io/otel                                   v1.44.0
go.opentelemetry.io/otel/sdk                               v1.44.0
go.opentelemetry.io/otel/trace                             v1.44.0
go.opentelemetry.io/otel/metric                            v1.44.0   (indirect)
go.opentelemetry.io/otel/exporters/otlp/otlptrace          v1.44.0   (indirect)
go.opentelemetry.io/otel/exporters/.../otlptracegrpc       v1.44.0
── а вот это уже другая каденция ──
go.opentelemetry.io/contrib/instrumentation/.../otelhttp   v0.69.0
go.opentelemetry.io/proto/otlp                             v1.10.0
go.opentelemetry.io/auto/sdk                               v1.2.1
```

**Ядро разбито на 6 модулей и всё равно релизится строго синхронно — v1.44.0 везде.**
Пользователь не выигрывает ничего, а платит шестью `require` и риском рассинхрона
(известный класс поломок сборки otel). Отдельная версия появляется только там, где
каденция **действительно** другая: `contrib`, `proto`, `auto`.

**Вывод:** дробить надо **по каденции, а не по пакетам.** Если два модуля всегда
релизятся вместе — это один модуль.

---

## 5. Рекомендуемая архитектура модулей

### 5.1. Целевая раскладка — 4 модуля

Однонаправленный DAG, проверен по §2.1: обратных рёбер нет.

```
          ┌──────────────────────────────────────────┐
          │  github.com/assanoff/skit        (ядро)  │  ← НИКОГДА не /v2
          │  errs logger retry safetick httpw dim    │
          │  closer config page order query worker   │
          │  health httpserver apitest migrate       │
          │  otel otel/otlp metrics middleware       │
          │  httplog rest rest/router rest/mid       │
          │  i18n auth httpclient debugsrv eventbus  │
          │  cron poller app  +  cmd/skit (CLI)      │
          └────────┬───────────────┬────────────┬────┘
                   │               │            │
       ┌───────────▼──────┐  ┌─────▼─────────┐  │
       │ skit/db          │  │ skit/grpckit  │  │
       │ dbx dbtest lock  │  │ grpcserver    │  │
       │ chx              │  │ grpctest      │  │
       └───────────┬──────┘  │ grpcgateway   │  │
                   │         └───────────────┘  │
       ┌───────────▼────────────────────────────▼──┐
       │ skit/msg                                  │
       │ broker broker/kafka broker/rabbitmq       │
       │ queue outbox                              │
       └───────────┬───────────────────────────────┘
                   │
       (этап D, по факту churn'а)
       ┌───────────▼──────────┐  ┌──────────────────┐
       │ skit/auditlog        │  │ skit/translation │
       └──────────────────────┘  └──────────────────┘
```

### 5.2. Почему именно так

| модуль | критерий «тяжесть» | критерий «каденция» | критерий «опциональность» |
|---|---|---|---|
| `skit` (ядро) | — | — | — (базис) |
| `skit/db` | pgx, sqlx, **testcontainers**, redis, ClickHouse, goose | правился 4 раза за 8 релизов | нужен не всем (есть сервисы без БД) |
| `skit/msg` | kafka-go, amqp091, go-rabbitmq | — | ✔ нужен меньшинству |
| `skit/grpckit` | grpc, protobuf, protovalidate, grpc-gateway — **~⅓ дерева зависимостей** | следует за релизами grpc-go | ✔ REST-only сервисы не платят |

`auditlog` и `translation` — кандидаты этапа D: они **опциональны** и у них
**своя каденция** (`translation` — лидер churn'а: 3 релиза из последних 4), но у них
нет тяжёлых зависимостей (`translation` → 1 indirect!). Два критерия из трёх — можно
вынести, можно оставить. Решать по факту.

### 5.3. Что **не** получает своего модуля и почему

* **Хелперы** (`safetick` 84 LOC, `retry` 241, `httpw` 106, `to` 72, `apitest` 164,
  `order` 101, `page` 210, `closer` 269, `dim` 221) — ноль тяжёлых зависимостей,
  ноль отдельной каденции. Отдельный `go.mod` на 84 строки кода — чистый оверхед на
  релиз. **Все в ядро.** Это прямой ответ на вопрос «что делать со вспомогательными
  модулями»: их не существует как модулей, они — ядро.
* **`cmd/skit` (CLI)** — оставить в корневом модуле. Единственная внешняя зависимость —
  `go-flags`, шаблоны `embed`-нутые. Отдельный модуль дал бы уродливый тег
  `cmd/skit/v1.0.0` и ничего не купил: благодаря pruning `go install
  github.com/assanoff/skit/cmd/skit@v1` и так собирается легко.
* **`provider`** — не модуль, а кандидат на выселение в шаблоны CLI (A2). Он единственный
  создаёт ребро «ядро → все спутники» и ломает DAG.
* **`dbtest` / `apitest` / `grpctest`** — не отдельный модуль `skit/test`, а по домам:
  `dbtest` → `skit/db` (тянет testcontainers, 63 indirect), `grpctest` → `skit/grpckit`,
  `apitest` → ядро (чистый stdlib).

### 5.4. Механика «много модулей в одном репозитории»

Go поддерживает это нативно, подводные камни известные:

* **Теги с префиксом директории**: `db/v1.2.0`, `msg/v0.4.0`, корневой — `v1.2.0`.
* **Подкаталог с собственным `go.mod` автоматически исключается** из родительского
  модуля — специальных `exclude` не нужно.
* **`go.work` в корне** для локальной разработки (иначе `replace` × N в каждом модуле).
  В релизных артефактах `go.work` не участвует.
* **CI: матрица по модулям** — `build/vet/test/lint/gorelease` для каждого.
* **Порядок релиза строго снизу вверх**: `skit` → `skit/db` → `skit/msg`. Обратных
  рёбер быть не должно — иначе получится version cycle между модулями одного репо
  (легально в Go, но выпускать мучительно).
* **`retract`** в каждом `go.mod` — для отзыва битых тегов.

---

## 6. Как не ломать API после 1.0.0 (это важнее модулей)

### 6.1. Проблема №1: нет `internal/`

Сейчас в SDK **ноль** `internal/`-пакетов (единственный `internal` — внутри шаблонов
CLI, то есть в генерируемом коде). Значит, на 1.0.0 замораживается **всё**: ~752
экспортированных объявления в 56 пакетах.

Топ по размеру поверхности:

| пакет | экспортированных объявлений |
|---|---|
| `translation` | 104 |
| `outbox` | 76 |
| `worker` | 61 |
| `dbx` | 54 |
| `auditlog` | 46 |
| `middleware` / `logger` | 41 |
| `queue` | 37 |

**Действие до 1.0.0:** пройти по каждому пакету и утащить в `internal/` всё, что не
должно быть контрактом. Это самая дешёвая и самая результативная мера из всего
документа: каждое объявление, спрятанное сегодня, — это степень свободы навсегда.
Ориентир: у `outbox` и `translation` реально «пользовательскими» являются десятки, а не
сотни имён.

### 6.2. Правила add-only

1. **Никогда не удалять и не менять сигнатуру экспортированного.** Только добавлять.
   Устаревшее помечать `// Deprecated: use X instead.` и оставлять навсегда.
2. **Функциональные опции вместо параметров.** `New(cfg, opts ...Option)` расширяется
   без слома; `New(a, b, c)` — нет. В `dim`, `logger`, `rest` паттерн уже есть — довести
   до всех конструкторов.
3. **Экспортированные интерфейсы — маленькие и окончательные.** Добавление метода в
   публичный интерфейс — breaking change для всех, кто его реализует. Сейчас интерфейсы
   уже узкие (`worker.Runnable` — 3 метода, `lock.Locker`, `queue.Queue`,
   `broker.Publisher`) — это хорошо, зафиксировать как правило.
4. **Структурная совместимость дороже номинальной.** `httpserver`, `grpcserver`,
   `debugsrv`, `cron`, `poller`, `grpcgateway` реализуют `worker.Runnable` **не
   импортируя `worker`** — интерфейс удовлетворяется структурно. Приём отличный,
   применять осознанно: он позволяет типам жить в разных модулях без риска из §4.3.
5. **Осторожно с публичными структурами.** Добавление поля ломает unkeyed-литералы.
   Если структура заполняется пользователем (`dbx.Config`, `broker` envelope) — либо
   документировать «только keyed», либо добавить неэкспортируемое нулевое поле.
6. **Не экспортировать чужие типы в сигнатурах, если можно иначе.** Сейчас
   `dbx.WithinTran` принимает `*sqlx.DB`/`*sqlx.Tx` — значит мажор sqlx станет нашим
   мажором. Для `dbx` уже есть интерфейсы `Beginner`/`CommitRollbacker` — расширить
   практику.

### 6.3. Alias-слой: как переносить пакеты, не ломая пользователей

Главный инструмент для всей реорганизации из §2.4. Go-алиасы типов прозрачны — старое
и новое имя это **один и тот же тип**, никакой конверсии и никаких двух копий:

```go
// page/page.go — остаётся навсегда как тонкий фасад
//
// Deprecated: moved to github.com/assanoff/skit/list. This package is a
// compatibility shim and will be kept for the lifetime of v1.
package page

import "github.com/assanoff/skit/list"

type Page = list.Page                        // alias, не новый тип

func Parse(p, rpp string) (Page, error) { return list.Parse(p, rpp) }
func New(n, rpp int) Page                { return list.New(n, rpp) }
```

Так можно выполнить **все** перестановки из §2.4 (`order`+`page`+`query` → `list`,
`to` → `rest`, `dbx/dialect` → `dbx`, `httpmw` → `httpclient`, `auditlog/db` →
`auditlog/auditpg`) **не ломая никого** — даже после 1.0.0. Ограничения: алиас работает
для типов и через обёртки для функций; для констант и переменных — `const X = list.X` /
`var X = list.X`.

### 6.4. Когда сломать всё-таки придётся: версионирование на уровне пакета

Прежде чем уводить модуль в `/v2`, есть более дешёвый ход — новый **пакет** внутри того
же модуля. Ровно так поступил stdlib с `math/rand/v2`.

```
rest/          v1 API — живёт вечно
restv2/        новый API
rest/adapt/    адаптеры restv2.HandlerFunc ↔ rest.HandlerFunc
```

Преимущества перед `/v2` модуля: пользователи мигрируют **по пакетам**, обе версии
сосуществуют в одном бинаре, а адаптеры между ними можно написать (оба пакета в одном
модуле — нет проблемы «двух копий типа»). Именовать без слэша — `restv2`, а не
`rest/v2`: путь с `/vN` резолвер модулей пытается трактовать как мажорный суффикс
модуля.

`/v2` всего модуля оставить как последнее средство, для полной смены философии.

### 6.5. CI-гейт

`gorelease` уже стоит и на v1+ падает на непроходящих изменениях (см. `RELEASING.md`) —
это правильная база. Добавить:

* **`apidiff`-отчёт как артефакт PR**: в комментарии видно, что именно добавилось/ушло,
  до мержа, а не на релизе.
* **проверку «нет новых экспортированных имён без doc-комментария»** — каждое новое
  имя это вечное обязательство, оно должно быть осознанным.
* **после разбиения — матрицу `gorelease` по модулям** + проверку, что нет обратных
  рёбер между модулями (простой скрипт поверх `go list`).

---

## 7. План работ

### Этап A — гигиена пакетов (сейчас, v0.12–v0.13)

Всё breaking, поэтому **до** 1.0.0. Порядок по убыванию отдачи:

1. **A1**: разделить `otel` → `otel` (API) + `otel/otlp` (бутстрап). Убирает gRPC из
   графа `middleware`. *Самое выгодное изменение в списке.*
2. **A9**: вынести `errs.Check`/`FieldError` в `validate`; `errs` становится
   zero-dependency, как `logger`.
3. **A2**: `provider` → в шаблоны CLI. Развязывает будущий DAG модулей.
4. **A3/A4/A5**: `to` → `rest` (или удалить), `dbx/dialect` → `dbx`,
   `httpmw` → `httpclient`.
5. **`order`+`page`+`query` → `list`** (если решаем объединять; §2.5).
6. **A6/A7**: `auditlog/mocks` → `auditlog/auditmock`, выровнять
   `auditlog/db` ↔ `translation/postgres`.
7. **A8**: определить статус `chx`, `poller`, `to`.

### Этап B — заморозка API (перед 1.0.0)

1. Проход по всем 56 пакетам: **всё, что не контракт, — в `internal/`.** Начать с
   `translation` (104), `outbox` (76), `worker` (61), `dbx` (54).
2. Довести функциональные опции до всех публичных конструкторов.
3. Зафиксировать правила §6.2 в `CONTRIBUTING.md`.
4. `apidiff`-отчёт в PR.
5. Записать политику совместимости в `README.md`: что гарантируем, что нет, что
   помечено experimental.

### Этап C — разбиение (в момент 1.0.0)

1. `go.work` в корне.
2. Выделить `skit/db` → тег `db/v1.0.0`.
3. Выделить `skit/msg` → `msg/v1.0.0`.
4. Выделить `skit/grpckit` → `grpckit/v1.0.0`.
5. Корень → `v1.0.0`.
6. Обновить `cmd/skit`: генерируемый `go.mod` получает нужные `require` в зависимости от
   выбранных генераторов (`--rest` не тянет `skit/grpckit`).
7. Обновить `RELEASING.md`: порядок релиза снизу вверх, теги с префиксом.
8. CI-матрица по модулям.

**Обратной совместимости при C не теряется**, если импорт-пути не меняются. Вот тут
важная тонкость: `github.com/assanoff/skit/dbx` при выносе в модуль
`github.com/assanoff/skit/db` **сменит путь** на `.../db/dbx`. Варианты:

* **(рекомендуется)** делать вынос вместе с этапом A, пока мы на v0.x и ломать можно;
* либо назвать модули так, чтобы пути совпали: модуль `github.com/assanoff/skit/dbx`
  ровно на директории `dbx/` — тогда путь не меняется, но модулей становится
  по числу пакетов (обратно к тому, чего мы избегаем);
* либо оставить в корневом модуле shim-пакет `dbx` с алиасами на `db/dbx` (§6.3) — но
  тогда корень зависит от спутника, обратное ребро, **нельзя**.

→ **Поэтому вынос модулей должен произойти до 1.0.0, а не после.** Этап C фактически
надо выполнять как часть подготовки к 1.0.0, а не после релиза.

### Этап D — по факту

Смотрим churn первых 6–12 месяцев после 1.0.0. Если `translation`/`auditlog` продолжают
релизиться отдельно — выносим. Если нет — оставляем в ядре.

---

## 8. Приложение: таблица пакетов

`LOC` — прод-код без тестов. `fan-in` — транзитивный, по всему репозиторию.

| пакет | LOC | внутренние зависимости | fan-in | модуль (план) |
|---|---:|---|---:|---|
| `logger` | 626 | — | 23 | skit |
| `errs` | 416 | — | 21 | skit |
| `retry` | 241 | — | 17 | skit |
| `safetick` | 84 | — | 16 | skit |
| `httpw` | 106 | — | 4 | skit |
| `dim` | 221 | — | 2 | skit |
| `closer` | 269 | — | 1 | skit |
| `page` | 210 | — | 1 | skit (→ `list`?) |
| `order` | 101 | — | 0 | skit (→ `list`?) |
| `query` | 244 | errs, page | 0 | skit (→ `list`?) |
| `config` | 111 | — | 0 | skit |
| `health` | 94 | — | 0 | skit |
| `httpserver` | 171 | — | 1 | skit |
| `apitest` | 164 | — | 0 | skit |
| `migrate` | 143 | — | 2 | skit |
| `otel` | 363 | — | 5 | skit (**разделить**, A1) |
| `translation` | 1172 | — | 2 | skit / `skit/translation` |
| `grpcgateway` | 139 | — | 0 | skit/grpckit |
| `dbx/dialect` | 72 | — | 0 | → влить в `dbx` (A4) |
| `worker` | 823 | errs, retry, safetick | 4 | skit |
| `rest` | 286 | errs | 6 | skit |
| `rest/router` | 306 | rest | 0 | skit |
| `rest/mid` | 452 | errs, i18n, logger, rest | 0 | skit |
| `middleware` | 469 | httpw, logger, otel | 0 | skit |
| `httplog` | 781 | httpw, logger | 0 | skit |
| `metrics` | 175 | httpw | 1 | skit |
| `i18n` | 230 | errs | 2 | skit |
| `auth` | 535 | errs, logger, rest | 0 | skit |
| `httpmw` | 319 | retry | 1 | → влить в `httpclient` (A5) |
| `httpclient` | 219 | httpmw | 0 | skit |
| `debugsrv` | 186 | httpserver | 0 | skit |
| `eventbus` | 233 | logger | 2 | skit |
| `cron` | 129 | safetick | 0 | skit |
| `poller` | 150 | safetick | 0 | skit (A8) |
| `app` | 204 | closer, dim, worker | 0 | skit |
| `to` | 72 | rest | **0** | → влить в `rest` / удалить (A3) |
| `cmd/skit` | 1914 | — | 0 | skit |
| `dbx` | 923 | logger, retry | 12 | skit/db |
| `dbtest` | 169 | dbx, migrate | 0 | skit/db |
| `lock` | 194 | dbx | 0 | skit/db |
| `chx` | 147 | migrate | 0 | skit/db (A8) |
| `broker` | 274 | safetick | 4 | skit/msg |
| `broker/kafka` | 346 | broker, logger | 0 | skit/msg |
| `broker/rabbitmq` | 462 | broker, logger | 1 | skit/msg |
| `queue` | 725 | dbx, logger, retry, worker | 1 | skit/msg |
| `outbox` | 1391 | broker, dbx, logger, metrics, otel, retry, worker | 0 | skit/msg |
| `grpcserver` | 623 | errs, logger, otel | 1 | skit/grpckit |
| `grpctest` | 80 | grpcserver, logger | 0 | skit/grpckit |
| `auditlog` | 982 | dbx, errs, logger, safetick | 5 | skit / `skit/auditlog` |
| `auditlog/db` | 271 | auditlog, dbx, logger | 0 | ↑ (переименовать, A7) |
| `auditlog/auditbus` | 76 | auditlog, eventbus | 1 | ↑ |
| `auditlog/auditqueue` | 140 | auditlog, auditbus, logger, queue, worker | 0 | ↑ |
| `auditlog/auditrest` | 208 | auditlog, errs, rest | 0 | ↑ |
| `auditlog/mocks` | 475 | auditlog | 0 | ↑ (переименовать, A6) |
| `translation/postgres` | 278 | dbx, logger, translation | 0 | ↑ translation |
| `translation/translationrest` | 112 | logger, rest, translation | 0 | ↑ translation |
| `provider` | 256 | broker/rabbitmq, dbx, dim, i18n, logger, otel | 0 | → в шаблоны CLI (A2) |

**Пакетов без `doc.go`** (нарушение конвенции репозитория): `auditlog/auditbus`,
`auditlog/auditqueue`, `auditlog/auditrest`, `auditlog/db`, `auditlog/mocks`, `chx`,
`cron`, `grpctest`, `httpclient`, `lock`, `translation/postgres`.

---

## 9. Открытые вопросы к решению

1. **`order`+`page`+`query` → `list`?** Даёт −2 пакета и правильную семантику, но
   меняет пути. Дёшево сейчас, дорого после 1.0.0 (нужен alias-слой).
2. **`auditlog` и `translation` — в ядро или отдельные модули?** Два критерия из трёх.
   Склоняюсь к «в ядро на 1.0.0, вынести на этапе D, если churn подтвердится».
3. **Имя модуля gRPC**: `skit/grpckit` / `skit/grpcx` / `skit/rpc`. Голое `skit/grpc`
   конфликтует с привычкой писать `import "google.golang.org/grpc"` — будет постоянный
   алиасинг у пользователей.
4. **Судьба `to`, `chx`, `poller`** — доводим до продакшн-статуса или помечаем
   experimental (тогда нужен отдельный `skit/x` на v0.x).
5. **Сколько поверхности реально уходит в `internal/`?** До оценки этого числа дату
   1.0.0 назначать рано — это главный необратимый шаг.
