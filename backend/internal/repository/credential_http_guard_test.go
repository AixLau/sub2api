package repository

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type credentialRouteGuardStub struct {
	service.CredentialRouteStore
	controlled bool
	err        error
}

func (s credentialRouteGuardStub) IsControlledCredentialAccount(context.Context, int64) (bool, error) {
	return s.controlled, s.err
}

type credentialGuardUpstreamStub struct {
	service.HTTPUpstream
	calls int
}

func (s *credentialGuardUpstreamStub) Do(*http.Request, string, int64, int) (*http.Response, error) {
	s.calls++
	return &http.Response{StatusCode: 200}, nil
}
func TestCredentialHTTPGuardAT21AT40(t *testing.T) {
	delegate := &credentialGuardUpstreamStub{}
	guard := &credentialGuardedHTTPUpstream{delegate: delegate, routes: credentialRouteGuardStub{controlled: true}}
	_, err := guard.Do(httptest.NewRequest("POST", "http://mock/responses", nil), "", 1, 10)
	require.ErrorContains(t, err, "GROUPED_TRANSPORT_UNSUPPORTED")
	require.Zero(t, delegate.calls)
	guard.routes = credentialRouteGuardStub{err: errors.New("db unavailable")}
	_, err = guard.Do(httptest.NewRequest("POST", "http://mock/responses", nil), "", 1, 10)
	require.ErrorIs(t, err, service.ErrAdmissionStoreUnavailable)
	require.Zero(t, delegate.calls)
	guard.routes = credentialRouteGuardStub{}
	_, err = guard.Do(httptest.NewRequest("POST", "http://mock/responses", nil), "", 1, 10)
	require.NoError(t, err)
	require.Equal(t, 1, delegate.calls)
}
