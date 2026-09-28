package service

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var credentialMetricsOnce sync.Once
var credentialAdmissionDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "sub2api_credential_admission_seconds", Help: "One TryAdmit call including connection acquisition and lock waits, excluding polling sleep and HTTP.", Buckets: []float64{.001, .002, .005, .01, .02, .05, .1, .5, 1, 5, 10}}, []string{"decision"})
var credentialAdmissionDecision = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_credential_admission_total", Help: "Grouped admission decisions, without request/user/session labels."}, []string{"decision"})

func RegisterCredentialMetrics() {
	credentialMetricsOnce.Do(func() { prometheus.MustRegister(credentialAdmissionDuration, credentialAdmissionDecision) })
}
func ObserveCredentialAdmission(seconds float64, decision AdmissionDecisionCode, err error) {
	label := string(decision)
	if err != nil {
		label = "ERROR"
	}
	credentialAdmissionDuration.WithLabelValues(label).Observe(seconds)
	credentialAdmissionDecision.WithLabelValues(label).Inc()
}
