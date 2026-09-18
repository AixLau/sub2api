// Command credential-rollout-mock is an isolated acceptance fixture. It never
// forwards traffic. Only its temporary CA is trusted by the test containers.
package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "client" {
		var in struct {
			URL, Method, Body string
			Headers           map[string]string
		}
		if json.NewDecoder(os.Stdin).Decode(&in) != nil {
			os.Exit(1)
		}
		result := struct {
			Status int    `json:"status"`
			Body   string `json:"body"`
			Error  string `json:"error,omitempty"`
		}{}
		request, err := http.NewRequest(in.Method, in.URL, strings.NewReader(in.Body))
		if err == nil {
			for key, value := range in.Headers {
				request.Header.Set(key, value)
			}
			var response *http.Response
			response, err = (&http.Client{Timeout: 25 * time.Second}).Do(request)
			if err == nil {
				result.Status = response.StatusCode
				data, readErr := io.ReadAll(response.Body)
				response.Body.Close()
				result.Body = string(data)
				err = readErr
			}
		}
		if err != nil {
			result.Error = err.Error()
		}
		_ = json.NewEncoder(os.Stdout).Encode(result)
		return
	}
	certificate, err := tls.LoadX509KeyPair("/fixture/mock.crt", "/fixture/mock.key")
	if err != nil {
		panic("fixture certificate unavailable")
	}
	var calls, unknown atomic.Int64
	var nextUnknown atomic.Bool
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			switch r.URL.Path {
			case "/stats":
				_ = json.NewEncoder(w).Encode(map[string]int64{"calls": calls.Load(), "unknown": unknown.Load()})
			case "/next-unknown":
				nextUnknown.Store(true)
				w.WriteHeader(http.StatusNoContent)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
			return
		}
		if r.Host != "chatgpt.com:443" {
			http.Error(w, "fixture target rejected", http.StatusForbidden)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		if rw.Flush() != nil {
			return
		}
		secure := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
		if secure.Handshake() != nil {
			return
		}
		reader := bufio.NewReader(secure)
		for {
			request, err := http.ReadRequest(reader)
			if err != nil {
				return
			}
			_, _ = io.Copy(io.Discard, request.Body)
			_ = request.Body.Close()
			if request.Method != http.MethodPost || !strings.HasPrefix(request.URL.Path, "/backend-api/codex/responses") {
				return
			}
			call := calls.Add(1)
			body := fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fixture_%d\",\"status\":\"completed\",\"model\":\"gpt-5.4\",\"output\":[],\"usage\":{\"input_tokens\":100,\"output_tokens\":20}}}\n\ndata: [DONE]\n\n", call)
			if nextUnknown.Swap(false) {
				unknown.Add(1)
				body = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_unknown\"}}\n\n"
			}
			_, _ = fmt.Fprintf(secure, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
			return
		}
	})
	if http.ListenAndServe(":8081", h) != nil {
		os.Exit(1)
	}
}
