package dedup

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/matryer/is"
)

func TestNewMemory(t *testing.T) {
	is := is.New(t)
	k, err := New("mem://", nil)
	is.NoErr(err)
	_, ok := k.st.(*memory)
	is.True(ok)         // mem:// is the memory store
	is.NoErr(k.Close()) // with nothing to close
}

func TestNewRedis(t *testing.T) {
	is := is.New(t)
	mr := miniredis.RunT(t)
	k, err := New("redis://"+mr.Addr()+"/0?max_retries=-1", nil, WithTTL(time.Hour))
	is.NoErr(err)
	is.Equal(k.ttl, time.Hour) // the options apply

	calls := 0
	_, err = k.Do(t.Context(), "k", counter(&calls, 1))
	is.NoErr(err)
	is.True(mr.Exists(redisPrefix + "k")) // the record is in Redis

	is.NoErr(k.Close())
	_, err = k.Do(t.Context(), "k2", counter(&calls, 1))
	is.True(errors.Is(err, ErrUnavailable)) // Close closed the client of New
}

func TestNewRejects(t *testing.T) {
	for name, uri := range map[string]string{
		"empty":      "",
		"no scheme":  "localhost:6379",
		"unknown":    "memcached://localhost:11211",
		"mem host":   "mem://lru",
		"redis db":   "redis://localhost:6379/x",
		"redis opt":  "redis://localhost:6379/0?bogus=1",
		"bad url":    "redis://user:secret@[::1",
		"bad scheme": "redis:/localhost",
	} {
		t.Run(name, func(t *testing.T) {
			is := is.New(t)
			_, err := New(uri, nil)
			is.True(err != nil)
			is.True(!strings.Contains(err.Error(), "secret")) // the password stays out
		})
	}
}
