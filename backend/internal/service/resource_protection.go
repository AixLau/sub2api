package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultMaxRequestBodyMiB        = 50
	DefaultInflightMemoryBudgetMiB  = 400
	DefaultRequestMemoryMultiplier  = 4
	DefaultMinimumRequestChargeKiB  = 256
	DefaultSmallRequestThresholdMiB = 1
	DefaultSmallRequestReserveMiB   = 64
	DefaultAdmissionWaitTimeoutMS   = 5000
	DefaultImageAuditMaxConcurrency = 5
	DefaultRequestAuditTimeoutMS    = 30000
	defaultRuntimeSafeMaximumMiB    = 1024
)

var ErrRequestMemoryBudgetExhausted = errors.New("request memory budget exhausted")

type RequestMemoryBudgetExhaustedError struct {
	RequestContentLength int64 `json:"request_content_length"`
	EstimatedChargeBytes int64 `json:"estimated_charge_bytes"`
	ActiveBytes          int64 `json:"active_bytes"`
	AdmissionLimitBytes  int64 `json:"admission_limit_bytes"`
	AvailableBytes       int64 `json:"available_bytes"`
	ActiveReservations   int   `json:"active_reservations"`
	WaitingRequests      int   `json:"waiting_requests"`
	AdmissionWaitMS      int   `json:"admission_wait_ms"`
	AmbiguousLength      bool  `json:"ambiguous_length"`
	SmallRequest         bool  `json:"small_request"`
}

func (e *RequestMemoryBudgetExhaustedError) Error() string {
	return ErrRequestMemoryBudgetExhausted.Error()
}

func (e *RequestMemoryBudgetExhaustedError) Unwrap() error {
	return ErrRequestMemoryBudgetExhausted
}

type ResourceProtectionConfig struct {
	MaxRequestBodyMiB        int `json:"max_request_body_mib"`
	InflightMemoryBudgetMiB  int `json:"inflight_memory_budget_mib"`
	RequestMemoryMultiplier  int `json:"request_memory_multiplier"`
	MinimumRequestChargeKiB  int `json:"minimum_request_charge_kib"`
	SmallRequestThresholdMiB int `json:"small_request_threshold_mib"`
	SmallRequestReserveMiB   int `json:"small_request_reserve_mib"`
	AdmissionWaitTimeoutMS   int `json:"admission_wait_timeout_ms"`
	ImageAuditMaxConcurrency int `json:"image_audit_max_concurrency"`
	RequestAuditTimeoutMS    int `json:"request_audit_timeout_ms"`
}

func DefaultResourceProtectionConfig() ResourceProtectionConfig {
	return ResourceProtectionConfig{DefaultMaxRequestBodyMiB, DefaultInflightMemoryBudgetMiB, DefaultRequestMemoryMultiplier, DefaultMinimumRequestChargeKiB, DefaultSmallRequestThresholdMiB, DefaultSmallRequestReserveMiB, DefaultAdmissionWaitTimeoutMS, DefaultImageAuditMaxConcurrency, DefaultRequestAuditTimeoutMS}
}

func (c *ResourceProtectionConfig) Normalize() {
	d := DefaultResourceProtectionConfig()
	if c.MaxRequestBodyMiB == 0 {
		c.MaxRequestBodyMiB = d.MaxRequestBodyMiB
	}
	if c.InflightMemoryBudgetMiB == 0 {
		c.InflightMemoryBudgetMiB = d.InflightMemoryBudgetMiB
	}
	if c.RequestMemoryMultiplier == 0 {
		c.RequestMemoryMultiplier = d.RequestMemoryMultiplier
	}
	if c.MinimumRequestChargeKiB == 0 {
		c.MinimumRequestChargeKiB = d.MinimumRequestChargeKiB
	}
	if c.SmallRequestThresholdMiB == 0 {
		c.SmallRequestThresholdMiB = d.SmallRequestThresholdMiB
	}
	if c.SmallRequestReserveMiB == 0 {
		c.SmallRequestReserveMiB = d.SmallRequestReserveMiB
	}
	if c.AdmissionWaitTimeoutMS == 0 {
		c.AdmissionWaitTimeoutMS = d.AdmissionWaitTimeoutMS
	}
	if c.ImageAuditMaxConcurrency == 0 {
		c.ImageAuditMaxConcurrency = d.ImageAuditMaxConcurrency
	}
	if c.RequestAuditTimeoutMS == 0 {
		c.RequestAuditTimeoutMS = d.RequestAuditTimeoutMS
	}
}

