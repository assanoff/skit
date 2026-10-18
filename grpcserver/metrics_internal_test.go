package grpcserver

import (
	"testing"

	"github.com/matryer/is"
	"google.golang.org/grpc/codes"
)

func TestCodeName(t *testing.T) {
	tests := []struct {
		code codes.Code
		want string
	}{
		{code: codes.OK, want: "OK"},
		{code: codes.Canceled, want: "CANCELLED"}, //nolint:misspell // the canonical gRPC name
		{code: codes.Unauthenticated, want: "UNAUTHENTICATED"},
		{code: codes.Code(99), want: "99"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			is.New(t).Equal(codeName(tt.code), tt.want)
		})
	}
}
