package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Only usage facts and internal IDs are durable here. No headers, Account maps,
// API key, prompt, output, session ID or token is serialized.
type CredentialUsageReceipt struct {
	GroupBillingDigest               string
	Lease                            LeaseRef
	Complete                         bool
	Outcome                          string
	ChannelDigest                    string
	LeaseID                          string
	UserID, APIKeyID, AccountID      int64
	GroupID                          *int64
	GroupUpdatedAt, AccountUpdatedAt time.Time
	SubscriptionID                   *int64
	PricingAt                        time.Time
	CatalogDigest                    string
	BaseMultiplier                   float64
	PayloadDigest                    string
	Result                           CredentialUsageResult
}
type CredentialUsageResult struct {
	Usage                                                                                          OpenAIUsage
	Model, BillingModel, UpstreamModel, UpstreamResponseModel, UpstreamRequestID, UpstreamEndpoint string
	ServiceTier, ReasoningEffort, RequestedReasoningEffort                                         *string
	Duration                                                                                       time.Duration
	Stream                                                                                         bool
	ImageCount                                                                                     int
	ImageSize                                                                                      string
}
type CredentialUsageReceiptStore interface {
	SaveCredentialUsageReceipt(context.Context, CredentialUsageReceipt) error
	PendingCredentialUsageReceipts(context.Context) ([]CredentialUsageReceipt, error)
	CompleteCredentialUsageReceipt(context.Context, string, string, string) error
}
type credentialUsageContextKey struct{}

func WithCredentialUsageContext(ctx context.Context, receipt CredentialUsageReceipt) context.Context {
	return context.WithValue(ctx, credentialUsageContextKey{}, receipt)
}
func (s *OpenAIGatewayService) CredentialUsageContext(ctx context.Context, apiKey *APIKey, account *Account, subscription *UserSubscription, lease string, payload []byte) context.Context {
	receipt := CredentialUsageReceipt{LeaseID: lease, UserID: apiKey.UserID, APIKeyID: apiKey.ID, AccountID: account.ID, GroupID: apiKey.GroupID, AccountUpdatedAt: account.UpdatedAt, PricingAt: OpenAIPricingAtFromContext(ctx), PayloadDigest: HashUsageRequestPayload(payload)}
	if receipt.PricingAt.IsZero() {
		receipt.PricingAt = time.Now()
	}
	if apiKey.Group != nil {
		receipt.GroupUpdatedAt = apiKey.Group.UpdatedAt
		receipt.GroupBillingDigest = credentialGroupBillingDigest(apiKey.Group)
		receipt.BaseMultiplier = s.ResolveUserGroupRateMultiplier(ctx, apiKey.UserID, apiKey.Group.ID, apiKey.Group.RateMultiplier)
	}
	if subscription != nil {
		id := subscription.ID
		receipt.SubscriptionID = &id
	}
	receipt.CatalogDigest = s.credentialCatalogDigest()
	receipt.ChannelDigest = s.credentialChannelDigest(ctx, apiKey.GroupID)
	return WithCredentialUsageContext(ctx, receipt)
}
func (s *OpenAIGatewayService) credentialCatalogDigest() string {
	if s.billingService == nil {
		return "unavailable"
	}
	var value any = s.billingService.fallbackPrices
	if pricing := s.billingService.pricingService; pricing != nil {
		pricing.mu.RLock()
		defer pricing.mu.RUnlock()
		value = pricing.pricingData
	}
	data, _ := json.Marshal(value)
	return CredentialDigest(data)
}
func (s *OpenAIGatewayService) persistCredentialUsage(ctx context.Context, result *OpenAIForwardResult, store PrincipalAdmissionStore, lease LeaseRef, complete bool, outcome string) error {
	receipt, ok := ctx.Value(credentialUsageContextKey{}).(CredentialUsageReceipt)
	if !ok || result == nil {
		return nil
	}
	sink, ok := store.(CredentialUsageReceiptStore)
	if !ok {
		return errors.New("CREDENTIAL_USAGE_STORE_UNAVAILABLE")
	}
	receipt.Lease = lease
	receipt.Complete = complete
	receipt.Outcome = outcome
	receipt.Result = CredentialUsageResult{Usage: result.Usage, Model: result.Model, BillingModel: result.BillingModel, UpstreamModel: result.UpstreamModel, UpstreamResponseModel: result.UpstreamResponseModel, UpstreamRequestID: result.RequestID, UpstreamEndpoint: result.UpstreamEndpoint, ServiceTier: result.ServiceTier, ReasoningEffort: result.ReasoningEffort, RequestedReasoningEffort: result.RequestedReasoningEffort, Duration: result.Duration, Stream: result.Stream, ImageCount: result.ImageCount, ImageSize: result.ImageSize}
	durable, end := context.WithTimeout(context.Background(), 5*time.Second)
	defer end()
	return sink.SaveCredentialUsageReceipt(durable, receipt)
}

