package handler

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	clientSetupSessionTTL        = 10 * time.Minute
	clientSetupKeyPrefix         = "client_setup:"
	codexDefaultHighSpeedGroupID = int64(2)
)

type clientSetupAPIKeyCreator interface {
	Create(ctx context.Context, userID int64, req service.CreateAPIKeyRequest) (*service.APIKey, error)
	GetAvailableGroups(ctx context.Context, userID int64) ([]service.Group, error)
}

type clientSetupStore interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
}

type ClientSetupHandler struct {
	redisClient   clientSetupStore
	apiKeyCreator clientSetupAPIKeyCreator
	now           func() time.Time
}

type clientSetupSession struct {
	SetupID     string    `json:"setup_id"`
	DeviceCode  string    `json:"device_code"`
	PollToken   string    `json:"poll_token,omitempty"`
	Client      string    `json:"client"`
	RedirectURI string    `json:"redirect_uri,omitempty"`
	Status      string    `json:"status"`
	UserID      int64     `json:"user_id,omitempty"`
	APIKey      string    `json:"api_key,omitempty"`
	APIKeyID    int64     `json:"api_key_id,omitempty"`
	SetupToken  string    `json:"setup_token,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	ApprovedAt  time.Time `json:"approved_at,omitempty"`
	ExchangedAt time.Time `json:"exchanged_at,omitempty"`
}

type createClientSetupSessionRequest struct {
	Client      string `json:"client"`
	RedirectURI string `json:"redirect_uri"`
}

type approveClientSetupSessionRequest struct {
	DeviceCode string `json:"device_code"`
	Client     string `json:"client"`
}

type exchangeClientSetupSessionRequest struct {
	SetupID    string `json:"setup_id"`
	SetupToken string `json:"setup_token"`
}

func NewClientSetupHandler(redisClient clientSetupStore, apiKeyCreator clientSetupAPIKeyCreator) *ClientSetupHandler {
	return &ClientSetupHandler{
		redisClient:   redisClient,
		apiKeyCreator: apiKeyCreator,
		now:           time.Now,
	}
}

func (h *ClientSetupHandler) CreateSession(c *gin.Context) {
	if h.redisClient == nil {
		response.Error(c, http.StatusServiceUnavailable, "client setup service is unavailable")
		return
	}

	var req createClientSetupSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	client := normalizeClientSetupClient(req.Client)
	if client == "" {
		response.BadRequest(c, "Invalid client")
		return
	}
	if req.RedirectURI != "" && !isAllowedLocalRedirectURI(req.RedirectURI) {
		response.BadRequest(c, "Invalid redirect_uri")
		return
	}

	setupID, err := randomURLToken(24)
	if err != nil {
		response.InternalError(c, "failed to create setup session")
		return
	}
	deviceCode, err := randomDeviceCode()
	if err != nil {
		response.InternalError(c, "failed to create setup session")
		return
	}
	pollToken, err := randomURLToken(32)
	if err != nil {
		response.InternalError(c, "failed to create setup session")
		return
	}

	session := clientSetupSession{
		SetupID:     setupID,
		DeviceCode:  deviceCode,
		PollToken:   pollToken,
		Client:      client,
		RedirectURI: strings.TrimSpace(req.RedirectURI),
		Status:      "pending",
		CreatedAt:   h.now().UTC(),
	}
	if err := h.saveSession(c.Request.Context(), &session); err != nil {
		response.InternalError(c, "failed to save setup session")
		return
	}

	response.Success(c, gin.H{
		"setup_id":     session.SetupID,
		"device_code":  session.DeviceCode,
		"poll_token":   session.PollToken,
		"client":       session.Client,
		"status":       session.Status,
		"verify_url":   h.buildVerifyURL(c, &session),
		"expires_in":   int(clientSetupSessionTTL.Seconds()),
		"redirect_uri": session.RedirectURI,
	})
}

func (h *ClientSetupHandler) GetSession(c *gin.Context) {
	session, ok := h.loadSessionOrError(c, c.Param("setup_id"))
	if !ok {
		return
	}

	out := gin.H{
		"setup_id":    session.SetupID,
		"client":      session.Client,
		"status":      session.Status,
		"device_code": session.DeviceCode,
	}
	if session.Status == "approved" && h.validPollToken(c, session) && session.RedirectURI != "" && session.SetupToken != "" {
		out["redirect_uri"] = appendSetupCallback(session.RedirectURI, session.SetupID, session.SetupToken)
		out["setup_token"] = session.SetupToken
	}
	response.Success(c, out)
}

func (h *ClientSetupHandler) ApproveSession(c *gin.Context) {
	if h.apiKeyCreator == nil {
		response.Error(c, http.StatusServiceUnavailable, "client setup service is unavailable")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	session, ok := h.loadSessionOrError(c, c.Param("setup_id"))
	if !ok {
		return
	}
	if session.Status != "pending" {
		response.Error(c, http.StatusConflict, "setup session is not pending")
		return
	}

	var req approveClientSetupSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(req.DeviceCode)), []byte(session.DeviceCode)) != 1 {
		response.Forbidden(c, "Invalid device code")
		return
	}
	if client := normalizeClientSetupClient(req.Client); client != "" && client != session.Client {
		response.BadRequest(c, "Client mismatch")
		return
	}

	setupToken, err := randomURLToken(32)
	if err != nil {
		response.InternalError(c, "failed to create setup token")
		return
	}
	groupID, err := h.resolveClientSetupGroupID(c.Request.Context(), subject.UserID, session.Client)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	apiKey, err := h.apiKeyCreator.Create(c.Request.Context(), subject.UserID, service.CreateAPIKeyRequest{
		Name:    fmt.Sprintf("%s-auto-%s", session.Client, h.now().Format("20060102-150405")),
		GroupID: groupID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	session.Status = "approved"
	session.UserID = subject.UserID
	session.APIKey = apiKey.Key
	session.APIKeyID = apiKey.ID
	session.SetupToken = setupToken
	session.ApprovedAt = h.now().UTC()
	if err := h.saveSession(c.Request.Context(), session); err != nil {
		response.InternalError(c, "failed to approve setup session")
		return
	}

	response.Success(c, gin.H{
		"setup_id":     session.SetupID,
		"client":       session.Client,
		"status":       session.Status,
		"setup_token":  session.SetupToken,
		"redirect_uri": appendSetupCallback(session.RedirectURI, session.SetupID, session.SetupToken),
	})
}

func (h *ClientSetupHandler) Exchange(c *gin.Context) {
	var req exchangeClientSetupSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	session, ok := h.loadSessionOrError(c, req.SetupID)
	if !ok {
		return
	}
	if session.Status == "exchanged" {
		response.Error(c, http.StatusGone, "setup token has already been used")
		return
	}
	if session.Status != "approved" || session.SetupToken == "" || session.APIKey == "" {
		response.Error(c, http.StatusConflict, "setup session is not approved")
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(req.SetupToken)), []byte(session.SetupToken)) != 1 {
		response.Forbidden(c, "Invalid setup token")
		return
	}

	apiKey := session.APIKey
	session.APIKey = ""
	session.SetupToken = ""
	session.Status = "exchanged"
	session.ExchangedAt = h.now().UTC()
	if err := h.saveSession(c.Request.Context(), session); err != nil {
		response.InternalError(c, "failed to exchange setup token")
		return
	}

	response.Success(c, gin.H{
		"api_key":    apiKey,
		"api_key_id": session.APIKeyID,
		"client":     session.Client,
	})
}

func (h *ClientSetupHandler) loadSessionOrError(c *gin.Context, setupID string) (*clientSetupSession, bool) {
	setupID = strings.TrimSpace(setupID)
	if setupID == "" {
		response.BadRequest(c, "Missing setup_id")
		return nil, false
	}
	session, err := h.loadSession(c.Request.Context(), setupID)
	if err == redis.Nil {
		response.NotFound(c, "setup session not found or expired")
		return nil, false
	}
	if err != nil {
		response.InternalError(c, "failed to load setup session")
		return nil, false
	}
	return session, true
}

func (h *ClientSetupHandler) loadSession(ctx context.Context, setupID string) (*clientSetupSession, error) {
	raw, err := h.redisClient.Get(ctx, clientSetupKeyPrefix+setupID).Bytes()
	if err != nil {
		return nil, err
	}
	var session clientSetupSession
	if err := json.Unmarshal(raw, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (h *ClientSetupHandler) saveSession(ctx context.Context, session *clientSetupSession) error {
	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return h.redisClient.Set(ctx, clientSetupKeyPrefix+session.SetupID, raw, clientSetupSessionTTL).Err()
}

func (h *ClientSetupHandler) resolveClientSetupGroupID(ctx context.Context, userID int64, client string) (*int64, error) {
	groups, err := h.apiKeyCreator.GetAvailableGroups(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list available groups: %w", err)
	}

	targetPlatform := clientSetupTargetPlatform(client)
	if targetPlatform == "" {
		return nil, infraerrors.BadRequest("CLIENT_SETUP_INVALID_CLIENT", "Invalid client")
	}

	if group := selectClientSetupGroup(groups, targetPlatform, client); group != nil {
		groupID := group.ID
		return &groupID, nil
	}

	return nil, infraerrors.Forbidden("CLIENT_SETUP_NO_AVAILABLE_GROUP", "当前账号没有可用于该客户端的分组，请先购买订阅或联系管理员开通可用分组")
}

func clientSetupTargetPlatform(client string) string {
	switch client {
	case "codex":
		return service.PlatformOpenAI
	case "claude":
		return service.PlatformAnthropic
	default:
		return ""
	}
}

func selectClientSetupGroup(groups []service.Group, targetPlatform, client string) *service.Group {
	filtered := make([]service.Group, 0, len(groups))
	for _, group := range groups {
		if group.ID <= 0 || group.Platform != targetPlatform || !group.IsActive() {
			continue
		}
		filtered = append(filtered, group)
	}

	if group := bestClientSetupGroup(filtered, func(group service.Group) bool {
		return group.IsSubscriptionType()
	}); group != nil {
		return group
	}

	if client == "codex" {
		if group := bestClientSetupGroup(filtered, func(group service.Group) bool {
			return group.ID == codexDefaultHighSpeedGroupID
		}); group != nil {
			return group
		}
	}

	if client == "codex" {
		if group := bestClientSetupGroup(filtered, func(group service.Group) bool {
			return isPublicStandardGroup(group) && floatEquals(group.RateMultiplier, 1.3)
		}); group != nil {
			return group
		}
	}

	return bestClientSetupGroup(filtered, isPublicStandardGroup)
}

func bestClientSetupGroup(groups []service.Group, match func(service.Group) bool) *service.Group {
	var best *service.Group
	for i := range groups {
		group := groups[i]
		if !match(group) {
			continue
		}
		if best == nil || group.SortOrder < best.SortOrder || group.SortOrder == best.SortOrder && group.ID < best.ID {
			best = &groups[i]
		}
	}
	return best
}

func isPublicStandardGroup(group service.Group) bool {
	return group.SubscriptionType == service.SubscriptionTypeStandard && !group.IsExclusive
}

func floatEquals(a, b float64) bool {
	return math.Abs(a-b) < 0.000001
}

func (h *ClientSetupHandler) validPollToken(c *gin.Context, session *clientSetupSession) bool {
	pollToken := strings.TrimSpace(c.Query("poll_token"))
	return pollToken != "" &&
		session.PollToken != "" &&
		subtle.ConstantTimeCompare([]byte(pollToken), []byte(session.PollToken)) == 1
}

func (h *ClientSetupHandler) buildVerifyURL(c *gin.Context, session *clientSetupSession) string {
	scheme := "https"
	if c.Request != nil {
		if proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); proto != "" {
			scheme = strings.Split(proto, ",")[0]
		} else if c.Request.TLS == nil {
			scheme = "http"
		}
	}
	host := c.Request.Host
	if forwardedHost := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); forwardedHost != "" {
		host = strings.Split(forwardedHost, ",")[0]
	}
	u := url.URL{Scheme: scheme, Host: host, Path: "/client-setup"}
	q := u.Query()
	q.Set("setup_id", session.SetupID)
	q.Set("device_code", session.DeviceCode)
	q.Set("client", session.Client)
	u.RawQuery = q.Encode()
	return u.String()
}

func normalizeClientSetupClient(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "codex":
		return "codex"
	case "claude", "claude-code", "cc":
		return "claude"
	default:
		return ""
	}
}

func randomURLToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func randomDeviceCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	var out strings.Builder
	for i, b := range buf {
		if i == 4 {
			out.WriteByte('-')
		}
		out.WriteByte(alphabet[int(b)%len(alphabet)])
	}
	return out.String(), nil
}

func isAllowedLocalRedirectURI(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func appendSetupCallback(raw, setupID, setupToken string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("setup_id", setupID)
	q.Set("setup_token", setupToken)
	u.RawQuery = q.Encode()
	return u.String()
}
