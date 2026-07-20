package dbx

import "testing"

// TestQueryStringPlaceholderPrefixOverlap verifies that a placeholder that is a
// prefix of a longer one (:id vs :id_owner) does not corrupt the longer one in
// the rendered debug SQL.
func TestQueryStringPlaceholderPrefixOverlap(t *testing.T) {
	type args struct {
		ID      int `db:"id"`
		IDOwner int `db:"id_owner"`
	}

	got := queryString("SELECT * FROM t WHERE id = :id AND id_owner = :id_owner", args{ID: 42, IDOwner: 7})
	want := "SELECT * FROM t WHERE id = 42 AND id_owner = 7"
	if got != want {
		t.Fatalf("queryString =\n  %q\nwant\n  %q", got, want)
	}
}

// TestQueryStringRedactsSensitive verifies secret-looking columns are redacted
// even when a shorter placeholder shares their prefix.
func TestQueryStringRedactsSensitive(t *testing.T) {
	type args struct {
		Token string `db:"token"`
	}

	got := queryString("SELECT * FROM t WHERE token = :token", args{Token: "s3cr3t"})
	want := "SELECT * FROM t WHERE token = '***'"
	if got != want {
		t.Fatalf("queryString =\n  %q\nwant\n  %q", got, want)
	}
}
