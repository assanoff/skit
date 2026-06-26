package apitest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Server wraps an httptest.Server with JSON request and assertion helpers.
type Server struct {
	*httptest.Server
	t *testing.T
}

// New starts an httptest.Server for handler and registers its shutdown with
// t.Cleanup.
func New(t *testing.T, handler http.Handler) *Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Server{Server: srv, t: t}
}

// Option customizes an outbound request, e.g. to set headers.
type Option func(*http.Request)

// WithHeader sets a request header.
func WithHeader(key, value string) Option {
	return func(r *http.Request) { r.Header.Set(key, value) }
}

// WithBearer sets an Authorization: Bearer <token> header.
func WithBearer(token string) Option {
	return WithHeader("Authorization", "Bearer "+token)
}

// Response holds a completed HTTP response with its body buffered for repeated
// assertions and decoding.
type Response struct {
	t          *testing.T
	StatusCode int
	Header     http.Header
	Body       []byte
}

// Do sends method to path with an optional raw string body (JSON when
// non-empty) and returns the buffered response. It fails the test on transport
// errors.
func (s *Server) Do(method, path, body string, opts ...Option) *Response {
	s.t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, s.URL+path, rdr)
	if err != nil {
		s.t.Fatalf("apitest: new request %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(req)
	}

	resp, err := s.Client().Do(req)
	if err != nil {
		s.t.Fatalf("apitest: %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		s.t.Fatalf("apitest: read body %s %s: %v", method, path, err)
	}
	return &Response{t: s.t, StatusCode: resp.StatusCode, Header: resp.Header, Body: buf}
}

// Get issues a GET request.
func (s *Server) Get(path string, opts ...Option) *Response {
	return s.Do(http.MethodGet, path, "", opts...)
}

// Delete issues a DELETE request.
func (s *Server) Delete(path string, opts ...Option) *Response {
	return s.Do(http.MethodDelete, path, "", opts...)
}

// PostJSON issues a POST with a JSON body.
func (s *Server) PostJSON(path, body string, opts ...Option) *Response {
	return s.Do(http.MethodPost, path, body, opts...)
}

// PutJSON issues a PUT with a JSON body.
func (s *Server) PutJSON(path, body string, opts ...Option) *Response {
	return s.Do(http.MethodPut, path, body, opts...)
}

// ExpectStatus fails the test unless the response status equals want. It
// returns the receiver for chaining.
func (r *Response) ExpectStatus(want int) *Response {
	r.t.Helper()
	if r.StatusCode != want {
		r.t.Fatalf("apitest: status = %d, want %d (body: %s)", r.StatusCode, want, r.Body)
	}
	return r
}

// Decode unmarshals the JSON body into v, failing the test on error.
func (r *Response) Decode(v any) {
	r.t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		r.t.Fatalf("apitest: decode body: %v (body: %s)", err, r.Body)
	}
}

// JSON decodes the body into a generic object map and returns it.
func (r *Response) JSON() map[string]any {
	r.t.Helper()
	var out map[string]any
	r.Decode(&out)
	return out
}

// JSONArray decodes the body into a slice of generic object maps.
func (r *Response) JSONArray() []map[string]any {
	r.t.Helper()
	var out []map[string]any
	r.Decode(&out)
	return out
}
