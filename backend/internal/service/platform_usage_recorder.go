package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PlatformUsageLogWriter is the narrow persistence contract shared by account
// tests and platform-initiated calls such as content moderation.
type PlatformUsageLogWriter interface {
	Create(ctx context.Context, log *UsageLog) (inserted bool, err error)
}

// PlatformUsageRecord describes a completed platform-owned upstream call. It
// deliberately has no user or API-key fields: these calls must never affect a
// customer's balance, quota, or active-user statistics.
type PlatformUsageRecord struct {
	Source           UsageSource
	Account          *Account
	RequestID        string
	Model            string
	RequestedModel   string
	UpstreamModel    string
	GroupID          *int64
	Usage            OpenAIUsage
	RequestType      RequestType
	DurationMS       *int
	FirstTokenMS     *int
	UserAgent        *string
	InboundEndpoint  *string
	UpstreamEndpoint *string
	ImageCount       int
	ImageSize        string
}

// PlatformUsageRecorder persists non-billable platform operations while still
// calculating their model cost for account and operational reporting.
type PlatformUsageRecorder interface {
	Record(ctx context.Context, record PlatformUsageRecord) error
}

type platformUsageRecorder struct {
	writer          PlatformUsageLogWriter
	billingService  *BillingService
	pricingResolver *ModelPricingResolver
}

func NewPlatformUsageRecorder(
	writer PlatformUsageLogWriter,
	billingService *BillingService,
	pricingResolver *ModelPricingResolver,
) PlatformUsageRecorder {
	return &platformUsageRecorder{
		writer:          writer,
		billingService:  billingService,
		pricingResolver: pricingResolver,
	}
}

func (r *platformUsageRecorder) Record(ctx context.Context, record PlatformUsageRecord) error {
	if r == nil || r.writer == nil {
		return errors.New("platform usage recorder is unavailable")
	}
	if record.Account == nil || record.Account.ID <= 0 {
		return errors.New("platform usage record account is required")
	}
	source := record.Source.Normalize()
	if !source.IsPlatformOperation() {
		return errors.New("platform usage record requires a platform source")
	}

	requestID := strings.TrimSpace(record.RequestID)
	if requestID == "" {
		requestID = "platform-" + string(source) + "-" + uuid.NewString()
	}
	model := strings.TrimSpace(record.Model)
	if model == "" {
		return errors.New("platform usage record model is required")
	}
	requestedModel := strings.TrimSpace(record.RequestedModel)
	if requestedModel == "" {
		requestedModel = model
	}
	upstreamModel := strings.TrimSpace(record.UpstreamModel)
	if upstreamModel == requestedModel {
		upstreamModel = ""
	}

	inputTokens := record.Usage.InputTokens - record.Usage.CacheReadInputTokens - record.Usage.CacheCreationInputTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	requestType := record.RequestType.Normalize()
	if requestType == RequestTypeUnknown {
		requestType = RequestTypeSync
	}
	rateMultiplier := 1.0
	logEntry := &UsageLog{
		Source:                source,
		AccountID:             record.Account.ID,
		RequestID:             requestID,
		Model:                 model,
		RequestedModel:        requestedModel,
		InputTokens:           inputTokens,
		OutputTokens:          record.Usage.OutputTokens,
		CacheCreationTokens:   record.Usage.CacheCreationInputTokens,
		CacheReadTokens:       record.Usage.CacheReadInputTokens,
		ImageOutputTokens:     record.Usage.ImageOutputTokens,
		RateMultiplier:        rateMultiplier,
		ActualCost:            0,
		GroupID:               cloneInt64Ptr(record.GroupID),
		ImageCount:            record.ImageCount,
		RequestType:           requestType,
		DurationMs:            clonePlatformUsageIntPtr(record.DurationMS),
		FirstTokenMs:          clonePlatformUsageIntPtr(record.FirstTokenMS),
		UserAgent:             clonePlatformUsageStringPtr(record.UserAgent),
		InboundEndpoint:       clonePlatformUsageStringPtr(record.InboundEndpoint),
		UpstreamEndpoint:      clonePlatformUsageStringPtr(record.UpstreamEndpoint),
		AccountRateMultiplier: &rateMultiplier,
		CreatedAt:             time.Now().UTC(),
	}
	if upstreamModel != "" {
		logEntry.UpstreamModel = &upstreamModel
	}
	if imageSize := strings.TrimSpace(record.ImageSize); imageSize != "" {
		logEntry.ImageSize = &imageSize
	}
	logEntry.SyncRequestTypeAndLegacyFields()

	if r.billingService != nil {
		billingModel := model
		if upstreamModel != "" {
			billingModel = upstreamModel
		}
		requestCount := record.ImageCount
		if requestCount <= 0 {
			requestCount = 1
		}
		cost, err := r.billingService.CalculateCostUnified(CostInput{
			Ctx:   ctx,
			Model: billingModel,
			Tokens: UsageTokens{
				InputTokens:         inputTokens,
				OutputTokens:        record.Usage.OutputTokens,
				CacheCreationTokens: record.Usage.CacheCreationInputTokens,
				CacheReadTokens:     record.Usage.CacheReadInputTokens,
				ImageOutputTokens:   record.Usage.ImageOutputTokens,
			},
			RequestCount:   requestCount,
			SizeTier:       strings.TrimSpace(record.ImageSize),
			RateMultiplier: rateMultiplier,
			Resolver:       r.pricingResolver,
		})
		if err == nil && cost != nil {
			logEntry.InputCost = cost.InputCost
			logEntry.OutputCost = cost.OutputCost
			logEntry.CacheCreationCost = cost.CacheCreationCost
			logEntry.CacheReadCost = cost.CacheReadCost
			logEntry.ImageOutputCost = cost.ImageOutputCost
			logEntry.TotalCost = cost.TotalCost
			if cost.BillingMode != "" {
				billingMode := cost.BillingMode
				logEntry.BillingMode = &billingMode
			}
		}
	}

	inserted, err := r.writer.Create(ctx, logEntry)
	if err != nil {
		return err
	}
	if !inserted {
		return errors.New("platform usage record was not inserted")
	}
	return nil
}

func clonePlatformUsageIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func clonePlatformUsageStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func platformUsageStringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
