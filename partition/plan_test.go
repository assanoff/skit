package partition

import (
	"strings"
	"testing"
	"time"

	"github.com/matryer/is"
)

// newKeeper builds a Keeper over no database: plan and parse need none.
func newKeeper(t *testing.T, cfg Config) *Keeper {
	t.Helper()
	k, err := New(nil, nil, cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return k
}

// existing parses the names of the partitions each table has, as Maintain
// reads them from the catalog.
func existing(k *Keeper, names map[string][]string) []part {
	var parts []part
	for _, table := range k.cfg.Tables {
		for _, name := range names[table] {
			if p, ok := k.parse(table, name); ok {
				parts = append(parts, p)
			}
		}
	}
	return parts
}

// partNames returns the names of the partitions.
func partNames(parts []part) []string {
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		names = append(names, p.name)
	}
	return names
}

func TestPlanCreatesCurrentAndAhead(t *testing.T) {
	is := is.New(t)
	k := newKeeper(t, Config{Tables: []string{"messages", "deliveries"}, Period: Month})
	now := time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)

	create, drop := k.plan(now, existing(k, map[string][]string{
		"messages": {"messages_2026_12"},
	}))

	is.Equal(partNames(create), []string{
		"deliveries_2026_12",
		"messages_2027_01",
		"deliveries_2027_01",
	}) // the current month and the next, only the missing ones, messages first
	is.Equal(len(drop), 0) // no retention, nothing is dropped
}

func TestPlanDropsTheReferencingTableFirst(t *testing.T) {
	is := is.New(t)
	k := newKeeper(t, Config{
		Tables:    []string{"messages", "deliveries"},
		Period:    Month,
		Retention: 90 * 24 * time.Hour,
	})
	now := time.Date(2027, 2, 1, 3, 30, 0, 0, time.UTC)
	have := map[string][]string{
		"messages":   {"messages_2026_10", "messages_2026_11"},
		"deliveries": {"deliveries_2026_10", "deliveries_2026_11"},
	}

	_, drop := k.plan(now, existing(k, have))

	// 90 days back is 3 November: October has ended before it, November not.
	is.Equal(partNames(drop), []string{"deliveries_2026_10", "messages_2026_10"})
}

// A row 89 days old stays with the retention of 90 days, whether now is the
// start, the middle or the end of the month; at the start of May 89 days back
// is the first of February.
func TestPlanKeepsRowsYoungerThanRetention(t *testing.T) {
	for _, now := range []time.Time{
		time.Date(2027, 5, 1, 3, 30, 0, 0, time.UTC),
		time.Date(2027, 5, 15, 3, 30, 0, 0, time.UTC),
		time.Date(2027, 5, 31, 23, 59, 0, 0, time.UTC),
	} {
		t.Run(now.Format(time.DateOnly), func(t *testing.T) {
			is := is.New(t)
			k := newKeeper(t, Config{
				Tables:    []string{"messages"},
				Period:    Month,
				Retention: 90 * 24 * time.Hour,
			})
			row := now.Add(-89 * 24 * time.Hour)
			holder := k.part("messages", k.cfg.Period.start(row))

			_, drop := k.plan(now, []part{holder})

			is.Equal(len(drop), 0) // the partition of the row stays
		})
	}
}

func TestPlanDays(t *testing.T) {
	is := is.New(t)
	k := newKeeper(t, Config{
		Tables:    []string{"audit.log"},
		Period:    Day,
		Ahead:     2,
		Retention: 48 * time.Hour,
	})
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	have := map[string][]string{
		"audit.log": {"log_2026_11_02", "log_2026_11_03", "log_2026_11_05"},
	}

	create, drop := k.plan(now, existing(k, have))

	is.Equal(partNames(create), []string{"audit.log_2026_11_06", "audit.log_2026_11_07"})
	// 48 hours back is 3 November 10:00: the 2nd has ended, the 3rd not yet.
	is.Equal(partNames(drop), []string{"audit.log_2026_11_02"})
}

func TestParseSkipsOtherNames(t *testing.T) {
	is := is.New(t)
	k := newKeeper(t, Config{Tables: []string{"messages"}, Period: Month})

	for _, name := range []string{
		"messages_default",
		"messages_2026_11_05", // a day, not a month
		"messages_2026_13",
		"messages2026_11",
		"deliveries_2026_11",
	} {
		_, ok := k.parse("messages", name)
		is.True(!ok) // not a partition of the month
	}
	p, ok := k.parse("messages", "messages_2026_11")
	is.True(ok)
	is.Equal(p.from, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	is.Equal(p.to, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
}

func TestNewChecksConfig(t *testing.T) {
	long := strings.Repeat("a", 53) // 53 + len("_2026_11_05") = 64 bytes
	for name, cfg := range map[string]Config{
		"no period":          {Tables: []string{"messages"}},
		"unknown period":     {Tables: []string{"messages"}, Period: 3},
		"negative ahead":     {Tables: []string{"messages"}, Period: Month, Ahead: -1},
		"negative retention": {Tables: []string{"messages"}, Period: Month, Retention: -time.Hour},
		"no tables":          {Period: Month},
		"uppercase":          {Tables: []string{"Messages"}, Period: Month},
		"quoted":             {Tables: []string{`"messages"`}, Period: Month},
		"injection":          {Tables: []string{"messages; DROP TABLE x"}, Period: Month},
		"two schemas":        {Tables: []string{"a.b.messages"}, Period: Month},
		"listed twice":       {Tables: []string{"messages", "messages"}, Period: Month},
		"long name":          {Tables: []string{long}, Period: Day},
	} {
		t.Run(name, func(t *testing.T) {
			is := is.New(t)
			_, err := New(nil, nil, cfg)
			is.True(err != nil) // the config is rejected
		})
	}
}

func TestNewDefaultsAhead(t *testing.T) {
	is := is.New(t)
	k := newKeeper(t, Config{Tables: []string{"public.messages"}, Period: Month})
	is.Equal(k.cfg.Ahead, 1) // the next period by default
}
