package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type credentialHTTPExecutionKey struct{}

type credentialHTTPExecution struct {
	transportContext     context.Context
	store                PrincipalAdmissionStore
	snapshot             CredentialExecutionSnapshot
	mu                   sync.Mutex
	dispatched, terminal bool
	status               int
	terminalOutcome      string
	headers              http.Header
	originalBody         []byte
}

// ForwardCredentialHTTP runs exactly one snapshot through the existing HTTP
// adapter. Calling the upstream twice (including an adapter compatibility retry)
// is refused. This entry does not select accounts or acquire legacy slots.
func (s *OpenAIGatewayService) ForwardCredentialHTTP(ctx context.Context, c *gin.Context, account *Account, body []byte, snapshot CredentialExecutionSnapshot, vault *CredentialVault, store PrincipalAdmissionStore) (result *OpenAIForwardResult, retErr error) {
	entered := false
	defer func() {
		if !entered {
			cleanup, end := context.WithTimeout(context.Background(), 3*time.Second)
			defer end()
			_ = store.Cancel(cleanup, snapshot.Lease)
		}
	}()
	if account == nil || account.ID != snapshot.AccountID || snapshot.Endpoint == "" {
		return nil, errors.New("INVALID_EXECUTION_SNAPSHOT")
	}
	if snapshot.Endpoint != "responses" && snapshot.Endpoint != "passthrough" && snapshot.Endpoint != "compact" && snapshot.Endpoint != "probe" {
		return nil, errors.New("GROUPED_TRANSPORT_UNSUPPORTED")
	}
	if GetOpenAIClientTransport(c) == OpenAIClientTransportWS {
		return nil, errors.New("GROUPED_TRANSPORT_UNSUPPORTED")
	}
	if err := validateCredentialHTTPInput(c.Request.Header, body); err != nil {
		return nil, err
	}
	if !snapshot.AccountUpdatedAt.IsZero() && !account.UpdatedAt.Equal(snapshot.AccountUpdatedAt) {
		return nil, errors.New("CONFIG_STALE")
	}
	secret, err := vault.Open(snapshot.SecretAAD, snapshot.SecretCiphertext)
	if err != nil {
		return nil, err
	}
	copyAccount := *account
	copyAccount.Credentials, err = cloneAccountJSONMap(account.Credentials)
	if err != nil {
		return nil, err
	}
	copyAccount.Extra, err = cloneAccountJSONMap(account.Extra)
	if err != nil {
		return nil, err
	}
	if copyAccount.Credentials == nil {
		copyAccount.Credentials = map[string]any{}
	}
	if copyAccount.Extra == nil {
		copyAccount.Extra = map[string]any{}
	}
	copyAccount.Credentials["access_token"] = secret.AccessToken
	if secret.AccountSubject != "" {
		copyAccount.Credentials["chatgpt_account_id"] = secret.AccountSubject
	}
	if secret.UserSubject != "" {
		copyAccount.Credentials["chatgpt_user_id"] = secret.UserSubject
	}
	// No refresh token is handed to protocol code. Versioned refresh is separate.
	delete(copyAccount.Credentials, "refresh_token")
	copyAccount.Extra["openai_device_id"] = snapshot.InstallationID
	copyAccount.Extra["codex_fingerprint_mode"] = "device"
	// Legacy namespace remains in credentials/seed. A new LOCAL_LOGICAL instance
	// has its own persistent generation seed, never derived from token material.
	if _, ok := codexFingerprintSeed(copyAccount.Extra); !ok {
		copyAccount.Extra["codex_fingerprint_seed"] = snapshot.Lease.Generation
	}
	copyAccount.ProxyID = snapshot.ProxyID
	if snapshot.Proxy != nil {
		proxy := *snapshot.Proxy
		copyAccount.Proxy = &proxy
	} else {
		copyAccount.Proxy = nil
	}
	execution := &credentialHTTPExecution{store: store, snapshot: snapshot, originalBody: append([]byte(nil), body...)}
	ctx = context.WithValue(ctx, credentialHTTPExecutionKey{}, execution)
	if !snapshot.Deadline.IsZero() {
		var end context.CancelFunc
		ctx, end = context.WithDeadline(ctx, snapshot.Deadline)
		defer end()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	execution.transportContext = ctx
	c.Request = c.Request.WithContext(ctx)
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				heartbeatCtx, end := context.WithTimeout(context.Background(), 3*time.Second)
				err := store.Heartbeat(heartbeatCtx, snapshot.Lease)
				end()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		execution.mu.Lock()
		terminal, status := execution.terminal, execution.status
		terminalOutcome := execution.terminalOutcome
		headers := execution.headers.Clone()
		execution.mu.Unlock()
		if observer, ok := store.(interface {
			ObserveCredentialFailure(context.Context, CredentialFailureObservation) error
		}); ok && (status >= 400 || execution.dispatched && status == 0) {
			obsCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			_ = observer.ObserveCredentialFailure(obsCtx, ClassifyCredentialFailure(snapshot, status, headers, terminal, time.Now()))
			stop()
		}
		outcome := "FAILED"
		if terminal && status >= 200 && status < 300 {
			outcome = "COMPLETED"
			if terminalOutcome != "" {
				outcome = terminalOutcome
			}
		}
		// Journal terminal usage before releasing the lease or scheduling billing.
		// If persistence fails, keep the attempt unknown/occupied; never free it
		// and silently lose the only observed usage facts.
		if result != nil {
			if err := s.persistCredentialUsage(ctx, result, store, snapshot.Lease, terminal, outcome); err != nil {
				terminal = false
				retErr = err
			}
		}
		finish := FinishAdmissionInput{Lease: snapshot.Lease, Complete: terminal, Outcome: outcome}
		if result != nil && terminal && result.Usage.HasBillableUsage() {
			input, output := int64(result.Usage.InputTokens), int64(result.Usage.OutputTokens)
			finish.InputTokens = &input
			finish.OutputTokens = &output
			finish.UpstreamRequestID = result.RequestID
		}
		finishCtx, end := context.WithTimeout(context.Background(), 5*time.Second)
		defer end()
		if err := store.Finish(finishCtx, finish); err != nil && retErr == nil {
			retErr = ErrAdmissionStoreUnavailable
		}
		if !terminal && execution.dispatched && retErr == nil {
			retErr = errors.New("UPSTREAM_RESULT_UNKNOWN")
		}
	}()
	entered = true
	return s.Forward(ctx, c, &copyAccount, body)
}