// Recovery stops for changed pricing/configuration rather than charging a new
// price. Already priced commands remain recoverable by the billing outbox.
func (s *OpenAIGatewayService) RecoverCredentialUsage(ctx context.Context, store CredentialUsageReceiptStore, keys APIKeyRepository, updater APIKeyQuotaUpdater) error {
	receipts, err := store.PendingCredentialUsageReceipts(ctx)
	if err != nil {
		return err
	}
	for _, receipt := range receipts {
		if admission, ok := store.(PrincipalAdmissionStore); ok {
			r := receipt.Result
			finish := FinishAdmissionInput{Lease: receipt.Lease, Complete: receipt.Complete, Outcome: receipt.Outcome, UpstreamRequestID: r.UpstreamRequestID}
			if r.Usage.HasBillableUsage() {
				input, output := int64(r.Usage.InputTokens), int64(r.Usage.OutputTokens)
				finish.InputTokens = &input
				finish.OutputTokens = &output
			}
			if err = admission.Finish(ctx, finish); err != nil {
				return err
			}
		}
		key, err := keys.GetByID(ctx, receipt.APIKeyID)
		if err != nil {
			return err
		}
		account, err := s.accountRepo.GetByID(ctx, receipt.AccountID)
		if err != nil {
			return err
		}
		if receipt.ChannelDigest == "unavailable" || key.User == nil || key.Group == nil || key.UserID != receipt.UserID || credentialGroupBillingDigest(key.Group) != receipt.GroupBillingDigest || !account.UpdatedAt.Equal(receipt.AccountUpdatedAt) || s.credentialCatalogDigest() != receipt.CatalogDigest || s.credentialChannelDigest(ctx, receipt.GroupID) != receipt.ChannelDigest || s.ResolveUserGroupRateMultiplier(ctx, key.UserID, key.Group.ID, key.Group.RateMultiplier) != receipt.BaseMultiplier {
			if err = store.CompleteCredentialUsageReceipt(ctx, receipt.LeaseID, "REVIEW_REQUIRED", "BILLING_CONFIGURATION_CHANGED"); err != nil {
				return err
			}
			continue
		}
		var subscription *UserSubscription
		if receipt.SubscriptionID != nil {
			subscription, err = s.userSubRepo.GetByID(ctx, *receipt.SubscriptionID)
			if err != nil {
				return err
			}
		}
		r := receipt.Result
		result := &OpenAIForwardResult{RequestID: CredentialBillingRequestID(receipt.LeaseID), Usage: r.Usage, Model: r.Model, BillingModel: r.BillingModel, UpstreamModel: r.UpstreamModel, UpstreamResponseModel: r.UpstreamResponseModel, UpstreamEndpoint: r.UpstreamEndpoint, ServiceTier: r.ServiceTier, ReasoningEffort: r.ReasoningEffort, RequestedReasoningEffort: r.RequestedReasoningEffort, Duration: r.Duration, Stream: r.Stream, ImageCount: r.ImageCount, ImageSize: r.ImageSize}
		if err = s.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: key.User, APIKeyService: updater, Account: account, Subscription: subscription, PricingAt: receipt.PricingAt, RequestPayloadHash: receipt.PayloadDigest, QuotaPlatform: PlatformFromAPIKey(key), InboundEndpoint: "/v1/responses", UpstreamEndpoint: r.UpstreamEndpoint}); err != nil {
			return err
		}
		if err = store.CompleteCredentialUsageReceipt(ctx, receipt.LeaseID, "RECORDED", ""); err != nil {
			return err
		}
	}
	return nil
}

func (s *OpenAIGatewayService) credentialChannelDigest(ctx context.Context, group *int64) string {
	if s.channelService == nil || group == nil {
		return "none"
	}
	// Read the repository, not the asynchronously invalidated channel cache.
	id, err := s.channelService.repo.GetChannelIDByGroupID(ctx, *group)
	if err != nil {
		return "unavailable"
	}
	if id == 0 {
		return "none"
	}
	channel, err := s.channelService.repo.GetByID(ctx, id)
	if err != nil {
		return "unavailable"
	}
	pricing, err := s.channelService.repo.ListModelPricing(ctx, id)
	if err != nil {
		return "unavailable"
	}
	data, err := json.Marshal([]any{channel, pricing})
	if err != nil {
		return "unavailable"
	}
	return CredentialDigest(data)
}

func credentialGroupBillingDigest(g *Group) string {
	if g == nil {
		return "none"
	}
	// Auth snapshots intentionally omit timestamps and relationship counts. Compare
	// the actual billing policy fields rather than missing projection metadata.
	data, _ := json.Marshal([]any{g.ID, g.Platform, g.RateMultiplier, g.PeakRateEnabled, g.PeakStart, g.PeakEnd, g.PeakRateMultiplier, g.SubscriptionType,
		g.ImageRateIndependent, g.ImageRateMultiplier, g.ImagePrice1K, g.ImagePrice2K, g.ImagePrice4K,
		g.VideoRateIndependent, g.VideoRateMultiplier, g.VideoPrice480P, g.VideoPrice720P, g.VideoPrice1080P, g.VideoModelPrices,
		g.WebSearchPricePerCall, g.SearchPricePer1k, g.AudioRealtimePricePerMin, g.AudioTTSPricePerMillionChars, g.AudioSTTPricePerHour,
		g.LongContextPricingEnabled, g.ModelPricing, g.FreeOpenAIFast})
	return CredentialDigest(data)
}
