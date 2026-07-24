package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	httppprof "net/http/pprof"
	"os"
	"strconv"
	"time"
)

const (
	pprofEnabledEnv = "SUB2API_PPROF_ENABLED"
	pprofListenAddr = "127.0.0.1:6060"
)

func pprofEnabled() bool {
	enabled, err := strconv.ParseBool(os.Getenv(pprofEnabledEnv))
	return err == nil && enabled
}

func newPprofServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", httppprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", httppprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", httppprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", httppprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", httppprof.Trace)

	return &http.Server{
		Addr:              pprofListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
}

func startPprofServer() *http.Server {
	if !pprofEnabled() {
		return nil
	}

	server := newPprofServer()
	go func() {
		log.Printf("pprof server listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("pprof server stopped unexpectedly: %v", err)
		}
	}()
	return server
}

func shutdownPprofServer(server *http.Server) {
	if server == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("pprof server forced to shutdown: %v", err)
	}
}
