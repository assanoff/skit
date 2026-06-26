package mid

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/assanoff/skit/rest"
)

// ETag returns application middleware that adds an entity-tag to successful
// responses and serves a 304 Not Modified when the client's If-None-Match
// matches. It is an app-developer choice attached per handler or group —
// handle("GET /x", h.get, mid.ETag()) — typically on cacheable reads.
//
// The tag is a strong validator computed from the encoded body, so it needs the
// body: ETag encodes the response once and forwards the bytes (no re-encode by
// Respond). It sets the ETag header via rest.GetWriter but never calls
// WriteHeader. Error, nil (204), and writer-less (direct-call) responses pass
// through untouched.
func ETag() rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			resp := next(ctx, r)
			if resp == nil || isError(resp) {
				return resp
			}
			w := rest.GetWriter(ctx)
			if w == nil {
				return resp
			}

			body, contentType, err := resp.Encode()
			if err != nil {
				return resp // let Respond surface the encode error uniformly
			}

			sum := sha256.Sum256(body)
			etag := `"` + hex.EncodeToString(sum[:16]) + `"`
			w.Header().Set("ETag", etag)

			if matchesETag(r.Header.Get("If-None-Match"), etag) {
				return rawEncoder{status: http.StatusNotModified}
			}
			return rawEncoder{data: body, contentType: contentType, status: statusOf(resp)}
		}
	}
}

// rawEncoder carries an already-encoded body, content type, and status, so a
// middleware that has read the body (ETag) can forward it without Respond
// re-encoding the original ResponseEncoder.
type rawEncoder struct {
	data        []byte
	contentType string
	status      int
}

func (e rawEncoder) Encode() ([]byte, string, error) {
	return e.data, e.contentType, nil
}

func (e rawEncoder) HTTPStatus() int {
	return e.status
}

// statusOf reports the HTTP status a ResponseEncoder would produce (200 unless it
// advertises one), so ETag can preserve it when forwarding the body.
func statusOf(resp rest.ResponseEncoder) int {
	if hs, ok := resp.(interface{ HTTPStatus() int }); ok {
		return hs.HTTPStatus()
	}
	return http.StatusOK
}

// matchesETag reports whether an If-None-Match header value matches etag. It
// honors the "*" wildcard and a comma-separated list, which covers the common
// conditional-GET cases.
func matchesETag(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	if strings.TrimSpace(ifNoneMatch) == "*" {
		return true
	}
	for candidate := range strings.SplitSeq(ifNoneMatch, ",") {
		if strings.TrimSpace(candidate) == etag {
			return true
		}
	}
	return false
}
