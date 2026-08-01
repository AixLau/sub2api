package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestContentModerationRuntime_ConstructorDoesNotStartLoops(t *testing.T) {
	settingRepo := newContentModerationRuntimeSettingRepo(t)
	_ = NewContentModerationService(settingRepo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)

	select {
	case <-settingRepo.configRead:
		t.Fatal("constructor started a content moderation worker")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestContentModerationRuntime_StartIsIdempotent(t *testing.T) {
	outboxRepo := newContentModerationRuntimeOutboxRepo()
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	svc.SetOutboxRepository(outboxRepo)
	svc.runtimeTimings = contentModerationRuntimeTimings{
		workerIdleWait:     time.Hour,
		cleanupDelay:       time.Hour,
		cleanupInterval:    time.Hour,
		outboxPollInterval: time.Millisecond,
	}
	registerContentModerationRuntimeCleanup(t, svc)

	svc.Start(context.Background())
	svc.Start(context.Background())
	requireSignalWithin(t, outboxRepo.claimStarted, time.Second, "outbox loop did not start")

	select {
	case <-outboxRepo.secondClaimStarted:
		t.Fatal("duplicate Start launched a second outbox loop")
	case <-time.After(25 * time.Millisecond):
	}
}

func TestContentModerationRuntime_CloseStopsWorkerLoopWithinOneSecond(t *testing.T) {
	settingRepo := newContentModerationRuntimeSettingRepo(t)
	svc := NewContentModerationService(settingRepo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	closer := registerContentModerationRuntimeCleanup(t, svc)
	svc.workerCount = 1
	svc.runtimeTimings = contentModerationRuntimeTimings{
		workerIdleWait:     time.Hour,
		cleanupDelay:       time.Hour,
		cleanupInterval:    time.Hour,
		outboxPollInterval: time.Hour,
	}
	svc.Start(context.Background())
	requireSignalWithin(t, settingRepo.configRead, time.Second, "worker loop did not read configuration")

	requireContentModerationCloseWithin(t, closer, time.Second)
}

func TestContentModerationRuntime_CloseStopsCleanupLoopWithinOneSecond(t *testing.T) {
	settingRepo := newContentModerationRuntimeSettingRepo(t)
	repo := newContentModerationRuntimeRepo()
	svc := NewContentModerationService(settingRepo, repo, nil, nil, nil, nil, nil)
	closer := registerContentModerationRuntimeCleanup(t, svc)
	svc.workerCount = 0
	svc.runtimeTimings = contentModerationRuntimeTimings{
		workerIdleWait:     time.Hour,
		cleanupDelay:       time.Millisecond,
		cleanupInterval:    time.Hour,
		outboxPollInterval: time.Hour,
	}
	svc.Start(context.Background())
	requireSignalWithin(t, repo.cleanupStarted, time.Second, "cleanup loop did not start")

	requireContentModerationCloseWithin(t, closer, time.Second)
	requireSignalWithin(t, repo.cleanupStopped, time.Second, "cleanup call did not observe cancellation")
}

func TestContentModerationRuntime_NormalCancellationDoesNotLogCleanupFailure(t *testing.T) {
	var logOutput bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logOutput, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	settingRepo := newContentModerationRuntimeSettingRepo(t)
	repo := newContentModerationRuntimeRepo()
	svc := NewContentModerationService(settingRepo, repo, nil, nil, nil, nil, nil)
	closer := registerContentModerationRuntimeCleanup(t, svc)
	svc.workerCount = 0
	svc.runtimeTimings = contentModerationRuntimeTimings{
		workerIdleWait:     time.Hour,
		cleanupDelay:       time.Millisecond,
		cleanupInterval:    time.Hour,
		outboxPollInterval: time.Hour,
	}
	svc.Start(context.Background())
	requireSignalWithin(t, repo.cleanupStarted, time.Second, "cleanup loop did not start")

	requireContentModerationCloseWithin(t, closer, time.Second)
	require.NotContains(t, logOutput.String(), "content_moderation.cleanup_failed")
}

func TestContentModerationRuntime_NormalCancellationDuringConfigLoadDoesNotLogFailure(t *testing.T) {
	var logOutput bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logOutput, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	settingRepo := newContentModerationRuntimeBlockingSettingRepo()
	svc := NewContentModerationService(settingRepo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	closer := registerContentModerationRuntimeCleanup(t, svc)
	svc.workerCount = 0
	svc.runtimeTimings = contentModerationRuntimeTimings{
		workerIdleWait:     time.Hour,
		cleanupDelay:       time.Millisecond,
		cleanupInterval:    time.Hour,
		outboxPollInterval: time.Hour,
	}
	svc.Start(context.Background())
	requireSignalWithin(t, settingRepo.configRead, time.Second, "cleanup loop did not begin loading configuration")

	requireContentModerationCloseWithin(t, closer, time.Second)
	require.NotContains(t, logOutput.String(), "content_moderation.cleanup_load_config_failed")
}

func TestContentModerationRuntime_GenuineConfigLoadFailureIsLogged(t *testing.T) {
	var logOutput bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logOutput, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{errors: map[string]error{
			SettingKeyContentModerationConfig: errors.New("settings unavailable"),
		}},
		&contentModerationTestRepo{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	svc.runCleanupOnce(context.Background())

	require.Contains(t, logOutput.String(), "content_moderation.cleanup_load_config_failed")
	require.Contains(t, logOutput.String(), "settings unavailable")
}

func TestContentModerationRuntime_GenuineCleanupFailureIsLogged(t *testing.T) {
	var logOutput bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logOutput, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	svc := NewContentModerationService(
		newContentModerationRuntimeSettingRepo(t),
		&contentModerationRuntimeCleanupErrorRepo{
			contentModerationTestRepo: &contentModerationTestRepo{},
			err:                       errors.New("cleanup unavailable"),
		},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	svc.runCleanupOnce(context.Background())

	require.Contains(t, logOutput.String(), "content_moderation.cleanup_failed")
	require.Contains(t, logOutput.String(), "cleanup unavailable")
}

func TestContentModerationRuntime_CloseStopsOutboxLoopWithinOneSecond(t *testing.T) {
	outboxRepo := newContentModerationRuntimeOutboxRepo()
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	closer := registerContentModerationRuntimeCleanup(t, svc)
	svc.SetOutboxRepository(outboxRepo)
	svc.runtimeTimings = contentModerationRuntimeTimings{
		workerIdleWait:     time.Hour,
		cleanupDelay:       time.Hour,
		cleanupInterval:    time.Hour,
		outboxPollInterval: time.Millisecond,
	}
	svc.Start(context.Background())
	requireSignalWithin(t, outboxRepo.claimStarted, time.Second, "outbox loop did not start")

	requireContentModerationCloseWithin(t, closer, time.Second)
	requireSignalWithin(t, outboxRepo.claimStopped, time.Second, "outbox claim did not observe cancellation")
}

func TestContentModerationRuntime_CloseIsIdempotent(t *testing.T) {
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	closer := registerContentModerationRuntimeCleanup(t, svc)
	svc.Start(context.Background())

	requireContentModerationCloseWithin(t, closer, time.Second)
	requireDirectContentModerationCloseWithin(t, svc, time.Second)
}

func TestContentModerationRuntime_SetOutboxRepositoryAfterStartDoesNotReplaceRepository(t *testing.T) {
	initialRepo := newContentModerationRuntimeOutboxRepo()
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	registerContentModerationRuntimeCleanup(t, svc)
	svc.SetOutboxRepository(initialRepo)
	svc.runtimeTimings = contentModerationRuntimeTimings{
		workerIdleWait:     time.Hour,
		cleanupDelay:       time.Hour,
		cleanupInterval:    time.Hour,
		outboxPollInterval: time.Millisecond,
	}
	svc.Start(context.Background())
	requireSignalWithin(t, initialRepo.claimStarted, time.Second, "outbox loop did not start")

	svc.SetOutboxRepository(newContentModerationRuntimeOutboxRepo())
	svc.runtimeMu.Lock()
	configuredRepo := svc.outboxRepo
	svc.runtimeMu.Unlock()
	require.Same(t, initialRepo, configuredRepo)
}

func TestContentModerationRuntime_WireProviderStartsRuntime(t *testing.T) {
	settingRepo := newContentModerationRuntimeSettingRepo(t)
	svc := ProvideContentModerationService(
		settingRepo,
		&contentModerationTestRepo{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		&config.Config{Moderation: config.ModerationSecurityConfig{
			CacheHMACKeyVersion: 1,
			AllowedHosts:        []string{"api.openai.com", "open.bigmodel.cn"},
		}},
		BuildInfo{},
	)
	registerContentModerationRuntimeCleanup(t, svc)

	requireSignalWithin(t, settingRepo.configRead, time.Second, "Wire provider did not start content moderation runtime")
}

type contentModerationRuntimeSettingRepo struct {
	*contentModerationTestSettingRepo
	configRead chan struct{}
	readOnce   sync.Once
}

func newContentModerationRuntimeSettingRepo(t *testing.T) *contentModerationRuntimeSettingRepo {
	t.Helper()
	cfg := defaultContentModerationConfig()
	rawConfig, err := json.Marshal(cfg)
	require.NoError(t, err)
	return &contentModerationRuntimeSettingRepo{
		contentModerationTestSettingRepo: &contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyContentModerationConfig: string(rawConfig),
		}},
		configRead: make(chan struct{}),
	}
}

func (r *contentModerationRuntimeSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	if key == SettingKeyContentModerationConfig {
		r.readOnce.Do(func() { close(r.configRead) })
	}
	return r.contentModerationTestSettingRepo.GetValue(ctx, key)
}

type contentModerationRuntimeBlockingSettingRepo struct {
	*contentModerationTestSettingRepo
	configRead chan struct{}
	readOnce   sync.Once
}

func newContentModerationRuntimeBlockingSettingRepo() *contentModerationRuntimeBlockingSettingRepo {
	return &contentModerationRuntimeBlockingSettingRepo{
		contentModerationTestSettingRepo: &contentModerationTestSettingRepo{},
		configRead:                       make(chan struct{}),
	}
}

func (r *contentModerationRuntimeBlockingSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	if key != SettingKeyContentModerationConfig {
		return r.contentModerationTestSettingRepo.GetValue(ctx, key)
	}
	r.readOnce.Do(func() { close(r.configRead) })
	<-ctx.Done()
	return "", ctx.Err()
}

type contentModerationRuntimeRepo struct {
	*contentModerationTestRepo
	cleanupStarted chan struct{}
	cleanupStopped chan struct{}
	startOnce      sync.Once
	stopOnce       sync.Once
}

func newContentModerationRuntimeRepo() *contentModerationRuntimeRepo {
	return &contentModerationRuntimeRepo{
		contentModerationTestRepo: &contentModerationTestRepo{},
		cleanupStarted:            make(chan struct{}),
		cleanupStopped:            make(chan struct{}),
	}
}

func (r *contentModerationRuntimeRepo) CleanupExpiredLogs(ctx context.Context, hitBefore, nonHitBefore time.Time) (*ContentModerationCleanupResult, error) {
	r.startOnce.Do(func() { close(r.cleanupStarted) })
	<-ctx.Done()
	r.stopOnce.Do(func() { close(r.cleanupStopped) })
	return nil, ctx.Err()
}

type contentModerationRuntimeCleanupErrorRepo struct {
	*contentModerationTestRepo
	err error
}

func (r *contentModerationRuntimeCleanupErrorRepo) CleanupExpiredLogs(context.Context, time.Time, time.Time) (*ContentModerationCleanupResult, error) {
	return nil, r.err
}

type contentModerationRuntimeOutboxRepo struct {
	*contentModerationTestOutboxRepo
	claimStarted       chan struct{}
	secondClaimStarted chan struct{}
	claimStopped       chan struct{}
	claimCount         atomic.Int64
	startOnce          sync.Once
	secondStartOnce    sync.Once
	stopOnce           sync.Once
}

func newContentModerationRuntimeOutboxRepo() *contentModerationRuntimeOutboxRepo {
	return &contentModerationRuntimeOutboxRepo{
		contentModerationTestOutboxRepo: &contentModerationTestOutboxRepo{},
		claimStarted:                    make(chan struct{}),
		secondClaimStarted:              make(chan struct{}),
		claimStopped:                    make(chan struct{}),
	}
}

func (r *contentModerationRuntimeOutboxRepo) ClaimDueEvents(ctx context.Context, now time.Time, limit int, lockFor time.Duration) ([]ContentModerationOutboxEvent, error) {
	count := r.claimCount.Add(1)
	r.startOnce.Do(func() { close(r.claimStarted) })
	if count > 1 {
		r.secondStartOnce.Do(func() { close(r.secondClaimStarted) })
	}
	<-ctx.Done()
	r.stopOnce.Do(func() { close(r.claimStopped) })
	return nil, ctx.Err()
}

type contentModerationRuntimeCloser struct {
	svc       *ContentModerationService
	closeOnce sync.Once
	done      chan struct{}
}

func registerContentModerationRuntimeCleanup(t *testing.T, svc *ContentModerationService) *contentModerationRuntimeCloser {
	t.Helper()
	closer := &contentModerationRuntimeCloser{svc: svc, done: make(chan struct{})}
	t.Cleanup(func() {
		closer.start()
		select {
		case <-closer.done:
		case <-time.After(time.Second):
			t.Errorf("content moderation Close timed out during cleanup")
		}
	})
	return closer
}

func requireContentModerationCloseWithin(t *testing.T, closer *contentModerationRuntimeCloser, timeout time.Duration) {
	t.Helper()
	closer.start()
	requireSignalWithin(t, closer.done, timeout, "content moderation Close timed out")
}

func requireDirectContentModerationCloseWithin(t *testing.T, svc *ContentModerationService, timeout time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		svc.Close()
		close(done)
	}()
	requireSignalWithin(t, done, timeout, "second content moderation Close timed out")
}

func (c *contentModerationRuntimeCloser) start() {
	c.closeOnce.Do(func() {
		go func() {
			c.svc.Close()
			close(c.done)
		}()
	})
}

func requireSignalWithin(t *testing.T, signal <-chan struct{}, timeout time.Duration, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(timeout):
		t.Fatal(message)
	}
}
