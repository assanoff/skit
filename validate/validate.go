package validate

import (
	"errors"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/assanoff/skit/errs"
)

var tags = newValidator()

func newValidator() *validator.Validate {
	v := validator.New()
	v.RegisterTagNameFunc(fieldName)
	return v
}

// Check validates val by its `validate:` struct tags. On failure it returns an
// InvalidArgument *errs.Error with one errs.FieldError per failed constraint;
// the error of a field is the failed tag ("required", "max"). On success it
// returns nil.
func Check(val any) error {
	err := tags.Struct(val)
	if err == nil {
		return nil
	}
	verrs, ok := errors.AsType[validator.ValidationErrors](err)
	if !ok {
		return errs.New(errs.InvalidArgument, err)
	}
	fe := make(errs.FieldErrors, 0, len(verrs))
	for _, e := range verrs {
		fe = append(fe, errs.FieldError{Field: path(e), Error: e.Tag()})
	}
	return fe.Err()
}

// fieldName names a struct field as the client sends it: by its json tag, else
// its query tag, else its Go name in lower case.
func fieldName(f reflect.StructField) string {
	for _, key := range []string{"json", "query"} {
		name, _, _ := strings.Cut(f.Tag.Get(key), ",")
		if name != "" && name != "-" {
			return name
		}
	}
	return strings.ToLower(f.Name)
}

// path is the field's path below the validated struct, such as "name" or
// "items[0].name".
func path(e validator.FieldError) string {
	_, p, ok := strings.Cut(e.Namespace(), ".")
	if !ok {
		return e.Field()
	}
	return p
}