func (c ResourceProtectionConfig) Validate(runtimeMaxMiB int) error {
	if runtimeMaxMiB <= 0 {
		runtimeMaxMiB = defaultRuntimeSafeMaximumMiB
	}
	ranges := []struct {
		name            string
		value, min, max int
	}{
		{"max_request_body_mib", c.MaxRequestBodyMiB, 1, 256}, {"inflight_memory_budget_mib", c.InflightMemoryBudgetMiB, 64, minInt(4096, runtimeMaxMiB)},
		{"request_memory_multiplier", c.RequestMemoryMultiplier, 2, 8}, {"minimum_request_charge_kib", c.MinimumRequestChargeKiB, 64, 4096},
		{"small_request_threshold_mib", c.SmallRequestThresholdMiB, 1, 8}, {"small_request_reserve_mib", c.SmallRequestReserveMiB, 16, 512},
		{"admission_wait_timeout_ms", c.AdmissionWaitTimeoutMS, 0, 60000}, {"image_audit_max_concurrency", c.ImageAuditMaxConcurrency, 1, 32},
		{"request_audit_timeout_ms", c.RequestAuditTimeoutMS, 1000, 300000},
	}
	for _, r := range ranges {
		if r.value < r.min || r.value > r.max {
			return fmt.Errorf("%s must be between %d and %d", r.name, r.min, r.max)
		}
	}
	maxBody := int64(c.MaxRequestBodyMiB) << 20
	budget := int64(c.InflightMemoryBudgetMiB) << 20
	reserve := int64(c.SmallRequestReserveMiB) << 20
	if int64(c.MinimumRequestChargeKiB)<<10 > maxBody {
		return errors.New("minimum_request_charge_kib must not exceed max_request_body_mib")
	}
	if reserve >= budget {
		return errors.New("small_request_reserve_mib must be less than inflight_memory_budget_mib")
	}
	if maxBody*int64(c.RequestMemoryMultiplier) > budget-reserve {
		return errors.New("maximum request charge must fit in the large-request budget")
	}
	return nil
}

type ResourceProtectionStatus struct {
	Config                ResourceProtectionConfig `json:"effective"`
	RuntimeSafeMaximumMiB int                      `json:"runtime_safe_maximum_mib"`
	ActiveBytes           int64                    `json:"active_bytes"`
	ActiveReservations    int                      `json:"active_reservations"`
	WaitingRequests       int                      `json:"waiting_requests"`
	ActiveImageAudits     int64                    `json:"active_image_audits"`
}

type resourceWaiter struct {
	charge  int64
	small   bool
	ready   chan struct{}
	granted bool
}

type imageWaiter struct {
	ready   chan struct{}
	granted bool
}
type ResourceReservation struct {
	manager *ResourceProtectionManager
	charge  int64
	once    sync.Once
}

func (r *ResourceReservation) Release() {
	if r != nil && r.manager != nil {
		r.once.Do(func() { r.manager.release(r.charge) })
	}
}

type ResourceProtectionManager struct {
	mu                 sync.Mutex
	config             ResourceProtectionConfig
	runtimeMaxMiB      int
	activeBytes        int64
	activeReservations int
	waiters            []*resourceWaiter
	imageWaiters       []*imageWaiter
	activeImageAudits  int64
}

type resourceProtectionContextKey struct{}
type resourceProtectionContextValue struct {
	reservation *ResourceReservation
	config      ResourceProtectionConfig
}

func (s *ContentModerationService) AcquireRequestResources(ctx context.Context, contentLength int64, contentEncoding string) (context.Context, error) {
	if s == nil {
		return ctx, errors.New("resource protection service unavailable")
	}
	ctx = withContentModerationInputCache(ctx)
	if s.resourceProtection == nil {
		s.resourceProtection = NewResourceProtectionManager(DefaultResourceProtectionConfig())
	}
	if s.settingRepo != nil {
		_, _ = s.loadConfig(ctx)
	}
	cfg := s.resourceProtection.Snapshot()
	r, err := s.resourceProtection.Acquire(ctx, contentLength, contentEncoding != "" && contentEncoding != "identity")
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, resourceProtectionContextKey{}, resourceProtectionContextValue{r, cfg}), nil
}

