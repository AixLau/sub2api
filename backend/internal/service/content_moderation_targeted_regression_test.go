package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type targetedLeaseLostDeadLetterOutboxRepo struct {
	*contentModerationTestOutboxRepo
}

func (r *targetedLeaseLostDeadLetterOutboxRepo) MarkEventDeadLetter(context.Context, int64, time.Time, string) error {
	return ErrContentModerationOutboxLeaseLost
}

func targetedSemanticReviewConfig() *ContentModerationConfig {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.SemanticReview.Enabled = true
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerAll
	cfg.SemanticReview.PrimaryModel = ContentModerationSemanticReviewPrimaryModel
	cfg.SemanticReview.MaxInputRunes = ContentModerationSemanticReviewDefaultMaxInputRunes
	return cfg
}

func TestTargetedSemanticEnqueueIdentityAndDuplicateAccounting(t *testing.T) {
	outbox := &contentModerationTestOutboxRepo{}
	svc := &ContentModerationService{
		outboxRepo:           outbox,
		rawRequestEncryptor:  contentModerationTestEncryptor{},
		semanticReviewRouter: &contentModerationSemanticReviewRouterStub{},
	}
	cfg := targetedSemanticReviewConfig()
	content := ContentModerationInput{Text: "same request body"}
	input := ContentModerationCheckInput{
		RequestID:      "batch-request-1",
		UserID:         7,
		Protocol:       ContentModerationProtocolBatchImages,
		policyRevision: "policy-a",
	}

	require.True(t, svc.enqueueSemanticReviewAfterRules(context.Background(), input, cfg, content, "same-hash", &ContentModerationDecision{Allowed: true}))
	require.False(t, svc.enqueueSemanticReviewAfterRules(context.Background(), input, cfg, content, "same-hash", &ContentModerationDecision{Allowed: true}))
	input.policyRevision = "policy-b"
	require.True(t, svc.enqueueSemanticReviewAfterRules(context.Background(), input, cfg, content, "same-hash", &ContentModerationDecision{Allowed: true}))
	input.RequestID = "batch-request-2"
	require.True(t, svc.enqueueSemanticReviewAfterRules(context.Background(), input, cfg, content, "same-hash", &ContentModerationDecision{Allowed: true}))

	events := outbox.snapshotEvents()
	require.Len(t, events, 3)
	require.NotEqual(t, events[0].DecisionID, events[1].DecisionID)
	require.NotEqual(t, events[1].DecisionID, events[2].DecisionID)
	require.EqualValues(t, 3, svc.asyncEnqueued.Load())
	require.Zero(t, svc.asyncDropped.Load())
	require.Zero(t, svc.asyncErrors.Load())
}

func TestTargetedSemanticErrorSanitizerPreservesUTF8(t *testing.T) {
	require.Equal(t, strings.Repeat("错", 240), sanitizeSemanticReviewError(strings.Repeat("错", 300)))
}

func TestTargetedSemanticDeadLetterPersistsSingleErrorAudit(t *testing.T) {
	encryptor := contentModerationTestEncryptor{}
	encrypted, err := encryptor.Encrypt("review input")
	require.NoError(t, err)
	repo := &contentModerationTestRepo{}
	outbox := &contentModerationTestOutboxRepo{}
	svc := NewContentModerationService(nil, repo, nil, nil, nil, nil, nil)
	svc.SetOutboxRepository(outbox)
	svc.rawRequestEncryptor = encryptor
	svc.semanticReviewRouter = &contentModerationSemanticReviewRouterStub{err: errors.New("semantic upstream timeout")}
	cfg := targetedSemanticReviewConfig()
	decisionID := "targeted-semantic-dead-letter"
	payload := contentModerationOutboxPayload{
		Config: cfg,
		SemanticReview: &contentModerationSemanticReviewOutboxPayload{
			DecisionID:       decisionID,
			InputHash:        "targeted-dead-letter-hash",
			Input:            contentModerationSemanticReviewOutboxInput{RequestID: "targeted-dead-letter-request", UserID: 17},
			TextEncrypted:    encrypted,
			EvidenceComplete: false,
			EvidenceRevision: "targeted-evidence-v1",
		},
	}
	inserted, err := outbox.EnqueueEvents(context.Background(), []ContentModerationOutboxEvent{{
		DecisionID: decisionID,
		EventType:  ContentModerationOutboxEventSemanticReview,
		Priority:   ContentModerationOutboxPriorityStrong,
		MaxRetries: 1,
		Payload:    contentModerationOutboxPayloadMap(payload),
	}})
	require.NoError(t, err)
	require.Equal(t, 1, inserted)

	require.NoError(t, svc.ProcessContentModerationOutboxOnce(context.Background(), 1))

	require.Len(t, outbox.snapshotDead(), 1)
	require.EqualValues(t, 1, svc.asyncErrors.Load())
	require.EqualValues(t, 1, svc.asyncDropped.Load())
	require.Zero(t, svc.asyncProcessed.Load())
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, decisionID, logs[0].DecisionID)
	require.Equal(t, ContentModerationActionError, logs[0].Action)
	require.Equal(t, contentModerationDecisionSourceSemantic, logs[0].DecisionSource)
	require.Equal(t, "platform_openai", logs[0].ModerationProvider)
	require.Equal(t, "semantic_review_dead_letter", logs[0].RiskContextReason)
	require.Contains(t, logs[0].Error, "semantic upstream timeout")
	require.False(t, logs[0].UserViolationEligible)
}

