package repository

import (
	"database/sql"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"net/http"
)

type credentialGuardedHTTPUpstream struct {
	delegate service.HTTPUpstream
	routes   service.CredentialRouteStore
}

func ProvideCredentialGuardedHTTPUpstream(cfg *config.Config, db *sql.DB) service.HTTPUpstream {
	return &credentialGuardedHTTPUpstream{delegate: NewHTTPUpstream(cfg), routes: NewCredentialRouteStore(db)}
}
func (g *credentialGuardedHTTPUpstream) check(req *http.Request, account int64) error {
	if account <= 0 {
		return nil
	}
	controlled, err := g.routes.IsControlledCredentialAccount(req.Context(), account)
	if err != nil {
		return service.ErrAdmissionStoreUnavailable
	}
	if controlled && !service.CredentialHTTPAccountAuthorized(req.Context(), account) {
		return errors.New("GROUPED_TRANSPORT_UNSUPPORTED")
	}
	return nil
}
func (g *credentialGuardedHTTPUpstream) Do(req *http.Request, proxy string, account int64, capacity int) (*http.Response, error) {
	if err := g.check(req, account); err != nil {
		return nil, err
	}
	return g.delegate.Do(req, proxy, account, capacity)
}
func (g *credentialGuardedHTTPUpstream) DoWithTLS(req *http.Request, proxy string, account int64, capacity int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	if err := g.check(req, account); err != nil {
		return nil, err
	}
	return g.delegate.DoWithTLS(req, proxy, account, capacity, profile)
}