func ReleaseRequestResources(ctx context.Context) {
	if ctx == nil {
		return
	}
	v, _ := ctx.Value(resourceProtectionContextKey{}).(resourceProtectionContextValue)
	if v.reservation != nil {
		v.reservation.Release()
	}
}
func RequestBodyLimit(ctx context.Context) int64 {
	if ctx != nil {
		if v, ok := ctx.Value(resourceProtectionContextKey{}).(resourceProtectionContextValue); ok {
			return int64(v.config.MaxRequestBodyMiB) << 20
		}
	}
	return int64(DefaultMaxRequestBodyMiB) << 20
}
func (s *ContentModerationService) ResourceProtectionStatus() ResourceProtectionStatus {
	if s == nil || s.resourceProtection == nil {
		return ResourceProtectionStatus{Config: DefaultResourceProtectionConfig(), RuntimeSafeMaximumMiB: detectRuntimeSafeMaximumMiB()}
	}
	return s.resourceProtection.Status()
}

func NewResourceProtectionManager(cfg ResourceProtectionConfig) *ResourceProtectionManager {
	cfg.Normalize()
	m := &ResourceProtectionManager{config: cfg, runtimeMaxMiB: detectRuntimeSafeMaximumMiB()}
	return m
}

func (m *ResourceProtectionManager) Snapshot() ResourceProtectionConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config
}
func (m *ResourceProtectionManager) Update(cfg ResourceProtectionConfig) error {
	cfg.Normalize()
	if err := cfg.Validate(m.runtimeMaxMiB); err != nil {
		return err
	}
	m.mu.Lock()
	m.config = cfg
	m.grantLocked()
	m.grantImagesLocked()
	m.mu.Unlock()
	return nil
}
func (m *ResourceProtectionManager) Status() ResourceProtectionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return ResourceProtectionStatus{m.config, m.runtimeMaxMiB, m.activeBytes, m.activeReservations, len(m.waiters), m.activeImageAudits}
}

func (m *ResourceProtectionManager) Acquire(ctx context.Context, contentLength int64, compressed bool) (*ResourceReservation, error) {
	m.mu.Lock()
	cfg := m.config
	maxBody := int64(cfg.MaxRequestBodyMiB) << 20
	if contentLength > maxBody {
		m.mu.Unlock()
		return nil, &RequestBodyTooLargeError{Limit: maxBody}
	}
	ambiguous := compressed || contentLength <= 0
	base := contentLength
	if ambiguous {
		base = maxBody
	}
	minimum := int64(cfg.MinimumRequestChargeKiB) << 10
	if base < minimum {
		base = minimum
	}
	charge := base * int64(cfg.RequestMemoryMultiplier)
	small := !ambiguous && contentLength <= int64(cfg.SmallRequestThresholdMiB)<<20
	w := &resourceWaiter{charge: charge, small: small, ready: make(chan struct{})}
	m.waiters = append(m.waiters, w)
	m.grantLocked()
	m.mu.Unlock()
	timeout := time.Duration(cfg.AdmissionWaitTimeoutMS) * time.Millisecond
	var timer <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timer = t.C
	}
	select {
	case <-w.ready:
		return &ResourceReservation{manager: m, charge: charge}, nil
	case <-ctx.Done():
		if m.cancel(w) {
			m.release(charge)
		}
		return nil, ctx.Err()
	case <-timer:
		if m.cancel(w) {
			m.release(charge)
		}
		return nil, m.memoryBudgetExhaustedError(contentLength, charge, ambiguous, small, cfg.AdmissionWaitTimeoutMS)
	default:
		if timeout == 0 {
			m.cancel(w)
			return nil, m.memoryBudgetExhaustedError(contentLength, charge, ambiguous, small, 0)
		}
	}
	select {
	case <-w.ready:
		return &ResourceReservation{manager: m, charge: charge}, nil
	case <-ctx.Done():
		if m.cancel(w) {
			m.release(charge)
		}
		return nil, ctx.Err()
	case <-timer:
		if m.cancel(w) {
			m.release(charge)
		}
		return nil, m.memoryBudgetExhaustedError(contentLength, charge, ambiguous, small, cfg.AdmissionWaitTimeoutMS)
	}
}

