# Background
## Основная концепция


Основные концепции и дизайн пакета переводов основаны на методике переводов, которая использовадлась в команде в экосистеме Choco.

Проблемы старого подхода
 - сложная схема данных, много таблиц, соответственно много операций JOIN
 - заполнение метаданных переводов производилось через миграции
 - части логики переводов растгивались по сервисам и репозиториям бизнес логики
 - слабая инкапсуляция, API
 - отсутствует документация
 - нет тестов


# Proposal
- **Декларативность** за счет использование struct tags для разметки переводимых полей (аналогично `json`, `db` ...).
Добавлен декларативный способ определения метаданных в описании объекта, поля которых нужно переводить.
Разметка полей тегами структуры — идиоматичный и часто используемый подход в экосистеме Go 
для разметки полей метаданными, например, в пакетах `json`, `jsonapi`, `db`, `bson`, `validate`, `xml` и других.
Возможность перевода вложенных списков и вложенных структур 


- **Типобезопасность** - проверка на этапе компиляции через интерфейс `Translatable`
Добавлена типобезопасность для определения объектов, которые нужно переводить, и проверяется во время компиляции.

- **Автоматизация** - поля автоматически переводятся на переданный язык.
Работа с переводами вынесена в слой представления данных (API REST) с помощью разметки полей тегами структуры ответа. Бизнес-логика остается незатронутой и не перегружается лишними методами. Работа с переводами реализована (инкапсулирована) в одном месте и осуществляется посредством вызова общих API методов пакета translation.


- **Независимость от БД** - интерфейс `Store` позволяет использовать любую базу данных.
Упрощена схема хранения данных о переводах в базе данных переводов.
Не зависит от конкретной базы данных.

- **Производительность** - реализованы эффективные batch операции через `TranslateSlice()` и `GetTranslationsBatch()`.

- **Простота тестирования** - mock реализации для unit-тестов

**Возможные улучшения:**
- Добавить опциональное кеширование. Так как переводы — нечасто меняющиеся значения, эффективность по производительности может быть повышена за счет использования внутреннего кеша, чтобы избежать частого обращения к базе данных за переводами полей объектов.
 - Хранение истории (версиониование) переводов, см history-design.md


# Implementation

## Основной API интерфейс

#### Language
```go
type Language struct {
    Code string `json:"code"` // "ru", "kk"
    Name string `json:"name"` // "Russian", "Kazakh"
}

// Предопределенные языки
var (
    LanguageRu = Language{Code: "ru", Name: "Russian"}
    LanguageKk = Language{Code: "kk", Name: "Kazakh"}
)
```

#### Translatable Interface
Любая модель, которую нужно переводить, должна реализовать этот интерфейс.

```go
// Интерфейс для типобезопасности
type Translatable interface {
    GetTranslationKey() (modelName, keyID string)
}
```

### Конфигурация

```go
type Config struct {
    // Store - реализация интерфейса хранилища
    Store Store
    
    // DefaultLanguage - язык по умолчанию
    DefaultLanguage Language
    
    // SupportedLangs - список всех поддерживаемых языков
    SupportedLangs []Language
}
```

### Основные методы для работы с переводами

#### Перевод моделей

```go
// Translate переводит одну модель на указанный язык
// Если lang == DefaultLanguage, перевод не выполняется
func (t *Translator) Translate(ctx context.Context, model Translatable, lang Language) error

// TranslateSlice переводит несколько моделей за один запрос (batch операция)
// Оптимизирован для списков - делает один запрос к БД для всех моделей
func TranslateSlice[T Translatable](ctx context.Context, t *Translator, models []T, lang Language) error
```

#### CRUD операции для переводов

```go
// Save сохраняет перевод модели на указанный язык
func (t *Translator) Save(ctx context.Context, lang Language, model Translatable) error

// Get получает перевод модели (если не найден, оставляет оригинальные значения)
func (t *Translator) Get(ctx context.Context, lang Language, model Translatable) error

// Delete удаляет перевод модели на указанный язык
func (t *Translator) Delete(ctx context.Context, lang Language, model Translatable) error
```

#### Валидация

```go
// ValidateTranslateTags проверяет корректность тегов translate в структуре
// Рекомендуется вызывать при инициализации приложения
func (t *Translator) ValidateTranslateTags(model Translatable) error

// CheckTranslationsExist проверяет наличие переводов на все поддерживаемые языки
// Используется перед активацией записей (is_active = true) enable/disable
func (t *Translator) CheckTranslationsExist(ctx context.Context, model Translatable) error

// ValidateLanguage проверяет, поддерживается ли указанный язык
func (t *Translator) ValidateLanguage(lang Language) error
```


