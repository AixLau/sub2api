package main

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProvideServiceBuildInfo(t *testing.T) {
	in := handler.BuildInfo{
		Version:   "v-test",
		Commit:    "abc1234",
		Date:      "2026-06-29T00:00:00Z",
		BuildType: "release",
	}
	out := provideServiceBuildInfo(in)
	require.Equal(t, in.Version, out.Version)
	require.Equal(t, in.Commit, out.Commit)
	require.Equal(t, in.Date, out.Date)
	require.Equal(t, in.BuildType, out.BuildType)
}

func TestProvideCleanup_WithMinimalDependencies_NoPanic(t *testing.T) {
	cfg := &config.Config{}

	oauthSvc := service.NewOAuthService(nil, nil)
	openAIOAuthSvc := service.NewOpenAIOAuthService(nil, nil)
	geminiOAuthSvc := service.NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	antigravityOAuthSvc := service.NewAntigravityOAuthService(nil)

	tokenRefreshSvc := service.NewTokenRefreshService(
		nil,
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil,
		nil,
		cfg,
		nil,
	)
	accountExpirySvc := service.NewAccountExpiryService(nil, time.Second)
	proxyExpirySvc := service.NewProxyExpiryService(nil, time.Second)
	subscriptionExpirySvc := service.NewSubscriptionExpiryService(nil, time.Second)
	pricingSvc := service.NewPricingService(cfg, nil)
	emailQueueSvc := service.NewEmailQueueService(nil, 1)
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	idempotencyCleanupSvc := service.NewIdempotencyCleanupService(nil, cfg)
	schedulerSnapshotSvc := service.NewSchedulerSnapshotService(nil, nil, nil, nil, cfg)
	opsSystemLogSinkSvc := service.NewOpsSystemLogSink(nil)
	moderationOutbox := newServerContentModerationOutboxRepo()
	contentModerationSvc := service.NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	contentModerationSvc.SetOutboxRepository(moderationOutbox)
	contentModerationSvc.Start(context.Background())
	t.Cleanup(contentModerationSvc.Close)
	select {
	case <-moderationOutbox.claimStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("content moderation outbox loop did not start")
	}

	cleanup := provideCleanup(
		nil, // entClient
		nil, // redis
		&service.OpsMetricsCollector{},
		&service.OpsAggregationService{},
		&service.OpsAlertEvaluatorService{},
		&service.OpsCleanupService{},
		&service.OpsScheduledReportService{},
		opsSystemLogSinkSvc,
		nil, // opsService
		nil, // opsIngressRejectAggregator
		nil, // apiKeyService
		nil, // authCacheInvalidationWorker
		schedulerSnapshotSvc,
		tokenRefreshSvc,
		accountExpirySvc,
		proxyExpirySvc,
		subscriptionExpirySvc,
		&service.UsageCleanupService{},
		idempotencyCleanupSvc,
		&service.BatchImageCleanupService{},
		nil, // batchImageWorker
		contentModerationSvc,
		pricingSvc,
		emailQueueSvc,
		billingCacheSvc,
		&service.UsageRecordWorkerPool{},
		&service.SubscriptionService{},
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil, // grokOAuth
		nil, // openAIGateway
		nil, // scheduledTestRunner
		nil, // backupSvc
		nil, // paymentOrderExpiry
		nil, // channelMonitorRunner
		nil, // quotaFlusher
		nil, // upstreamBillingProbe
		nil, // auditLog
		nil, // promptAudit
	)

	require.NotPanics(t, func() {
		cleanup()
	})
	select {
	case <-moderationOutbox.claimStopped:
	case <-time.After(time.Second):
		t.Fatal("server cleanup did not stop content moderation runtime")
	}
}

type serverContentModerationOutboxRepo struct {
	claimStarted chan struct{}
	claimStopped chan struct{}
}

func newServerContentModerationOutboxRepo() *serverContentModerationOutboxRepo {
	return &serverContentModerationOutboxRepo{
		claimStarted: make(chan struct{}),
		claimStopped: make(chan struct{}),
	}
}

func (r *serverContentModerationOutboxRepo) EnqueueEvents(context.Context, []service.ContentModerationOutboxEvent) error {
	return nil
}

func (r *serverContentModerationOutboxRepo) ClaimDueEvents(ctx context.Context, _ time.Time, _ int, _ time.Duration) ([]service.ContentModerationOutboxEvent, error) {
	select {
	case <-r.claimStarted:
	default:
		close(r.claimStarted)
	}
	<-ctx.Done()
	select {
	case <-r.claimStopped:
	default:
		close(r.claimStopped)
	}
	return nil, ctx.Err()
}

func (r *serverContentModerationOutboxRepo) MarkEventSucceeded(context.Context, int64) error {
	return nil
}

func (r *serverContentModerationOutboxRepo) ScheduleEventRetry(context.Context, int64, int, time.Time, string) error {
	return nil
}

func (r *serverContentModerationOutboxRepo) MarkEventDeadLetter(context.Context, int64, string) error {
	return nil
}

func (r *serverContentModerationOutboxRepo) GetStatus(context.Context, time.Time) (*service.ContentModerationOutboxStatus, error) {
	return &service.ContentModerationOutboxStatus{}, nil
}

func (r *serverContentModerationOutboxRepo) ListDeadLetters(context.Context, int) ([]service.ContentModerationOutboxEvent, error) {
	return nil, nil
}

func (r *serverContentModerationOutboxRepo) ReplayDeadLetter(context.Context, int64) (bool, error) {
	return false, nil
}

func (r *serverContentModerationOutboxRepo) Cleanup(context.Context, time.Time, time.Time, int) (int64, error) {
	return 0, nil
}
