package errs

import (
	"errors"
	"strings"

	"github.com/go-playground/validator/v10"
)

// FieldError describes a single failed validation constraint on an input field.
type FieldError struct {
	Field string `json:"field"`
	Error string `json:"error"`
}

var validate = validator.New()

// Check validates val using struct tags (github.com/go-playground/validator).
// On failure it returns an *Error with code InvalidArgument whose Fields list
// the offending fields. On success it returns nil.
func Check(val any) error {
	if err := validate.Struct(val); err != nil {
		var verrs validator.ValidationErrors
		if !errors.As(err, &verrs) {
			return New(InvalidArgument, err)
		}

		fields := make([]FieldError, 0, len(verrs))
		for _, fe := range verrs {
			fields = append(fields, FieldError{
				Field: fieldName(fe),
				Error: fe.Tag(),
			})
		}

		e := Newf(InvalidArgument, "validation failed")
		e.Title = "data validation error"
		e.Fields = fields
		return e
	}
	return nil
}

// NewFieldErrors builds an InvalidArgument error from an explicit field list.
func NewFieldErrors(msg string, fields ...FieldError) *Error {
	e := Newf(InvalidArgument, "%s", msg)
	e.Fields = fields
	return e
}

func fieldName(fe validator.FieldError) string {
	// Prefer the json-ish lowercased namespace minus the struct root.
	ns := fe.Namespace()
	if i := strings.IndexByte(ns, '.'); i >= 0 {
		ns = ns[i+1:]
	}
	if ns == "" {
		return strings.ToLower(fe.Field())
	}
	return strings.ToLower(ns)
}
