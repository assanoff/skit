package broker_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/broker"
)

func TestPermanent(t *testing.T) {
	is := is.New(t)
	boom := errors.New("boom")

	err := fmt.Errorf("publish: %w", broker.Permanent(boom))
	is.True(broker.IsPermanent(err)) // found through a wrap
	is.True(errors.Is(err, boom))    // the cause stays reachable
	is.Equal(err.Error(), "publish: boom")

	is.True(!broker.IsPermanent(boom))
	is.True(!broker.IsPermanent(nil))
	is.NoErr(broker.Permanent(nil))
}
