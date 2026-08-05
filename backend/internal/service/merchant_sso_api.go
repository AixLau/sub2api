package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrMerchantSSOAPIKeyInvalid  = infraerrors.Unauthorized("MERCHANT_SSO_API_KEY_INVALID", "invalid merchant SSO API key")
	ErrMerchantSSOAPIKeyMissing  = infraerrors.ServiceUnavailable("MERCHANT_SSO_API_KEY_NOT_CONFIGURED", "merchant SSO API key is not configured")
	ErrMerchantSSORequestInvalid = infraerrors.BadRequest("MERCHANT_SSO_REQUEST_INVALID", "invalid merchant SSO request")
)

const merchantSSOIdentityProvider = "oidc"

type MerchantSSOAPIRequest struct {
	MerchantCode    string `json:"merchantCode"`
	UserID          string `json:"userId"`
	Username        string `json:"username"`
	Nickname        string `json:"nickname"`
	Email           string `json:"email"`
	Phone           string `json:"phone"`
	ExternalUserID  string `json:"externalUserId"`
	ExternalAccount string `json:"externalAccount"`
	StartTime       string `json:"startTime"`
	EndTime         string `json:"endTime"`
}

type MerchantSSOAPIResult struct {
	UserID      int64                       `json:"user_id"`
	Account     string                      `json:"account"`
	RedirectURL string                      `json:"redirect_url"`
	Status      string                      `json:"status"`
	Records     []MerchantSSORechargeRecord `json:"records,omitempty"`
}

type MerchantSSORechargeRecord struct {
	OrderNo         string `json:"order_no"`
	UserID          string `json:"user_id"`
	Amount          string `json:"amount"`
	Currency        string `json:"currency"`
	BalanceBefore   string `json:"balance_before,omitempty"`
	BalanceAfter    string `json:"balance_after,omitempty"`
	ChargeType      string `json:"charge_type,omitempty"`
	PayMethod       string `json:"pay_method,omitempty"`
	Status          string `json:"status"`
	PlatformOrderNo string `json:"platform_order_no,omitempty"`
	CreatedAt       string `json:"created_at"`
}

type MerchantSSOAPIService struct {
	entClient   *dbent.Client
	userRepo    UserRepository
	authService *AuthService
	adminAPIKey string
}

func NewMerchantSSOAPIService(entClient *dbent.Client, userRepo UserRepository, authService *AuthService, cfg *config.Config) *MerchantSSOAPIService {
	key := ""
	if cfg != nil {
		key = strings.TrimSpace(cfg.MerchantSSOAPI.APIKey)
	}
	return &MerchantSSOAPIService{entClient: entClient, userRepo: userRepo, authService: authService, adminAPIKey: key}
}

func (s *MerchantSSOAPIService) AuthenticateAPIKey(key string) error {
	if s == nil || s.adminAPIKey == "" {
		return ErrMerchantSSOAPIKeyMissing
	}
	if key == "" || subtleConstantTimeCompare(key, s.adminAPIKey) == false {
		return ErrMerchantSSOAPIKeyInvalid
	}
	return nil
}

func subtleConstantTimeCompare(a, b string) bool {
	// bcrypt is intentionally not used for the service key; compare fixed config
	// material in constant time after hashing both values to equal-length bytes.
	aHash, bHash := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(aHash[:], bHash[:]) == 1
}

