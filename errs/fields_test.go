package errs

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/matryer/is"
)

func TestFieldErrorsEmptyIsNil(t *testing.T) {
	is := is.New(t)
	var fe FieldErrors
	fe.Add("id", nil)
	err := fe.Err()
	is.True(err == nil) // no field recorded: a true nil, not a typed nil
}

func TestFieldErrorsErr(t *testing.T) {
	is := is.New(t)
	var fe FieldErrors
	fe.Add("id", errors.New("invalid UUID length: 3"))
	fe.Add("name", nil)
	fe.Add("from", errors.New("after to"))

	e := From(fe.Err())
	is.Equal(e.Code, InvalidArgument)
	is.Equal(e.Title, "data validation error")
	is.Equal(e.Message, "validation failed")
	is.Equal(e.Fields, FieldErrors{
		{Field: "id", Error: "invalid UUID length: 3"},
		{Field: "from", Error: "after to"},
	})
}

func TestNewFieldError(t *testing.T) {
	is := is.New(t)
	cause := errors.New("page must be positive")
	e := NewFieldError("page", cause)
	is.Equal(e.Code, InvalidArgument)
	is.Equal(e.Fields, FieldErrors{{Field: "page", Error: "page must be positive"}})
	is.True(errors.Is(e, cause)) // the cause stays in the chain

	data, _, err := e.Encode()
	is.NoErr(err)
	var body map[string]any
	is.NoErr(json.Unmarshal(data, &body))
	is.Equal(body, map[string]any{
		"code":   "invalid_argument",
		"title":  "data validation error",
		"detail": "validation failed",
		"fields": []any{map[string]any{"field": "page", "error": "page must be positive"}},
	})
}