# Quick Start
## Пример внедрения и использования

### Шаг 1: Определение модели с тегами

У модели уровня представления (API REST) добавляем тег "translate".

**Теги translate:**
- `translate:"primary"` - поле с идентификатором объекта (обязательно)
- `translate:"column_name"` - имя колонки для хранения перевода

```go
package products

import "module/sdk/translate"

// ApiProduct - модель для API уровня представления
type ApiProduct struct {
    ID          string `json:"id" jsonapi:"primary,products" translate:"primary"`
    Name        string `json:"name" jsonapi:"attr,name" translate:"name"`
    Description string `json:"description" jsonapi:"attr,description" translate:"description"`
    AuthorID    int    `json:"author_id"` // Поля без тега translate не переводятся
    IsActive    bool   `json:"is_active"`
}

// Реализация интерфейса Translatable
func (a *ApiProduct) GetTranslationKey() (modelName, keyID string) {
    return "products", a.ID
}
```


```go
// Инициализация translator
    translateStore := postgres.NewStore(db)
    translator, err := translation.New(translation.Config{
        Store:           translateStore,
        DefaultLanguage: translation.LanguageRu,
        SupportedLangs: []translation.Language{
            translation.LanguageRu,
            translation.LanguageKk,
        },
    })
```    

Поля, размеченные тегом translate, будут переведены автоматически в соответствии с языком, переданным от клиента в заголовке X-Language.

Если язык по умолчанию (определяется в пакете translate как translation.DefaultLanguage), то перевод делать не нужно, возвращаем значения на языке по умолчанию.

Если передан неправильный язык, то сервер возвращает данные на языке по умолчанию.


### Работа с переводами в админке (CRUD операции)

На уровне представления сервиса (api REST) добавляем два метода:
 - POST /api/v1/resource/admin/translate
 - GET  /api/v1/resource/admin/translate?filter[model_id]=124&filter[language]=ru


**Модель данных для перевода:**
Добавляем модель данных для перевода.

```go
type ApiModelTranslate struct {
  ID       string                   `jsonapi:"primary,translate" json:"-" translate:"primary"`
  Name     string                   `jsonapi:"attr,title" json:"title" translate:"title"`
  Language *translation.LanguageModel `jsonapi:"relation,language" json:"-"`
}
```


**Метод GET** `/api/v1/resource/admin/translate?filter[model_id]=124&filter[language]=kk` 
возвращает данные для перевода.

**Тело ответа:**
```json
{
    "data": {
        "type": "translate",
        "id": "124",
        "attributes": {
            "title": "value of the model in english",
            "description": "value of the model in english"
        },
        "relationships": {
            "language": {
                "data": {
                    "type": "language",
                    "id": "ru",
                    "name": "Russian"
                }
            }
        }
    }
}
```

**Метод POST** `/api/v1/resource/admin/translate` 
сохраняет данные перевода.

**Тело запроса:**
```json
{
    "data": {
        "type": "translate",
        "id": "124",
        "attributes": {
            "title": "value of the model in kk",
            "description": "value of the model in kk",
            ... другие аттрибуты которые нужно переводить
        },
        "relationships": {
            "language": {
                "data": {
                    "type": "language",
                    "id": "kk",
                    "name": "Kazakh"
                }
            }
        }
    }
}
```


### Валидация и проверка перевода в runtime

При запросе от клиента в ответ бизнес логика должна отправлять только активные записи (is_active = true) и/или не помеченные на удаление (deleted_at is null).

Перед тем как установить запись активной (is_active = true), нужно проверить, есть ли у объекта перевод.

Включение активности производится методами:
- `/api/v1/resource/{id}/enable`
- `/api/v1/resource/{id}/disable`

В методах выполняется получение данных объекта из базы, перевод в модель представления для клиента
и вызов метода:

```go
func (t *Translator) CheckTranslationsExist(ctx context.Context, model Translatable) error
```

Метод вернет ошибку, если не удалось выполнить перевод размеченных полей на все требуемые языки, определенные в пакете translation.SupportedLangs.



### Если на уровне представления меняется транспортный уровень

Если на уровне представления меняется способ взаимодействия с REST на protobuf, то переводы и разметку выносим в модели бизнес-логики или добавяем dto.


## Схема хранения данных о переводах

### Старая схема данных

#### Список языков

