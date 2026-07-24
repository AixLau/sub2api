package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPprofEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "unset", value: "", want: false},
		{name: "true", value: "true", want: true},
		{name: "one", value: "1", want: true},
		{name: "false", value: "false", want: false},
		{name: "invalid", value: "enabled", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(pprofEnabledEnv, tt.value)
			if got := pprofEnabled(); got != tt.want {
				t.Fatalf("pprofEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewPprofServerIsLoopbackOnly(t *testing.T) {
	server := newPprofServer()
	if server.Addr != pprofListenAddr {
		t.Fatalf("server.Addr = %q, want %q", server.Addr, pprofListenAddr)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /debug/pprof/ status = %d, want %d", recorder.Code, http.StatusOK)
	}
}
