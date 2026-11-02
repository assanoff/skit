package queue

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/matryer/is"
)

type (
	welcome struct {
		UserID int `json:"user_id"`
	}
	reminder struct{}
	receipt  struct{}
)

// fixed is an Encoder that writes payload for every value.
func fixed(payload string) Encoder {
	return func(context.Context, any) ([]byte, error) {
		return []byte(payload), nil
	}
}

// failing is an Encoder that fails every value.
func failing(context.Context, any) ([]byte, error) {
	return nil, errors.New("encode failed")
}

func TestRegister(t *testing.T) {
	is := is.New(t)
	reg := NewRegistry()
	is.NoErr(reg.Register[welcome]("email", "email.welcome"))
	is.NoErr(reg.Register[reminder]("email", "email.reminder", WithEncoder(fixed("hi"))))
	is.True(reg.Register[*welcome]("email", "email.other") != nil) // *T is T: a duplicate
	is.True(reg.Register[receipt]("", "email.receipt") != nil)     // a queue is required
	is.True(reg.Register[receipt]("email", "") != nil)             // and so is a kind

	rt, err := reg.lookup(&welcome{})
	is.NoErr(err)
	is.Equal(rt.queue, "email") // *T resolves the route of T
	is.Equal(rt.kind, "email.welcome")
	b, err := rt.enc(t.Context(), welcome{UserID: 7})
	is.NoErr(err)
	is.Equal(string(b), `{"user_id":7}`) // JSON by default
	rt, err = reg.lookup(reminder{})
	is.NoErr(err)
	b, err = rt.enc(t.Context(), reminder{})
	is.NoErr(err)
	is.Equal(string(b), "hi") // or the Encoder of the route

	_, err = reg.lookup(receipt{})
	is.True(err != nil) // an unregistered type
	_, err = reg.lookup(nil)
	is.True(err != nil) // and a nil value fail
}

func TestRegistryCheck(t *testing.T) {
	is := is.New(t)
	noop := func(context.Context, Task) error { return nil }
	reg := NewRegistry()
	is.NoErr(reg.Register[welcome]("email", "email.welcome"))
	is.NoErr(reg.Register[reminder]("email", "email.reminder"))
	is.NoErr(reg.Register[receipt]("email", "email.receipt", Drop()))
	is.NoErr(reg.Register[order]("orders", "order.created"))
	mux := NewMux(WithFallback(noop))
	is.NoErr(mux.Register("email.welcome", noop))

	err := reg.Check("email", mux)
	is.True(errors.Is(err, ErrUnknownKind))                  // the fallback does not count
	is.True(strings.Contains(err.Error(), "email.reminder")) // for a kind with no handler
	is.True(!strings.Contains(err.Error(), "email.receipt")) // a dropped route sends nothing

	is.NoErr(mux.Register("email.reminder", noop))
	is.NoErr(reg.Check("email", mux))       // the kinds of another queue aside
	is.True(reg.Check("email", nil) != nil) // a nil mux is an error
}
