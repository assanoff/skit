// Package httpclient builds configured *http.Client values for calling upstream
// services, and helps consume their JSON responses. It is the transport half of
// a typed service client (see `skit add http-client`): the generated client
// package builds one of these in its constructor, then its methods assemble
// requests and decode responses through DoJSON.
//
// The transport is composed from httpmw middlewares — retry (5xx/429 +
// Retry-After), a User-Agent, an idempotency key, arbitrary headers — and, when
// OAuth2 is configured, an OAuth2 client_credentials bearer. Composition order
// (httpmw.Chain) puts retry outermost and auth innermost, so a retried attempt
// re-attaches the current token.
package httpclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/assanoff/skit/httpmw"
)

// DefaultTimeout is the per-request client timeout when Config.Timeout is unset.
const DefaultTimeout = 30 * time.Second

// maxErrorBody caps how much of a non-2xx response body is read into a
// StatusError, so a large/hostile error page cannot balloon memory.
const maxErrorBody = 8 << 10

// Config assembles an upstream HTTP client. All fields are optional except that
// OAuth2, when set, requires its own required fields. The zero Config yields a
// plain client with DefaultTimeout and no retry/auth.
type Config struct {
	// Timeout is the per-request client timeout (default DefaultTimeout). It
	// bounds the whole call including retries.
	Timeout time.Duration
	// Retry, when non-nil, wraps the transport in retry (5xx/429 + Retry-After).
	Retry *httpmw.RetryConfig
	// OAuth2, when non-nil, adds a client_credentials bearer token.
	OAuth2 *OAuth2Config
	// UserAgent, when non-empty, sets the User-Agent header (product name).
	UserAgent string
	// Version, Environment refine the User-Agent to "product/version (env)".
	Version     string
	Environment string
	// IdempotencyHeader, when non-empty, injects a per-request idempotency key
	// under this header (retry-safe: kept stable across retries).
	IdempotencyHeader string
	// Headers are static headers set on every request.
	Headers map[string]string
	// Base is the innermost (wire) transport (default http.DefaultTransport).
	Base http.RoundTripper
}

// New builds an *http.Client with the composed transport. It returns an error
// only when OAuth2 is set but invalid.
func New(cfg Config) (*http.Client, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	// Innermost: OAuth2 bearer over the wire transport, or the wire transport
	// itself. Retry/headers are layered above this so they re-run per attempt.
	base := cfg.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if cfg.OAuth2 != nil {
		oauthRT, err := OAuth2Transport(*cfg.OAuth2, base)
		if err != nil {
			return nil, err
		}
		base = oauthRT
	}

	// Outermost first: retry wraps everything; then per-request decorations.
	var mws []httpmw.Middleware
	if cfg.Retry != nil {
		mws = append(mws, httpmw.Retry(*cfg.Retry))
	}
	if cfg.UserAgent != "" {
		mws = append(mws, httpmw.UserAgent(cfg.UserAgent, cfg.Version, cfg.Environment))
	}
	if cfg.IdempotencyHeader != "" {
		mws = append(mws, httpmw.IdempotencyKey(cfg.IdempotencyHeader))
	}
	for k, v := range cfg.Headers {
		mws = append(mws, httpmw.SetHeader(k, v))
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: httpmw.Chain(base, mws...),
	}, nil
}

// StatusError is returned by DoJSON when the upstream responds with a non-2xx
// status. It carries the status and a (capped) snapshot of the body so callers
// can branch on StatusCode with errors.As and surface the upstream message.
type StatusError struct {
	StatusCode int
	Status     string
	Body       []byte
}

func (e *StatusError) Error() string {
	if len(e.Body) > 0 {
		return fmt.Sprintf("http %s: %s", e.Status, e.Body)
	}
	return fmt.Sprintf("http %s", e.Status)
}

// DoJSON sends req through c, and on a 2xx response decodes the JSON body into
// out (skip decoding by passing a nil out). A non-2xx response yields a
// *StatusError; a transport error is returned as-is. The response body is always
// closed. An empty 2xx body is not an error (out is left unchanged).
func DoJSON(c *http.Client, req *http.Request, out any) error {
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return &StatusError{StatusCode: resp.StatusCode, Status: resp.Status, Body: body}
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("httpclient: decode response: %w", err)
	}
	return nil
}
