package broker

import "errors"

// permanentError marks an error no retry fixes; see Permanent.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }

func (e permanentError) Unwrap() error { return e.err }

// Permanent marks err as one no retry fixes: the message itself cannot be
// delivered, such as an envelope that does not encode or a message over the
// size limit of the broker. outbox.Handler sends such an event to the dead
// letters of its queue and retries any other error. A misconfiguration (a
// missing exchange or topic, no rights) is not permanent: once it is fixed,
// the message goes out. A Publisher of your own marks its errors the same way.
// Permanent(nil) is nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanent reports whether err, or an error it wraps, was marked by
// Permanent.
func IsPermanent(err error) bool {
	_, ok := errors.AsType[permanentError](err)
	return ok
}
