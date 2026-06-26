package errs

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"runtime"
)

// Error is the canonical application error.
//
// The JSON shape is intentionally small and stable so it can be returned
// directly to API clients:
//
//	{"code":"not_found","title":"...","detail":"widget 42 not found","fields":[...]}
type Error struct {
	Code    Code           `json:"-"`
	CodeStr string         `json:"code"`
	Title   string         `json:"title,omitempty"`
	Message string         `json:"detail"`
	Fields  []FieldError   `json:"fields,omitempty"`
	Args    map[string]any `json:"-"`

	// MessageID is an optional i18n message key. When set it overrides CodeStr as
	// the lookup key the i18n package uses to localize Message; Args supply the
	// template data. Left empty, translation falls back to CodeStr.
	MessageID string `json:"-"`

	funcName string
	fileName string
	wrapped  error
}

// New creates an *Error with the given code, wrapping err. The message is taken
// from err. The call site (function and file:line) is captured for logging.
func New(code Code, err error) *Error {
	return newError(code, err, errText(err))
}

// Newf creates an *Error with a formatted message and no wrapped cause.
func Newf(code Code, format string, args ...any) *Error {
	return newError(code, nil, fmt.Sprintf(format, args...))
}

// Wrapf wraps err with a code and a formatted message that prefixes the cause.
func Wrapf(code Code, err error, format string, args ...any) *Error {
	msg := fmt.Sprintf(format, args...)
	if err != nil {
		msg = msg + ": " + errText(err)
	}
	return newError(code, err, msg)
}

func newError(code Code, wrapped error, msg string) *Error {
	e := &Error{
		Code:    code,
		CodeStr: code.String(),
		Message: msg,
		wrapped: wrapped,
	}
	if pc, file, line, ok := runtime.Caller(2); ok {
		e.fileName = fmt.Sprintf("%s:%d", file, line)
		if fn := runtime.FuncForPC(pc); fn != nil {
			e.funcName = fn.Name()
		}
	}
	return e
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.funcName != "" {
		return fmt.Sprintf("%s: %s: %s", e.funcName, e.CodeStr, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.CodeStr, e.Message)
}

// Unwrap exposes the wrapped cause for errors.Is/errors.As.
func (e *Error) Unwrap() error { return e.wrapped }

// HTTPStatus returns the HTTP status code for this error.
func (e *Error) HTTPStatus() int { return e.Code.HTTPStatus() }

// WithTitle sets a human-friendly title and returns the error for chaining.
func (e *Error) WithTitle(title string) *Error {
	e.Title = title
	return e
}

// WithMessageID sets the i18n lookup key and returns the error for chaining.
func (e *Error) WithMessageID(id string) *Error {
	e.MessageID = id
	return e
}

// WithArgs attaches i18n arguments and returns the error for chaining.
func (e *Error) WithArgs(args map[string]any) *Error {
	if e.Args == nil {
		e.Args = map[string]any{}
	}
	maps.Copy(e.Args, args)
	return e
}

// Encode renders the error as JSON, redacting any secrets in the detail.
// It satisfies the web encoder contract: (data, contentType, error).
func (e *Error) Encode() ([]byte, string, error) {
	out := *e
	out.Message = Sanitize(out.Message)
	data, err := json.Marshal(out)
	return data, "application/json", err
}

// From normalizes any error into an *Error. If err already is (or wraps) an
// *Error it is returned as-is; otherwise it becomes an Internal error.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return New(Internal, err)
}

// Is reports whether err is (or wraps) an *Error with the given code.
func Is(err error, code Code) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == code
	}
	return false
}
