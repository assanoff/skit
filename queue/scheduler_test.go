package queue

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbx"
)

// A task scheduled through Bind lives and dies with its transaction; a
// dropped type writes nothing, and a value that does not route leaves the
// transaction usable.
func TestScheduler(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	log := slog.New(slog.DiscardHandler)
	q := newPG(t)
	reg := NewRegistry()
	is.NoErr(reg.Register[welcome]("email", "email.welcome"))
	is.NoErr(reg.Register[reminder]("email", "email.reminder", WithEncoder(fixed("hi"))))
	is.NoErr(reg.Register[receipt]("email", "email.receipt", Drop()))
	is.NoErr(reg.Register[order]("orders", "order.created", WithEncoder(failing)))

	boom := errors.New("boom")
	err := dbx.WithinTran(ctx, log, q.db, func(tx *dbx.Tx) error {
		is.NoErr(Bind(tx, q, reg).Schedule(ctx, welcome{UserID: 1}))
		return boom
	})
	is.True(errors.Is(err, boom))
	err = dbx.WithinTran(ctx, log, q.db, func(tx *dbx.Tx) error {
		s := Bind(tx, q, reg)
		is.True(s.Schedule(ctx, order{}) != nil)    // the encoder fails
		is.True(s.Schedule(ctx, struct{}{}) != nil) // an unregistered type
		is.NoErr(s.Schedule(ctx, &welcome{UserID: 2}))
		is.NoErr(s.Schedule(ctx, reminder{}))
		return s.Schedule(ctx, receipt{})
	})
	is.NoErr(err)

	claimed, err := q.claim(ctx, "email", later(), time.Minute, 10)
	is.NoErr(err)
	// The rolled-back task is gone, and the dropped one was never written.
	is.Equal(kinds(claimed), []string{"email.welcome", "email.reminder"})
	is.Equal(string(claimed[0].Payload), `{"user_id":2}`)
	is.Equal(string(claimed[1].Payload), "hi")
	orders, err := q.claim(ctx, "orders", later(), time.Minute, 10)
	is.NoErr(err)
	is.Equal(len(orders), 0)
}
