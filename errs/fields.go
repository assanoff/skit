package errs

// The title and detail of every field error: the fields carry the specifics.
const (
	fieldsTitle  = "data validation error"
	fieldsDetail = "validation failed"
)

// FieldError describes a single invalid input field.
type FieldError struct {
	Field string `json:"field"`
	Error string `json:"error"`
}

// FieldErrors collects the invalid fields of an input, so a client sees them
// all at once.
type FieldErrors []FieldError

// Add records err for field. A nil err records nothing, so the result of a
// parse can be added unchecked.
func (fe *FieldErrors) Add(field string, err error) {
	if err == nil {
		return
	}
	*fe = append(*fe, FieldError{Field: field, Error: err.Error()})
}

// Err returns nil when no field is recorded, else an InvalidArgument *Error
// whose Fields are fe. The result is an error, not an *Error: an empty list
// gives a true nil, never a typed nil that compares non-nil.
func (fe *FieldErrors) Err() error {
	if len(*fe) == 0 {
		return nil
	}
	e := newError(InvalidArgument, nil, fieldsDetail)
	e.Title = fieldsTitle
	e.Fields = *fe
	return e
}

// NewFieldError returns an InvalidArgument *Error for one invalid field. It
// wraps err.
func NewFieldError(field string, err error) *Error {
	e := newError(InvalidArgument, err, fieldsDetail)
	e.Title = fieldsTitle
	e.Fields = FieldErrors{{Field: field, Error: errText(err)}}
	return e
}