func credentialExecutionFromContext(ctx context.Context) *credentialHTTPExecution {
	v, _ := ctx.Value(credentialHTTPExecutionKey{}).(*credentialHTTPExecution)
	return v
}
func (e *credentialHTTPExecution) roundTrip(req *http.Request, accountID int64, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	e.mu.Lock()
	if e.dispatched || accountID != e.snapshot.AccountID {
		e.mu.Unlock()
		return nil, errors.New("GROUPED_REPLAY_DISABLED")
	}
	e.dispatched = true
	e.mu.Unlock()
	if len(e.originalBody) > 0 && e.snapshot.Endpoint != "compact" {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body.Close()
		rebuilt, err := preserveCredentialHTTPMetadata(e.originalBody, raw)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(rebuilt))
		req.ContentLength = int64(len(rebuilt))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(rebuilt)), nil }
	}
	if err := e.store.BeginDispatch(req.Context(), e.snapshot.Lease); err != nil {
		return nil, err
	}
	// Preserve protocol/profile context values while connecting detached request
	// cancellation to the owning execution. Do not replace UA/TLS context.
	cancelTransport := func() {}
	if e.transportContext != nil {
		transportCtx, cancel := context.WithCancel(req.Context())
		stop := context.AfterFunc(e.transportContext, cancel)
		cancelTransport = func() { stop(); cancel() }
		req = req.WithContext(transportCtx)
	}
	// The existing adapter may detach its context for usage draining. Grouped
	// ownership cancellation must still stop the actual transport.
	req.Header.Del("Idempotency-Key")
	req.Header.Del("X-Idempotency-Key")
	resp, err := send(req)
	if err != nil {
		cancelTransport()
		return resp, err
	}
	e.mu.Lock()
	e.status = resp.StatusCode
	e.headers = resp.Header.Clone()
	e.mu.Unlock()
	resp.Body = &credentialTerminalBody{ReadCloser: resp.Body, execution: e, cleanup: cancelTransport, sse: strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")}
	return resp, nil
}

// A transport EOF alone does not complete a stream; require a protocol terminal
// event. Preserve bytes as observed and bound the parser buffer independently.
type credentialTerminalBody struct {
	cleanup func()
	io.ReadCloser
	execution *credentialHTTPExecution
	sse       bool
	line      []byte
	overflow  bool
}

func (b *credentialTerminalBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if b.sse {
		for _, v := range p[:n] {
			if v == '\n' {
				if !b.overflow {
					b.observe(b.line)
				}
				b.line = b.line[:0]
				b.overflow = false
				continue
			}
			if len(b.line) < 1<<20 {
				b.line = append(b.line, v)
			} else {
				b.overflow = true
			}
		}
	} else if err == io.EOF {
		b.execution.mu.Lock()
		b.execution.terminal = true
		b.execution.mu.Unlock()
	}
	return n, err
}
func (b *credentialTerminalBody) observe(line []byte) {
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	data := bytes.TrimSpace(line[5:])
	if bytes.Equal(data, []byte("[DONE]")) {
		return
	}
	var event struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &event) != nil {
		return
	}
	switch event.Type {
	case "response.completed", "response.failed", "response.incomplete":
		b.execution.mu.Lock()
		b.execution.terminal = true
		if event.Type == "response.completed" {
			b.execution.terminalOutcome = "COMPLETED"
		} else {
			b.execution.terminalOutcome = "FAILED"
		}
		b.execution.mu.Unlock()
	}
}

func (b *credentialTerminalBody) Close() error {
	err := b.ReadCloser.Close()
	if b.cleanup != nil {
		b.cleanup()
	}
	return err
}
