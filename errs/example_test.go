package errs_test

import (
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/assanoff/skit/errs"
)

// A handler returns an *Error with a stable code; the transport answers with
// the status of the code and the JSON of the error.
func Example() {
	err := errs.Newf(errs.NotFound, "widget %d not found", 42).
		WithTitle("widget not found")

	body, contentType, _ := err.Encode()
	fmt.Println(err.HTTPStatus(), contentType)
	fmt.Println(string(body))
	// Output:
	// 404 application/json
	// {"code":"not_found","title":"widget not found","detail":"widget 42 not found"}
}

// Wrapf puts a low-level cause behind a stable code: errs.Is checks the code,
// errors.Is still finds the cause.
func ExampleWrapf() {
	err := errs.Wrapf(errs.Unavailable, io.ErrUnexpectedEOF, "load widget %d", 42)

	fmt.Println(err)
	fmt.Println(errs.Is(err, errs.Unavailable), errors.Is(err, io.ErrUnexpectedEOF))
	// Output:
	// github.com/assanoff/skit/errs_test.ExampleWrapf: unavailable: load widget 42: unexpected EOF
	// true true
}

// From turns any error into an *Error: one it wraps is returned as is, any
// other becomes Internal.
func ExampleFrom() {
	known := fmt.Errorf("query: %w", errs.Newf(errs.AlreadyExists, "name taken"))
	unknown := errors.New("connection reset")

	fmt.Println(errs.From(known).Code, errs.From(known).HTTPStatus())
	fmt.Println(errs.From(unknown).Code, errs.From(unknown).HTTPStatus())
	// Output:
	// already_exists 409
	// internal 500
}

// FieldErrors collects every invalid field of an input, so the client sees
// them all at once; Add skips a nil error.
func ExampleFieldErrors() {
	var fe errs.FieldErrors
	_, err := strconv.Atoi("ten")
	fe.Add("limit", err)
	_, err = strconv.Atoi("0")
	fe.Add("offset", err)
	fe.Add("sort", errors.New("unknown field \"color\""))

	body, _, _ := errs.From(fe.Err()).Encode()
	fmt.Println(string(body))
	// Output:
	// {"code":"invalid_argument","title":"data validation error","detail":"validation failed","fields":[{"field":"limit","error":"strconv.Atoi: parsing \"ten\": invalid syntax"},{"field":"sort","error":"unknown field \"color\""}]}
}

// Sanitize redacts secrets in a message bound for a client or a log.
func ExampleSanitize() {
	fmt.Println(errs.Sanitize(`login failed: password=hunter2 {"token":"abc"}`))
	// Output:
	// login failed: password=*** {"token":"***"}
}
