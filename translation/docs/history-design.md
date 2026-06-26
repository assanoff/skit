#### Схема базы данных для истории

```sql
create table translation_history (
    id serial primary key,
    model_name varchar(100) not null,
    column_name varchar(100) not null,
    key_id varchar(255) not null,
    language_id char(2) not null,
    old_value text,
    new_value text,
    changed_at timestamp not null default current_timestamp,
    changed_by integer not null,
    
    foreign key (language_id) references language(language_id)
);

create index idx_translation_history_lookup on translation_history(model_name, key_id, language_id, changed_at desc);
create index idx_translation_history_user on translation_history(changed_by, changed_at desc);
```

#### Типы данных

```go
type TranslationHistory struct {
    ID         int       `json:"id" db:"id"`
    ModelName  string    `json:"model_name" db:"model_name"`
    ColumnName string    `json:"column_name" db:"column_name"`
    KeyID      string    `json:"key_id" db:"key_id"`
    LanguageID string    `json:"language_id" db:"language_id"`
    OldValue   *string   `json:"old_value" db:"old_value"`   // nullable для новых записей
    NewValue   string    `json:"new_value" db:"new_value"`
    ChangedAt  time.Time `json:"changed_at" db:"changed_at"`
    ChangedBy  int       `json:"changed_by" db:"changed_by"`
}

type HistoryFilter struct {
    ModelName  string
    KeyID      string
    LanguageID string
    ChangedBy  *int
    DateFrom   *time.Time
    DateTo     *time.Time
    Limit      int
    Offset     int
}
```

#### Расширение Store интерфейса

```go
type Store interface {
    // ... существующие методы ...
    
    // История изменений
    SaveTranslationHistory(ctx context.Context, history TranslationHistory) error
    GetTranslationHistory(ctx context.Context, filter HistoryFilter) ([]TranslationHistory, error)
    GetTranslationHistoryByID(ctx context.Context, id int) (*TranslationHistory, error)
}
```

#### Расширение Translator

```go
type Translator struct {
    db              *sql.DB
    store           Store
    defaultLang     Language
    supportedLangs  []Language
    cache           Cache
    trackHistory    bool  // включить/выключить отслеживание истории
}

// Получение истории изменений для конкретного объекта
func (t *Translator) GetHistory(ctx context.Context, model Translatable, lang Language) ([]TranslationHistory, error) {
    modelName, keyID := model.GetTranslationKey()
    
    filter := HistoryFilter{
        ModelName:  modelName,
        KeyID:      keyID,
        LanguageID: lang.Code,
        Limit:      100,
    }
    
    return t.store.GetTranslationHistory(ctx, filter)
}

// Получение истории с фильтрацией
func (t *Translator) GetHistoryWithFilter(ctx context.Context, filter HistoryFilter) ([]TranslationHistory, error) {
    return t.store.GetTranslationHistory(ctx, filter)
}

// Откат к предыдущей версии
func (t *Translator) RollbackToHistory(ctx context.Context, historyID int, userID int) error {
    // Получаем запись из истории
    history, err := t.store.GetTranslationHistoryByID(ctx, historyID)
    if err != nil {
        return err
    }
    
    if history.OldValue == nil {
        return errors.New("cannot rollback: no previous value")
    }
    
    // Создаем данные для отката
    data := TranslationData{
        ModelName: history.ModelName,
        KeyID:     history.KeyID,
        Language:  Language{Code: history.LanguageID},
        Columns: map[string]string{
            history.ColumnName: *history.OldValue,
        },
    }
    
    // Сохраняем откат (это создаст новую запись в истории)
    return t.saveWithHistory(ctx, data, userID)
}
```

####  Метод Save с историей