```sql
create table if not exists public.language
(
    language_id char(2) not null constraint language_pkey primary key,
    language     varchar(250)
);
```


#### Список таблиц

```sql
public.table_information
(
    table_id uuid not null constraint table_information_pkey primary key,
    table_name       varchar(250)
);
```

#### Список колонок

```sql
public.column_information
(
    column_id uuid not null constraint column_information_pkey primary key,
    column_name       varchar(250),
    translation_enabled bool,
    table_id uuid,

    foreign key (table_id) references table_information(table_id)
);
```

#### Данные перевода: колонка, идентификатор, язык, перевод

```sql
create table if not exists public.translation
(
    column_id uuid not null,
    key_id varchar(255) not null,
    language_id char(2) not null,
    translation_value     varchar,

    foreign key (column_id) references column_information(column_id),
    foreign key (language_id) references language(language_id),
    primary key (column_id, key_id, language_id)
);
```

#### Заполнение метаданных перевода в базе данных (пример)

```sql
values
    ('ru', 'Russian'),
    ('kk', 'Kazakh') ON CONFLICT(language_id) DO NOTHING;

insert into table_information (table_id, table_name)
values
    ('821ea7c7-1810-4fc8-ab44-a5c07de9cbd4', 'products'); -- сопоставляется с тегом translate:"key,products"

insert into column_information (column_id, column_name, translation_enabled, table_id) -- column_id сопоставляется с тегом translate:"title"
values
    ('38cf5846-0d5c-42ff-8e8b-584e835ecedd', 'title', true, '821ea7c7-1810-4fc8-ab44-a5c07de9cbd4');
```

### Новая упрощенная схема хранения данных

В старой схеме данных для каждого перевода нужно 3 JOIN операции.
table_information и column_information фактически дублируют метаданные из кода.

```sql
-- Таблица языков
create table language (
    language_id char(2) primary key,
    name varchar(250)
);

-- Объединенная таблица переводов
create table translation (
    model_name varchar(100) not null,  -- интерфейс GetTranslationKey()
    column_name varchar(100) not null, -- из тега translate:"title"
    key_id varchar(255) not null,      -- из тега translate:"primary"
    language_id char(2) not null,      -- переданный язык из X-Language
    translation_value text,
    created_at timestamp,
    created_by integer,
    updated_at timestamp,
    updated_by integer,
    
    primary key (model_name, column_name, key_id, language_id),
    foreign key (language_id) references language(language_id)
);

-- Индекс для быстрого поиска
create index idx_translation_lookup on translation(key_id, model_name, language_id);
```

По сравнению со старой схемой (с таблицами `table_information` и `column_information`):

1. **Меньше JOIN операций** - все данные в одной таблице
2. **Проще поддержка** - метаданные хранятся в коде, а не дублируются в БД
3. **Быстрее запросы** - один SELECT вместо нескольких JOIN
4. **Легче миграция** - одна таблица вместо трех


### Пример данных

После применения миграции таблица `language` будет содержать:

| language_id | name     |
|-------------|----------|
| ru          | Russian  |
| kk          | Kazakh   |

Пример записи в таблице `translation`:

| model_name | column_name | key_id                               | language_id | translation_value      |
|------------|-------------|--------------------------------------|-------------|------------------------|
| products    | name        | 123e4567-e89b-12d3-a456-426614174000 | kk          | Қазақша атауы          |
| products    | description | 123e4567-e89b-12d3-a456-426614174000 | kk          | Қазақша сипаттамасы    |




### Unit тесты с Mock Translator

```go
package products_test

import (
    "testing"
    "translate-poc/sdk/translation"
)

func TestListProducts(t *testing.T) {
    // Создаем mock translator
    translator := translation.NewMockTranslator()
    
    // Настраиваем тестовые переводы
    testProduct := &ApiProduct{
        ID:          "123",
        Name:        "Казахское название",
        Description: "Казахское описание",
    }
    
    err := translator.Save(context.Background(), translation.LanguageKk, testProduct)
    if err != nil {
        t.Fatal(err)
    }
    
    // Создаем handler с mock translator
    handler := NewHandler(mockCore, translator)
    
    // Тестируем handler
    // ...
}
```


## Структура пакета