func (m *ResourceProtectionManager) memoryBudgetExhaustedError(contentLength, charge int64, ambiguous, small bool, waitMS int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	limit := int64(m.config.InflightMemoryBudgetMiB) << 20
	if !small {
		limit -= int64(m.config.SmallRequestReserveMiB) << 20
	}
	available := limit - m.activeBytes
	if available < 0 {
		available = 0
	}
	return &RequestMemoryBudgetExhaustedError{
		RequestContentLength: contentLength,
		EstimatedChargeBytes: charge,
		ActiveBytes:          m.activeBytes,
		AdmissionLimitBytes:  limit,
		AvailableBytes:       available,
		ActiveReservations:   m.activeReservations,
		WaitingRequests:      len(m.waiters),
		AdmissionWaitMS:      waitMS,
		AmbiguousLength:      ambiguous,
		SmallRequest:         small,
	}
}

func (m *ResourceProtectionManager) canGrantLocked(w *resourceWaiter) bool {
	budget := int64(m.config.InflightMemoryBudgetMiB) << 20
	reserve := int64(m.config.SmallRequestReserveMiB) << 20
	limit := budget
	if !w.small {
		limit = budget - reserve
	}
	return m.activeBytes+w.charge <= limit
}
func (m *ResourceProtectionManager) grantLocked() {
	for i := 0; i < len(m.waiters); {
		w := m.waiters[i]
		if !m.canGrantLocked(w) {
			i++
			continue
		}
		m.waiters = append(m.waiters[:i], m.waiters[i+1:]...)
		w.granted = true
		m.activeBytes += w.charge
		m.activeReservations++
		close(w.ready)
	}
}
func (m *ResourceProtectionManager) cancel(w *resourceWaiter) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w.granted {
		return true
	}
	for i, x := range m.waiters {
		if x == w {
			m.waiters = append(m.waiters[:i], m.waiters[i+1:]...)
			break
		}
	}
	return false
}
func (m *ResourceProtectionManager) release(charge int64) {
	m.mu.Lock()
	m.activeBytes -= charge
	m.activeReservations--
	if m.activeBytes < 0 {
		m.activeBytes = 0
	}
	m.grantLocked()
	m.mu.Unlock()
}
func (m *ResourceProtectionManager) AcquireImage(ctx context.Context) (func(), error) {
	w := &imageWaiter{ready: make(chan struct{})}
	m.mu.Lock()
	m.imageWaiters = append(m.imageWaiters, w)
	m.grantImagesLocked()
	m.mu.Unlock()

	select {
	case <-w.ready:
		return func() {
			m.mu.Lock()
			m.activeImageAudits--
			m.grantImagesLocked()
			m.mu.Unlock()
		}, nil
	case <-ctx.Done():
		m.mu.Lock()
		if w.granted {
			m.activeImageAudits--
			m.grantImagesLocked()
		} else {
			for i, candidate := range m.imageWaiters {
				if candidate == w {
					m.imageWaiters = append(m.imageWaiters[:i], m.imageWaiters[i+1:]...)
					break
				}
			}
		}
		m.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (m *ResourceProtectionManager) grantImagesLocked() {
	limit := int64(m.config.ImageAuditMaxConcurrency)
	for m.activeImageAudits < limit && len(m.imageWaiters) > 0 {
		w := m.imageWaiters[0]
		m.imageWaiters = m.imageWaiters[1:]
		w.granted = true
		m.activeImageAudits++
		close(w.ready)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func detectRuntimeSafeMaximumMiB() int {
	limits := make([]int64, 0, 2)
	for _, path := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		value := strings.TrimSpace(string(raw))
		if value == "" || value == "max" {
			continue
		}
		bytes, err := strconv.ParseInt(value, 10, 64)
		if err == nil && bytes > 0 && bytes < 1<<60 {
			limits = append(limits, bytes/2)
			break
		}
	}
	if bytes, ok := parseGoMemoryLimit(os.Getenv("GOMEMLIMIT")); ok {
		limits = append(limits, bytes*2/3)
	}
	if len(limits) == 0 {
		return defaultRuntimeSafeMaximumMiB
	}
	limit := limits[0]
	for _, candidate := range limits[1:] {
		if candidate < limit {
			limit = candidate
		}
	}
	return int(limit >> 20)
}

func parseGoMemoryLimit(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "off") {
		return 0, false
	}
	units := []struct {
		suffix string
		factor int64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"GB", 1_000_000_000}, {"MB", 1_000_000}, {"KB", 1_000}}
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(value, unit.suffix)), 10, 64)
			return n * unit.factor, err == nil && n > 0
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	return n, err == nil && n > 0
}