```go
// Внутренний метод сохранения с отслеживанием истории
func (t *Translator) saveWithHistory(ctx context.Context, data TranslationData, userID int) error {
    // Если история отключена, просто сохраняем
    if !t.trackHistory {
        return t.store.SaveTranslation(ctx, data)
    }
    
    // Получаем текущие значения для сравнения
    currentTranslations, err := t.store.GetTranslations(ctx, data.ModelName, data.KeyID, data.Language)
    if err != nil && err != ErrTranslationNotFound {
        return err
    }
    
    tx, err := t.store.BeginTx(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback()
    
    if err := tx.SaveTranslation(ctx, data); err != nil {
        return err
    }
    
    // Записываем историю
    for columnName, newValue := range data.Columns {
        var oldValue *string
        if current, exists := currentTranslations[columnName]; exists {
            oldValue = &current
        }
        
        // Пропускаем если значение не изменилось
        if oldValue != nil && *oldValue == newValue {
            continue
        }
        
        history := TranslationHistory{
            ModelName:  data.ModelName,
            ColumnName: columnName,
            KeyID:      data.KeyID,
            LanguageID: data.Language.Code,
            OldValue:   oldValue,
            NewValue:   newValue,
            ChangedAt:  time.Now(),
            ChangedBy:  userID,
        }
        
        if err := tx.SaveTranslationHistory(ctx, history); err != nil {
            return err
        }
    }
    
    return tx.Commit()
}

// в публичный метод Save добавить userID или сделать метод обертку
func (t *Translator) Save(ctx context.Context, lang Language, model Translatable, userID int) error {
    modelName, keyID := model.GetTranslationKey()
    
    fields, err := t.parseTranslateTags(model)
    if err != nil {
        return err
    }
    
    columns := make(map[string]string)
    for _, field := range fields {
        if !field.isPrimary {
            columns[field.columnName] = field.value
        }
    }
    
    data := TranslationData{
        ModelName: modelName,
        KeyID:     keyID,
        Language:  lang,
        Columns:   columns,
    }
    
    return t.saveWithHistory(ctx, data, userID)
}
```

#### Пример

```go
package main

import (
    "context"
    "fmt"
    "time"
    "your-project/translate"
)

func main() {
    // Инициализация с включенной историей
    translator, _ := translation.New(translation.Config{
        DB:              db,
        DefaultLanguage: translation.LanguageRu,
        SupportedLangs:  []translation.Language{translation.LanguageRu, translation.LanguageKk},
        TrackHistory:    true,  // включаем отслеживание истории
    })
    
    ctx := context.Background()
    userID := 42  


    // 1. Сохранение перевода с автоматической записью в историю
    product := &Product{
        ID:          "123",
        Title:       "Заголовок на казахском",
        Description: "Описание на казахском",
    }
    
    err := translator.Save(ctx, translation.LanguageKk, product, userID)
    if err != nil {
        fmt.Printf("Error saving: %v\n", err)
    }
    
    // 2. Получение истории изменений для объекта
    history, err := translator.GetHistory(ctx, product, translation.LanguageKk)
    if err != nil {
        fmt.Printf("Error getting history: %v\n", err)
    }
    
    fmt.Println("История изменений:")
    for _, h := range history {
        oldVal := "null"
        if h.OldValue != nil {
            oldVal = *h.OldValue
        }
        fmt.Printf("[%s] %s.%s: '%s' -> '%s' (user: %d)\n",
            h.ChangedAt.Format("2006-01-02 15:04:05"),
            h.ModelName,
            h.ColumnName,
            oldVal,
            h.NewValue,
            h.ChangedBy,
        )
    }
    
    // 3. Получение истории с фильтрацией
    dateFrom := time.Now().AddDate(0, -1, 0) // последний месяц
    filter := translation.HistoryFilter{
        ModelName:  "products",
        KeyID:      "123",
        LanguageID: "kk",
        DateFrom:   &dateFrom,
        Limit:      50,
    }
    
    filteredHistory, err := translator.GetHistoryWithFilter(ctx, filter)
    if err != nil {
        fmt.Printf("Error: %v\n", err)
    }
    
    // 4. Откат к предыдущей версии
    if len(history) > 0 {
        lastHistoryID := history[0].ID
        err = translator.RollbackToHistory(ctx, lastHistoryID, userID)
        if err != nil {
            fmt.Printf("Error rolling back: %v\n", err)
        } else {
            fmt.Println("Successfully rolled back to previous version")
        }
    }
    
    // 5. История изменений конкретного пользователя
    adminUserID := 1
    adminFilter := translation.HistoryFilter{
        ChangedBy: &adminUserID,
        Limit:     100,
    }
    
    adminHistory, err := translator.GetHistoryWithFilter(ctx, adminFilter)
    fmt.Printf("Пользователь %d сделал %d изменений\n", adminUserID, len(adminHistory))
}
```

#### Пример реализации Store для PostgreSQL