func (s *MerchantSSOAPIService) RegisterLogin(ctx context.Context, req MerchantSSOAPIRequest) (*MerchantSSOAPIResult, error) {
	if err := validateMerchantSSORequest(req, true); err != nil {
		return nil, err
	}
	user, err := s.resolveOrCreateUser(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.resultWithLogin(ctx, user, req)
}

func (s *MerchantSSOAPIService) Login(ctx context.Context, req MerchantSSOAPIRequest) (*MerchantSSOAPIResult, error) {
	if err := validateMerchantSSORequest(req, false); err != nil {
		return nil, err
	}
	user, err := s.lookupUser(ctx, req)
	if err != nil {
		return nil, infraerrors.NotFound("MERCHANT_USER_NOT_FOUND", "merchant user is not registered")
	}
	if !user.IsActive() {
		return nil, ErrUserNotActive
	}
	return s.resultWithLogin(ctx, user, req)
}

func (s *MerchantSSOAPIService) RechargeRecords(ctx context.Context, req MerchantSSOAPIRequest) (*MerchantSSOAPIResult, error) {
	if err := validateMerchantSSORequest(req, false); err != nil {
		return nil, err
	}
	user, err := s.lookupUser(ctx, req)
	if err != nil {
		return nil, infraerrors.NotFound("MERCHANT_USER_NOT_FOUND", "merchant user is not registered")
	}
	q := s.entClient.PaymentOrder.Query().Where(paymentorder.UserIDEQ(user.ID), paymentorder.StatusIn(OrderStatusPaid, OrderStatusRecharging, OrderStatusCompleted))
	if t, ok := parseMerchantSSOTime(req.StartTime); ok {
		q = q.Where(paymentorder.PaidAtGTE(t))
	}
	if t, ok := parseMerchantSSOTime(req.EndTime); ok {
		q = q.Where(paymentorder.PaidAtLTE(t))
	}
	orders, err := q.Order(dbent.Desc(paymentorder.FieldPaidAt)).Limit(100).All(ctx)
	if err != nil {
		return nil, err
	}
	records := make([]MerchantSSORechargeRecord, 0, len(orders))
	for _, order := range orders {
		orderNo := order.OutTradeNo
		if strings.TrimSpace(orderNo) == "" {
			orderNo = fmt.Sprintf("sub2api-%d", order.ID)
		}
		createdAt := order.CreatedAt
		if order.PaidAt != nil {
			createdAt = *order.PaidAt
		}
		records = append(records, MerchantSSORechargeRecord{
			OrderNo:         orderNo,
			UserID:          strconv.FormatInt(user.ID, 10),
			Amount:          fmt.Sprintf("%.2f", order.Amount),
			Currency:        "CNY",
			Status:          strings.ToLower(order.Status),
			PlatformOrderNo: order.PaymentTradeNo,
			CreatedAt:       createdAt.Format(time.RFC3339),
		})
	}
	return &MerchantSSOAPIResult{UserID: user.ID, Account: merchantAccount(req, user), Status: user.Status, Records: records}, nil
}

func (s *MerchantSSOAPIService) resultWithLogin(ctx context.Context, user *User, req MerchantSSOAPIRequest) (*MerchantSSOAPIResult, error) {
	result := &MerchantSSOAPIResult{UserID: user.ID, Account: merchantAccount(req, user), Status: user.Status}
	if s.authService == nil {
		return nil, infraerrors.ServiceUnavailable("MERCHANT_SSO_AUTH_UNAVAILABLE", "merchant SSO login service is not configured")
	}
	pair, err := s.authService.GenerateTokenPair(ctx, user, "")
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("MERCHANT_SSO_LOGIN_UNAVAILABLE", "merchant SSO login is temporarily unavailable").WithCause(err)
	}
	params := url.Values{}
	params.Set("access_token", pair.AccessToken)
	params.Set("refresh_token", pair.RefreshToken)
	params.Set("expires_in", fmt.Sprintf("%d", pair.ExpiresIn))
	result.RedirectURL = "https://aixlau.me/auth/callback#" + params.Encode()
	return result, nil
}

func merchantAccount(req MerchantSSOAPIRequest, user *User) string {
	if value := strings.TrimSpace(req.ExternalAccount); value != "" {
		return value
	}
	if value := strings.TrimSpace(user.Username); value != "" {
		return value
	}
	return user.Email
}

func (s *MerchantSSOAPIService) lookupUser(ctx context.Context, req MerchantSSOAPIRequest) (*User, error) {
	key := merchantSSOIdentityKey(req)
	subject := merchantSSOSubject(req)
	if subject != "" {
		identity, err := s.entClient.AuthIdentity.Query().Where(authidentity.ProviderTypeEQ(merchantSSOIdentityProvider), authidentity.ProviderKeyEQ(key), authidentity.ProviderSubjectEQ(subject)).WithUser().Only(ctx)
		if err == nil && identity.Edges.User != nil {
			return s.userRepo.GetByID(ctx, identity.UserID)
		}
	}
	if strings.TrimSpace(req.Email) != "" {
		return s.userRepo.GetByEmail(ctx, strings.TrimSpace(req.Email))
	}
	return nil, infraerrors.NotFound("MERCHANT_USER_NOT_FOUND", "merchant user is not registered")
}

func (s *MerchantSSOAPIService) resolveOrCreateUser(ctx context.Context, req MerchantSSOAPIRequest) (*User, error) {
	if user, err := s.lookupUser(ctx, req); err == nil {
		return user, nil
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		digest := sha256.Sum256([]byte(merchantSSOIdentityKey(req)))
		email = fmt.Sprintf("merchant-%s@invalid.local", hex.EncodeToString(digest[:8]))
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = strings.TrimSpace(req.ExternalAccount)
		if username == "" {
			username = "merchant_" + req.ExternalUserID
		}
	}
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword(randomBytes, bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := &User{Email: email, Username: username, PasswordHash: string(hash), Role: RoleUser, Status: StatusActive, SignupSource: "oidc"}
	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}
	_, err = s.entClient.AuthIdentity.Create().SetUserID(user.ID).SetProviderType(merchantSSOIdentityProvider).SetProviderKey(merchantSSOIdentityKey(req)).SetProviderSubject(merchantSSOSubject(req)).SetMetadata(map[string]any{"merchant_code": req.MerchantCode, "external_account": req.ExternalAccount}).Save(ctx)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func merchantSSOIdentityKey(req MerchantSSOAPIRequest) string {
	return "merchant:" + strings.TrimSpace(req.MerchantCode)
}

func merchantSSOSubject(req MerchantSSOAPIRequest) string {
	if value := strings.TrimSpace(req.ExternalUserID); value != "" {
		return value
	}
	return strings.TrimSpace(req.UserID)
}
func validateMerchantSSORequest(req MerchantSSOAPIRequest, requireUser bool) error {
	if strings.TrimSpace(req.MerchantCode) == "" {
		return ErrMerchantSSORequestInvalid
	}
	if requireUser && strings.TrimSpace(req.UserID) == "" {
		return ErrMerchantSSORequestInvalid
	}
	if !requireUser && merchantSSOSubject(req) == "" && strings.TrimSpace(req.Email) == "" {
		return ErrMerchantSSORequestInvalid
	}
	return nil
}
func parseMerchantSSOTime(raw string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	return t, err == nil
}
