package validate_test

import (
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/validate"
)

type item struct {
	Name string `json:"name" validate:"required"`
}

type request struct {
	CreatedAt   string `json:"created_at,omitempty" validate:"required"`
	PageSize    int    `query:"page_size" validate:"min=1"`
	DisplayName string `validate:"required"`
	Secret      string `json:"-" validate:"required"`
	Items       []item `json:"items" validate:"dive"`
}

func TestCheckValid(t *testing.T) {
	is := is.New(t)
	err := validate.Check(request{
		CreatedAt:   "2026-10-20",
		PageSize:    10,
		DisplayName: "x",
		Secret:      "s",
		Items:       []item{{Name: "a"}},
	})
	is.True(err == nil) // a true nil, not a typed nil
}

func TestCheckNamesFields(t *testing.T) {
	is := is.New(t)
	err := validate.Check(request{Items: []item{{Name: "a"}, {}}})

	e := errs.From(err)
	is.Equal(e.Code, errs.InvalidArgument)
	is.Equal(e.Title, "data validation error")
	is.Equal(e.Message, "validation failed")
	is.Equal(e.Fields, errs.FieldErrors{
		{Field: "created_at", Error: "required"},  // json tag
		{Field: "page_size", Error: "min"},        // query tag
		{Field: "displayname", Error: "required"}, // Go name, lower case
		{Field: "secret", Error: "required"},      // json:"-" is not a name
		{Field: "items[1].name", Error: "required"},
	})
}

func TestCheckNotStruct(t *testing.T) {
	is := is.New(t)
	err := validate.Check(42)
	is.True(errs.Is(err, errs.InvalidArgument))
}
