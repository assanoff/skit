package httpclient

import (
	"net/url"
	"testing"

	"github.com/matryer/is"
)

func TestRedactURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string // "" means url.full stays as otelhttp set it
	}{
		{name: "no signature", in: "https://u:p@api/x?a=1"},
		{
			name: "signature keys",
			in:   "https://api/x?Signature=s&a=1&AWSAccessKeyId=k",
			want: "https://api/x?Signature=REDACTED&a=1&AWSAccessKeyId=REDACTED",
		},
		{
			name: "user info dropped",
			in:   "https://u:p@api/x?sig=s",
			want: "https://api/x?sig=REDACTED",
		},
		{
			name: "escaped key",
			in:   "https://api/x?X%2DGoog-Signature=s",
			want: "https://api/x?X%2DGoog-Signature=REDACTED",
		},
		{name: "key without value", in: "https://api/x?sig"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			is := is.New(t)
			u, err := url.Parse(tc.in)
			is.NoErr(err)

			got, ok := redactURL(u)

			is.Equal(ok, tc.want != "")
			is.Equal(got, tc.want)
			is.Equal(u.String(), tc.in) // the request URL is untouched
		})
	}
}
