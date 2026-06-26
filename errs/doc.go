// Package errs provides a single, transport-agnostic error type with stable
// codes, an HTTP (and later gRPC) status mapping, request validation helpers,
// and automatic redaction of secrets in user-facing messages.
//
// An *Error carries a machine-readable Code, an optional Title, a human
// Message (the "detail"), and optional Args used for i18n. It implements the
// standard error interface and unwraps to any underlying cause, so it composes
// with errors.Is/errors.As. The JSON shape is small and stable
// ({"code","title","detail","fields"}) so an *Error can be returned directly to
// API clients.
//
// # Codes
//
// Code is a stable classification whose integer values are aligned with
// google.golang.org/grpc/codes, so gRPC mapping is an identity and HTTP mapping
// is a small lookup table. Each code has a snake_case String (e.g.
// "invalid_argument") used as the wire "code" and as the default i18n key, and
// an HTTPStatus. The canonical codes are OK, Canceled, Unknown,
// InvalidArgument, DeadlineExceeded, NotFound, AlreadyExists, PermissionDenied,
// ResourceExhausted, FailedPrecondition, Aborted, OutOfRange, Unimplemented,
// Internal, Unavailable, DataLoss, and Unauthenticated.
//
// # Creating errors
//
// New wraps an existing error, Newf formats a fresh message, and Wrapf wraps a
// cause behind a formatted prefix. The call site (function and file:line) is
// captured for logging. Chain WithTitle, WithMessageID, and WithArgs to enrich
// an error:
//
//	if w == nil {
//	    return errs.Newf(errs.NotFound, "widget %d not found", id).
//	        WithTitle("widget not found").
//	        WithMessageID("widget.not_found").
//	        WithArgs(map[string]any{"id": id})
//	}
//
//	// Wrap a low-level cause behind a stable code:
//	if err := db.QueryRow(...); err != nil {
//	    return errs.Wrapf(errs.Internal, err, "load widget %d", id)
//	}
//
// # Normalizing and inspecting
//
// From normalizes any error into an *Error (an unrecognized error becomes
// Internal), and Is reports whether an error carries a given Code:
//
//	e := errs.From(err)                 // always an *errs.Error (or nil)
//	if errs.Is(err, errs.NotFound) {    // code-aware check
//	    ...
//	}
//
// # Encoding (HTTP)
//
// Encode renders the error as JSON and returns (data, contentType, error),
// satisfying the web encoder contract. It runs the detail through Sanitize, so
// secrets never leak to clients. HTTPStatus gives the response status:
//
//	body, contentType, _ := e.Encode()
//	w.Header().Set("Content-Type", contentType)
//	w.WriteHeader(e.HTTPStatus())
//	w.Write(body)
//
// # Validation
//
// Check validates a struct via github.com/go-playground/validator tags and, on
// failure, returns an InvalidArgument *Error whose Fields list the offending
// inputs. NewFieldErrors builds the same shape from an explicit list:
//
//	type CreateReq struct {
//	    Name  string `validate:"required"`
//	    Email string `validate:"required,email"`
//	}
//	if err := errs.Check(req); err != nil {
//	    return err // 400 with per-field FieldError entries
//	}
//
// # Sanitization
//
// Sanitize redacts common secret patterns (password/token/secret/api_key in
// key=value and JSON forms) and truncates to MaxMessageLen on a UTF-8 boundary.
// Encode applies it automatically; call it directly for any other string bound
// for logs or responses.
//
// # i18n
//
// An *Error is localized by the i18n package using MessageID (or, when unset,
// CodeStr) as the lookup key and Args as template data; see that package's
// TranslateError.
package errs