```
translate/
├── doc.go             // Документация пакета
├── types.go           // Основные типы
├── errors.go          // Определения ошибок
├── config.go          // Конфигурация
├── translator.go      // Основная логика и публичный API
├── parser.go          // Парсинг struct tags
├── storage.go         // Интерфейс Store для хранилища
├── validator.go       // Валидация тегов и переводов
├── middleware.go      // HTTP middleware для извлечения языка
├── mock.go            // Mock реализации для тестирования
├── postgres/
│   ├── store.go       // Реализация интерфейса Store для PostgreSQL
│   └── migrations/
│       └── 001_create_translation_tables.sql
├── docs/              // Документация
│   ├── README.md
│   ├── translate-design.md
│   └── design-review.md
├── *_test.go          // Unit тесты
└── example_test.go    // Примеры использования
```



## Best Practices

### 1. Хранение оригинального контента

**Всегда хранить оригинальный контент на языке по умолчанию**

```go
//  Правильно
product := &ApiProduct{
    ID:          "123",
    Name:        "Русское название",  // Оригинал на русском
    Description: "Русское описание",
}
// Сохраняем в основную таблицу products

// Затем добавляем переводы
productKk := &ApiProductTranslate{
    ID:          "123",
    Name:        "Казахское название",
    Description: "Казахское описание",
}
translator.Save(ctx, translation.LanguageKk, productKk)
```

### 2. Валидация перед активацией

**Всегда проверять наличие всех переводов** перед установкой активности записи `is_active = true`:

```go
//  Правильно
func EnableProduct(id string) error {
    product := getProductByID(id)
    
    // Проверяем переводы
    if err := translator.CheckTranslationsExist(ctx, product); err != nil {
        return fmt.Errorf("cannot enable: %w", err)
    }
    
    // Активируем только если все переводы есть
    return setActive(id, true)
}

//  Неправильно - активируем без проверки
func EnableProduct(id string) error {
    return setActive(id, true)
}
```

### 3. Использование Batch операций

**Использовать `TranslateSlice()` для списков** вместо цикла с `Translate()`:

```go
//  Правильно - один запрос к БД
products := getProducts()
translation.TranslateSlice(ctx, translator, products, lang)

//  Неправильно - N запросов к БД
for _, product := range products {
    translator.Translate(ctx, product, lang)
}
```

### 4. Fallback

**Не прерывать запрос от клиента при ошибке перевода** - возвращать оригинальные значения:

```go
//  Правильно
if err := translator.Translate(ctx, product, lang); err != nil {
    log.Printf("Translation error: %v", err)
    // Продолжаем с оригинальными значениями
}
respondJSON(w, http.StatusOK, product)

//  Неправильно - прерываем запрос
if err := translator.Translate(ctx, product, lang); err != nil {
    return http.StatusInternalServerError
}
```

### 5. Использование middleware

**Добавить middleware для автоматического извлечения языка**:

```go
//  Правильно
r.Use(translator.HTTPMiddleware)

func Handler(w http.ResponseWriter, r *http.Request) {
    lang := translation.LanguageFromContext(r.Context())
    // Язык уже извлечен и провалидирован
}

//  Неправильно - ручной парсинг в каждом handler
func Handler(w http.ResponseWriter, r *http.Request) {
    langCode := r.Header.Get("X-Language")
    // Нужно валидировать, обрабатывать ошибки и т.д.
}
```

### 6. Тестирование с Mock

**Использовать `NewMockTranslator()` в unit-тестах**:

```go
//  Правильно
func TestHandler(t *testing.T) {
    translator := translation.NewMockTranslator()
    handler := NewHandler(core, translator)
    // Тестируем без реальной БД
}

//  Неправильно - требует реальную БД для каждого теста
func TestHandler(t *testing.T) {
    db := setupRealDB()
    translator := translation.New(...)
    // Медленно и сложно
}
```

### 7. Проверка тегов при тестировании

**Валидировать теги при тестировании**:

```go
 func TestValidateTranslateTags(t *testing.T) {
	translator := translation.NewMockTranslator()

	t.Run("ApiProduct has valid translate tags", func(t *testing.T) {
		apiProduct := &products.ApiProduct{
			ID:          "test-123",
			Name:        "Test Product",
			Description: "Test Description",
		}
		if err := translator.ValidateTranslateTags(apiProduct); err != nil {
			t.Errorf("ApiProduct has invalid translate tags: %v", err)
		}
	})
}
```

### 8. Оптимизация для языка по умолчанию

**Не делать запросы к БД для языка по умолчанию**:

```go
//  Правильно - проверка встроена в Translate()
translator.Translate(ctx, product, lang)
// Если lang == DefaultLanguage, запрос к БД не делается

//  Неправильно - лишняя проверка
if lang != translator.DefaultLanguage() {
    translator.Translate(ctx, product, lang)
}
```