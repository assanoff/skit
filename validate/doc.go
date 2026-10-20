// Package validate checks a struct by its `validate:` tags
// (github.com/go-playground/validator) and reports the failures as an
// errs.FieldErrors error.
//
// It is a package of its own so that errs stays on the standard library: only
// a package that validates by tags builds the validator. A service that checks
// its input by hand collects the failures in errs.FieldErrors directly, and its
// client gets the same response.
//
// # Usage
//
//	type CreateReq struct {
//	    Name      string `json:"name" validate:"required,max=100"`
//	    CreatedAt string `json:"created_at" validate:"required"`
//	}
//
//	if err := validate.Check(req); err != nil {
//	    return err // 400: one entry per failed constraint in "fields"
//	}
//
// # Field names
//
// A field is named as the client sends it: by its json tag, else its query
// tag, else its Go name in lower case. A nested field carries its path:
// "items[0].name". The error of a field is the failed tag: "required", "max".
package validate
