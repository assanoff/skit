package httpw

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"
)

func TestWrapStatusAndBytes(t *testing.T) {
	is := is.New(t)

	rec := httptest.NewRecorder()
	ww := Wrap(rec)

	is.Equal(ww.Status(), 0) // nothing written yet

	ww.WriteHeader(http.StatusCreated)
	n, err := ww.Write([]byte("hello"))
	is.NoErr(err)
	is.Equal(n, 5)

	is.Equal(ww.Status(), http.StatusCreated)
	is.Equal(ww.BytesWritten(), 5)
	is.Equal(rec.Code, http.StatusCreated) // proxied to the underlying writer
	is.Equal(rec.Body.String(), "hello")
}

func TestWrapImplicitOK(t *testing.T) {
	is := is.New(t)

	rec := httptest.NewRecorder()
	ww := Wrap(rec)
	_, _ = ww.Write([]byte("x"))

	is.Equal(ww.Status(), http.StatusOK) // a Write with no WriteHeader implies 200
}

func TestWrapTee(t *testing.T) {
	is := is.New(t)

	rec := httptest.NewRecorder()
	ww := Wrap(rec)

	var tee bytes.Buffer
	ww.Tee(&tee)
	_, _ = ww.Write([]byte("dup"))

	is.Equal(rec.Body.String(), "dup") // proxied to the original
	is.Equal(tee.String(), "dup")      // and copied to the tee
}

// TestWrapFlushThroughResponseController is the core guarantee of the
// Unwrap-based design: the wrapper does not implement http.Flusher itself, but
// http.ResponseController reaches the underlying Flusher through Unwrap.
func TestWrapFlushThroughResponseController(t *testing.T) {
	is := is.New(t)

	rec := httptest.NewRecorder() // implements http.Flusher
	ww := Wrap(rec)

	_, isFlusher := any(ww).(http.Flusher)
	is.True(!isFlusher) // the wrapper deliberately does NOT re-implement Flusher

	err := http.NewResponseController(ww).Flush()
	is.NoErr(err)        // ...yet Flush still reaches the recorder via Unwrap
	is.True(rec.Flushed) // and actually flushed
}
