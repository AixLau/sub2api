package proxy

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

// This file is the only sub2api-specific addition to the ported bps-local
// proxy package. It lets the plugin adapter reuse the bridge per request with
// credentials supplied by the host instead of a local OAuth file, so the rest
// of the package stays a verbatim snapshot that is easy to re-sync.

// StaticCredentials is the immutable identity the host resolved for the bound
// account of one request. The host owns refresh; the plugin never stores it.
type StaticCredentials struct {
	Token     string
	AccountID string
}

// Credentials implements CredentialSource.
func (c StaticCredentials) Credentials() (string, string, error) {
	if strings.TrimSpace(c.Token) == "" {
		return "", "", errors.New("host did not supply an OAuth access token")
	}
	return c.Token, c.AccountID, nil
}

// Expiry implements CredentialSource; the host tracks token expiry.
func (StaticCredentials) Expiry() time.Time { return time.Time{} }

// NewShared builds the root bridge whose replay cache every proxy-specific
// server shares. It owns no credentials.
func NewShared(cfg Config) (*Server, error) {
	return New(cfg, nil)
}

// ForProxy returns a server with its own outbound transport for proxyURL that
// keeps sharing the root replay cache. An empty proxyURL means a direct
// connection: environment proxies of the plugin process are ignored so the
// administrator's "disabled" choice is honoured.
func (s *Server) ForProxy(proxyURL string) (*Server, error) {
	cfg := s.cfg
	cfg.ProxyURL = strings.TrimSpace(proxyURL)
	next, err := New(cfg, nil)
	if err != nil {
		return nil, err
	}
	if cfg.ProxyURL == "" {
		if transport, ok := next.client.Transport.(*http.Transport); ok {
			transport.Proxy = nil
		}
	}
	next.replay = s.replay
	return next, nil
}

// CloseIdleConnections releases pooled upstream connections.
func (s *Server) CloseIdleConnections() {
	if s != nil && s.client != nil {
		s.client.CloseIdleConnections()
	}
}

// ServeResponses bridges one POST /responses request. The caller sets the
// session_id header to an account-scoped value so replayed tool identities
// never cross accounts or sessions.
func (s *Server) ServeResponses(w http.ResponseWriter, r *http.Request, credentials CredentialSource) {
	scoped := *s
	scoped.store = credentials
	scoped.handleResponses(w, r)
}
