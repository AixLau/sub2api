package service

import "context"

// contextMutex serializes process-local credential mutations while honoring cancellation.
// OAuth token refresh itself no longer uses a process-wide refresh lock; this mutex
// remains for the Grok credential mutation and Vertex service-account exchange paths.
type contextMutex struct {
	token chan struct{}
}

func newContextMutex() *contextMutex {
	return &contextMutex{token: make(chan struct{}, 1)}
}

func (m *contextMutex) Lock(ctx context.Context) error {
	select {
	case m.token <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *contextMutex) Unlock() {
	<-m.token
}
