package errs

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPStatusMapping(t *testing.T) {
	cases := map[Code]int{
		OK:               http.StatusOK,
		InvalidArgument:  http.StatusBadRequest,
		NotFound:         http.StatusNotFound,
		AlreadyExists:    http.StatusConflict,
		Unauthenticated:  http.StatusUnauthorized,
		PermissionDenied: http.StatusForbidden,
		Internal:         http.StatusInternalServerError,
	}
	for code, want := range cases {
		if got := code.HTTPStatus(); got != want {
			t.Errorf("%s: got %d want %d", code, got, want)
		}
	}
}

func TestFromAndIs(t *testing.T) {
	base := New(NotFound, errors.New("widget 1 missing"))
	wrapped := fmt.Errorf("service layer: %w", base)

	if !Is(wrapped, NotFound) {
		t.Fatal("Is should see NotFound through wrapping")
	}
	if got := From(wrapped); got.Code != NotFound {
		t.Fatalf("From should extract the *Error, got code %v", got.Code)
	}

	plain := From(errors.New("boom"))
	if plain.Code != Internal {
		t.Fatalf("plain error should map to Internal, got %v", plain.Code)
	}
}

func TestEncodeSanitizes(t *testing.T) {
	e := Newf(Internal, `connect failed token=abcdef123 host=db`)
	data, ct, err := e.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if ct != "application/json" {
		t.Fatalf("content type: %s", ct)
	}
	if strings.Contains(string(data), "abcdef123") {
		t.Fatalf("secret not redacted: %s", data)
	}
	if !strings.Contains(string(data), "token=***") {
		t.Fatalf("expected redaction marker, got: %s", data)
	}
}