func TestTargetedStaleSemanticDeadLetterDoesNotPersistAudit(t *testing.T) {
	encryptor := contentModerationTestEncryptor{}
	encrypted, err := encryptor.Encrypt("review input")
	require.NoError(t, err)
	repo := &contentModerationTestRepo{}
	baseOutbox := &contentModerationTestOutboxRepo{}
	outbox := &targetedLeaseLostDeadLetterOutboxRepo{contentModerationTestOutboxRepo: baseOutbox}
	svc := NewContentModerationService(nil, repo, nil, nil, nil, nil, nil)
	svc.SetOutboxRepository(outbox)
	svc.rawRequestEncryptor = encryptor
	svc.semanticReviewRouter = &contentModerationSemanticReviewRouterStub{err: errors.New("semantic upstream timeout")}
	cfg := targetedSemanticReviewConfig()
	decisionID := "targeted-stale-semantic-dead-letter"
	inserted, err := outbox.EnqueueEvents(context.Background(), []ContentModerationOutboxEvent{{
		DecisionID: decisionID,
		EventType:  ContentModerationOutboxEventSemanticReview,
		Priority:   ContentModerationOutboxPriorityStrong,
		MaxRetries: 1,
		Payload: contentModerationOutboxPayloadMap(contentModerationOutboxPayload{
			Config: cfg,
			SemanticReview: &contentModerationSemanticReviewOutboxPayload{
				DecisionID:    decisionID,
				InputHash:     "targeted-stale-dead-letter-hash",
				Input:         contentModerationSemanticReviewOutboxInput{RequestID: "targeted-stale-dead-letter-request", UserID: 17},
				TextEncrypted: encrypted,
			},
		}),
	}})
	require.NoError(t, err)
	require.Equal(t, 1, inserted)

	require.NoError(t, svc.ProcessContentModerationOutboxOnce(context.Background(), 1))

	require.Empty(t, baseOutbox.snapshotDead())
	require.Zero(t, svc.asyncErrors.Load())
	require.Zero(t, svc.asyncDropped.Load())
	require.Empty(t, repo.snapshotLogs())
}

func TestTargetedSemanticProcessedCounterRequiresSuccessfulAck(t *testing.T) {
	encryptor := contentModerationTestEncryptor{}
	encrypted, err := encryptor.Encrypt("review input")
	require.NoError(t, err)
	repo := &contentModerationTestRepo{}
	baseOutbox := &contentModerationTestOutboxRepo{}
	outbox := &contentModerationFailingAckOutboxRepo{contentModerationTestOutboxRepo: baseOutbox}
	svc := NewContentModerationService(nil, repo, nil, nil, nil, nil, nil)
	svc.SetOutboxRepository(outbox)
	svc.rawRequestEncryptor = encryptor
	svc.semanticReviewRouter = &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "allow", Severity: "low", Confidence: 0.95, Model: ContentModerationSemanticReviewPrimaryModel,
	}}
	cfg := targetedSemanticReviewConfig()
	decisionID := "targeted-semantic-ack-failure"
	payload := contentModerationOutboxPayload{
		Config: cfg,
		SemanticReview: &contentModerationSemanticReviewOutboxPayload{
			DecisionID:    decisionID,
			InputHash:     "targeted-ack-failure-hash",
			Input:         contentModerationSemanticReviewOutboxInput{RequestID: "targeted-ack-failure-request", UserID: 17},
			TextEncrypted: encrypted,
		},
	}
	inserted, err := outbox.EnqueueEvents(context.Background(), []ContentModerationOutboxEvent{{
		DecisionID: decisionID,
		EventType:  ContentModerationOutboxEventSemanticReview,
		Priority:   ContentModerationOutboxPriorityStrong,
		Payload:    contentModerationOutboxPayloadMap(payload),
	}})
	require.NoError(t, err)
	require.Equal(t, 1, inserted)

	require.NoError(t, svc.ProcessContentModerationOutboxOnce(context.Background(), 1))

	require.Zero(t, svc.asyncProcessed.Load())
	require.Empty(t, baseOutbox.snapshotSucceeded())
	require.Len(t, repo.snapshotLogs(), 1)
}