```go
// postgres/store.go
type PostgresStore struct {
    db *sql.DB
}

func (s *PostgresStore) SaveTranslationHistory(ctx context.Context, history translation.TranslationHistory) error {
    query := `
        INSERT INTO translation_history 
        (model_name, column_name, key_id, language_id, old_value, new_value, changed_at, changed_by)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    `
    
    _, err := s.db.ExecContext(ctx, query,
        history.ModelName,
        history.ColumnName,
        history.KeyID,
        history.LanguageID,
        history.OldValue,
        history.NewValue,
        history.ChangedAt,
        history.ChangedBy,
    )
    
    return err
}

func (s *PostgresStore) GetTranslationHistory(ctx context.Context, filter translation.HistoryFilter) ([]translation.TranslationHistory, error) {
    query := `
        SELECT id, model_name, column_name, key_id, language_id, old_value, new_value, changed_at, changed_by
        FROM translation_history
        WHERE 1=1
    `
    args := []any{}
    argPos := 1
    
    if filter.ModelName != "" {
        query += fmt.Sprintf(" AND model_name = $%d", argPos)
        args = append(args, filter.ModelName)
        argPos++
    }
    
    if filter.KeyID != "" {
        query += fmt.Sprintf(" AND key_id = $%d", argPos)
        args = append(args, filter.KeyID)
        argPos++
    }
    
    if filter.LanguageID != "" {
        query += fmt.Sprintf(" AND language_id = $%d", argPos)
        args = append(args, filter.LanguageID)
        argPos++
    }
    
    if filter.ChangedBy != nil {
        query += fmt.Sprintf(" AND changed_by = $%d", argPos)
        args = append(args, *filter.ChangedBy)
        argPos++
    }
    
    if filter.DateFrom != nil {
        query += fmt.Sprintf(" AND changed_at >= $%d", argPos)
        args = append(args, *filter.DateFrom)
        argPos++
    }
    
    if filter.DateTo != nil {
        query += fmt.Sprintf(" AND changed_at <= $%d", argPos)
        args = append(args, *filter.DateTo)
        argPos++
    }
    
    query += " ORDER BY changed_at DESC"
    
    if filter.Limit > 0 {
        query += fmt.Sprintf(" LIMIT $%d", argPos)
        args = append(args, filter.Limit)
        argPos++
    }
    
    if filter.Offset > 0 {
        query += fmt.Sprintf(" OFFSET $%d", argPos)
        args = append(args, filter.Offset)
        argPos++
    }
    
    rows, err := s.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    var history []translation.TranslationHistory
    for rows.Next() {
        var h translation.TranslationHistory
        err := rows.Scan(
            &h.ID,
            &h.ModelName,
            &h.ColumnName,
            &h.KeyID,
            &h.LanguageID,
            &h.OldValue,
            &h.NewValue,
            &h.ChangedAt,
            &h.ChangedBy,
        )
        if err != nil {
            return nil, err
        }
        history = append(history, h)
    }
    
    return history, rows.Err()
}

func (s *PostgresStore) GetTranslationHistoryByID(ctx context.Context, id int) (*translation.TranslationHistory, error) {
    query := `
        SELECT id, model_name, column_name, key_id, language_id, old_value, new_value, changed_at, changed_by
        FROM translation_history
        WHERE id = $1
    `
    
    var h translation.TranslationHistory
    err := s.db.QueryRowContext(ctx, query, id).Scan(
        &h.ID,
        &h.ModelName,
        &h.ColumnName,
        &h.KeyID,
        &h.LanguageID,
        &h.OldValue,
        &h.NewValue,
        &h.ChangedAt,
        &h.ChangedBy,
    )
    
    if err == sql.ErrNoRows {
        return nil, translation.ErrTranslationNotFound
    }
    
    return &h, err
}
```

#### API эндпоинты для истории

```go
// GET /api/v1/translate/{id}/history?language=kk&limit=50
func GetTranslationHistoryHandler(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    lang := r.URL.Query().Get("language")
    limitStr := r.URL.Query().Get("limit")
    
    limit := 50
    if limitStr != "" {
        limit, _ = strconv.Atoi(limitStr)
    }
    
    filter := translation.HistoryFilter{
        ModelName:  "products",
        KeyID:      id,
        LanguageID: lang,
        Limit:      limit,
    }
    
    history, err := translator.GetHistoryWithFilter(r.Context(), filter)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    json.NewEncoder(w).Encode(history)
}

// POST /api/v1/translate/{id}/rollback
func RollbackTranslationHandler(w http.ResponseWriter, r *http.Request) {
    var req struct {
        HistoryID int `json:"history_id"`
    }
    
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    userID := getUserIDFromContext(r.Context())
    
    err := translator.RollbackToHistory(r.Context(), req.HistoryID, userID)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    w.WriteHeader(http.StatusOK)
}
```