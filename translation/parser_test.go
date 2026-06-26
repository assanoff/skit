package translation

import (
	"errors"
	"testing"
)

type ValidModel struct {
	ID    string `translate:"primary"`
	Title string `translate:"title"`
	Desc  string `translate:"description"`
}

func (v *ValidModel) GetTranslationKey() (modelName, keyID string) {
	return "valid", v.ID
}

type NoPrimaryModel struct {
	Title string `translate:"title"`
}

func (n *NoPrimaryModel) GetTranslationKey() (modelName, keyID string) {
	return "no_primary", ""
}

type MultiplePrimaryModel struct {
	ID1 string `translate:"primary"`
	ID2 string `translate:"primary"`
}

func (m *MultiplePrimaryModel) GetTranslationKey() (modelName, keyID string) {
	return "multiple", m.ID1
}

func TestParseTranslateTags(t *testing.T) {
	tests := []struct {
		name      string
		model     Translatable
		wantErr   error
		wantCount int
	}{
		{
			name:      "valid model",
			model:     &ValidModel{ID: "123", Title: "Test", Desc: "Description"},
			wantErr:   nil,
			wantCount: 3,
		},
		{
			name:    "no primary key",
			model:   &NoPrimaryModel{Title: "Test"},
			wantErr: ErrNoPrimaryKey,
		},
		{
			name:    "multiple primary keys",
			model:   &MultiplePrimaryModel{ID1: "1", ID2: "2"},
			wantErr: ErrMultiplePrimaryKeys,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields, err := parseTranslateTags(tt.model)

			// Check if we expected a specific error
			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("parseTranslateTags() expected error %v, got nil", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("parseTranslateTags() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			// We didn't expect an error
			if err != nil {
				t.Errorf("parseTranslateTags() unexpected error = %v", err)
				return
			}

			// Check field count
			if tt.wantCount > 0 && len(fields) != tt.wantCount {
				t.Errorf("parseTranslateTags() got %d fields, want %d", len(fields), tt.wantCount)
			}
		})
	}
}

func TestParseTranslateTags_FieldInfo(t *testing.T) {
	model := &ValidModel{
		ID:    "123",
		Title: "Test Title",
		Desc:  "Test Description",
	}

	fields, err := parseTranslateTags(model)
	if err != nil {
		t.Fatalf("parseTranslateTags() error = %v", err)
	}

	// Check primary field
	var primaryFound bool
	for _, field := range fields {
		if field.isPrimary {
			primaryFound = true
			if field.fieldName != "ID" {
				t.Errorf("Primary field name = %s, want ID", field.fieldName)
			}
			if field.value != "123" {
				t.Errorf("Primary field value = %s, want 123", field.value)
			}
		}
	}

	if !primaryFound {
		t.Error("Primary field not found")
	}

	// Check title field
	var titleFound bool
	for _, field := range fields {
		if field.columnName == "title" {
			titleFound = true
			if field.fieldName != "Title" {
				t.Errorf("Title field name = %s, want Title", field.fieldName)
			}
			if field.value != "Test Title" {
				t.Errorf("Title field value = %s, want Test Title", field.value)
			}
		}
	}

	if !titleFound {
		t.Error("Title field not found")
	}
}

func TestSetFieldValue(t *testing.T) {
	model := &ValidModel{
		ID:    "123",
		Title: "Original",
		Desc:  "Original",
	}

	// Test setting Title
	err := setFieldValue(model, "Title", "New Title")
	if err != nil {
		t.Errorf("setFieldValue() error = %v", err)
	}
	if model.Title != "New Title" {
		t.Errorf("Title = %s, want New Title", model.Title)
	}

	// Test setting Desc
	err = setFieldValue(model, "Desc", "New Description")
	if err != nil {
		t.Errorf("setFieldValue() error = %v", err)
	}
	if model.Desc != "New Description" {
		t.Errorf("Desc = %s, want New Description", model.Desc)
	}

	// Test invalid field
	err = setFieldValue(model, "NonExistent", "value")
	if err == nil {
		t.Error("Expected error for non-existent field")
	}
}

func TestSetFieldValue_Pointer(t *testing.T) {
	model := &ValidModel{
		ID:    "123",
		Title: "Original",
	}

	// Should work with pointer
	err := setFieldValue(model, "Title", "Updated")
	if err != nil {
		t.Errorf("setFieldValue() with pointer error = %v", err)
	}
	if model.Title != "Updated" {
		t.Errorf("Title = %s, want Updated", model.Title)
	}
}

// Test models for nested struct support
type label struct {
	Title      string `jsonapi:"attr,title" json:"title" translate:"label_title"`
	Background string `jsonapi:"attr,background" json:"background"`
}

type NestedModel struct {
	ID    string `translate:"primary"`
	Title string `translate:"title"`
	Label label  `jsonapi:"attr,label,omitempty" json:"label"`
}

func (n *NestedModel) GetTranslationKey() (modelName, keyID string) {
	return "nested", n.ID
}

func TestParseTranslateTags_NestedStruct(t *testing.T) {
	model := &NestedModel{
		ID:    "123",
		Title: "Test Title",
		Label: label{
			Title:      "Label Title",
			Background: "red",
		},
	}

	fields, err := parseTranslateTags(model)
	if err != nil {
		t.Fatalf("parseTranslateTags() error = %v", err)
	}

	// Should have 3 fields: ID (primary), Title, and Label.Title
	if len(fields) != 3 {
		t.Errorf("parseTranslateTags() got %d fields, want 3", len(fields))
		for _, f := range fields {
			t.Logf("Field: %s, Column: %s, Primary: %v, Value: %s", f.fieldName, f.columnName, f.isPrimary, f.value)
		}
	}

	// Check that Label.Title field was found
	var labelTitleFound bool
	for _, field := range fields {
		if field.columnName == "label_title" {
			labelTitleFound = true
			if field.fieldName != "Label.Title" {
				t.Errorf("Label title field name = %s, want Label.Title", field.fieldName)
			}
			if field.value != "Label Title" {
				t.Errorf("Label title value = %s, want Label Title", field.value)
			}
		}
	}

	if !labelTitleFound {
		t.Error("Label.Title field not found")
	}

	// Verify Background field (without translate tag) was not included
	for _, field := range fields {
		if field.fieldName == "Label.Background" {
			t.Error("Label.Background should not be included (no translate tag)")
		}
	}
}

func TestParseTranslateTags_EmptyNestedStruct(t *testing.T) {
	// Model with empty nested struct
	model := &NestedModel{
		ID:    "123",
		Title: "Test Title",
		Label: label{}, // Empty label (zero value)
	}

	fields, err := parseTranslateTags(model)
	if err != nil {
		t.Fatalf("parseTranslateTags() error = %v", err)
	}

	// Should only have ID and Title fields, not Label fields
	// because Label is empty (zero value)
	expectedFields := map[string]bool{
		"primary": false, // ID
		"title":   false, // Title
	}

	for _, field := range fields {
		if _, expected := expectedFields[field.columnName]; expected {
			expectedFields[field.columnName] = true
		} else if field.columnName == "label_title" {
			t.Errorf("Label.Title should not be included when Label is empty, but found: %+v", field)
		}
	}

	// Verify expected fields were found
	for col, found := range expectedFields {
		if !found {
			t.Errorf("Expected field with column %s not found", col)
		}
	}
}

func TestSetFieldValue_NestedStruct(t *testing.T) {
	model := &NestedModel{
		ID:    "123",
		Title: "Original Title",
		Label: label{
			Title:      "Original Label",
			Background: "red",
		},
	}

	// Test setting nested field
	err := setFieldValue(model, "Label.Title", "New Label Title")
	if err != nil {
		t.Errorf("setFieldValue() error = %v", err)
	}
	if model.Label.Title != "New Label Title" {
		t.Errorf("Label.Title = %s, want New Label Title", model.Label.Title)
	}

	// Test setting top-level field still works
	err = setFieldValue(model, "Title", "New Title")
	if err != nil {
		t.Errorf("setFieldValue() error = %v", err)
	}
	if model.Title != "New Title" {
		t.Errorf("Title = %s, want New Title", model.Title)
	}

	// Test invalid nested field
	err = setFieldValue(model, "Label.NonExistent", "value")
	if err == nil {
		t.Error("Expected error for non-existent nested field")
	}
}

// PointerNestedModel for testing pointer to nested struct
type PointerNestedModel struct {
	ID    string `translate:"primary"`
	Title string `translate:"title"`
	Media *media `json:"media"`
}

type media struct {
	URL     string `translate:"media_url"`
	Preview string `translate:"media_preview"`
}

func (m *PointerNestedModel) GetTranslationKey() (modelName, keyID string) {
	return "pointer_nested_model", m.ID
}

func TestParseTranslateTags_PointerToStruct(t *testing.T) {
	model := &PointerNestedModel{
		ID:    "123",
		Title: "Test Title",
		Media: &media{
			URL:     "http://example.com/media.jpg",
			Preview: "http://example.com/preview.jpg",
		},
	}

	fields, err := parseTranslateTags(model)
	if err != nil {
		t.Fatalf("parseTranslateTags() error = %v", err)
	}

	// Should have: ID (primary), Title, Media.URL, Media.Preview
	if len(fields) != 4 {
		t.Fatalf("Expected 4 fields, got %d", len(fields))
	}

	// Check nested fields
	foundMediaURL := false
	foundMediaPreview := false
	for _, field := range fields {
		if field.fieldName == "Media.URL" && field.columnName == "media_url" {
			foundMediaURL = true
		}
		if field.fieldName == "Media.Preview" && field.columnName == "media_preview" {
			foundMediaPreview = true
		}
	}

	if !foundMediaURL {
		t.Error("Media.URL field not found")
	}
	if !foundMediaPreview {
		t.Error("Media.Preview field not found")
	}
}

func TestSetFieldValue_PointerToStruct(t *testing.T) {
	model := &PointerNestedModel{
		ID:    "123",
		Title: "Original Title",
		Media: &media{
			URL:     "http://example.com/media.jpg",
			Preview: "http://example.com/preview.jpg",
		},
	}

	// Test setting nested field through pointer
	err := setFieldValue(model, "Media.URL", "http://example.com/new_media.jpg")
	if err != nil {
		t.Errorf("setFieldValue() error = %v", err)
	}
	if model.Media.URL != "http://example.com/new_media.jpg" {
		t.Errorf("Media.URL = %s, want http://example.com/new_media.jpg", model.Media.URL)
	}

	// Test setting another nested field
	err = setFieldValue(model, "Media.Preview", "http://example.com/new_preview.jpg")
	if err != nil {
		t.Errorf("setFieldValue() error = %v", err)
	}
	if model.Media.Preview != "http://example.com/new_preview.jpg" {
		t.Errorf("Media.Preview = %s, want http://example.com/new_preview.jpg", model.Media.Preview)
	}
}
