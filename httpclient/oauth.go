package httpclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// defaultTokenTimeout bounds the token-endpoint fetch when OAuth2Config leaves
// TokenTimeout unset.
const defaultTokenTimeout = 10 * time.Second

// OAuth2Config configures an OAuth2 client_credentials token source. TokenURL,
// ClientID and ClientSecret are required. Scopes are baked into the token source
// (a client needs one instance per scope set — the token cache is per source).
type OAuth2Config struct {
	// TokenURL is the full OAuth2 token endpoint.
	TokenURL string
	// ClientID, ClientSecret are the client_credentials grant.
	ClientID     string
	ClientSecret string
	// Scopes are requested with the token; may be empty.
	Scopes []string
	// TokenTimeout bounds each token fetch (default 10s). The token fetch uses
	// its own client, separate from the business request transport.
	TokenTimeout time.Duration
}

func (c OAuth2Config) validate() error {
	switch {
	case strings.TrimSpace(c.TokenURL) == "":
		return errors.New("httpclient: OAuth2Config.TokenURL is required")
	case strings.TrimSpace(c.ClientID) == "":
		return errors.New("httpclient: OAuth2Config.ClientID is required")
	case strings.TrimSpace(c.ClientSecret) == "":
		return errors.New("httpclient: OAuth2Config.ClientSecret is required")
	}
	return nil
}

// OAuth2Transport returns an http.RoundTripper that adds an OAuth2
// client_credentials bearer token to every request, refreshing and caching it
// automatically, then delegates to base (or http.DefaultTransport when nil).
//
// base is the transport for business requests — pass the wire transport here and
// compose retries/headers ABOVE this in the chain (httpmw.Chain), so a retried
// request re-attaches the current token. Token fetches go through their own
// small client (TokenTimeout), never through base, so they are not themselves
// retried or wrapped.
func OAuth2Transport(cfg OAuth2Config, base http.RoundTripper) (http.RoundTripper, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if base == nil {
		base = http.DefaultTransport
	}
	timeout := cfg.TokenTimeout
	if timeout <= 0 {
		timeout = defaultTokenTimeout
	}

	tokenClient := &http.Client{Timeout: timeout}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, tokenClient)

	cc := &clientcredentials.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		TokenURL:     cfg.TokenURL,
		Scopes:       cfg.Scopes,
	}
	return &oauth2.Transport{
		Source: cc.TokenSource(ctx),
		Base:   base,
	}, nil
}
