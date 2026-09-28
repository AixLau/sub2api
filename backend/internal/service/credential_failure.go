package service

import (
	"net/http"
	"strconv"
	"time"
)

type CredentialFailureObservation struct {
	Origin, Scope, Code                        string
	PrincipalID, InstanceID, CredentialVersion int64
	Generation                                 string
	Status                                     int
	RetryAt                                    time.Time
	MayHaveExecuted                            bool
	Confidence                                 string
}

// Structured provider scope is supplied only by a verified adapter. Free text
// never broadens a credential error into a principal revocation.
func ClassifyCredentialFailure(snapshot CredentialExecutionSnapshot, status int, headers http.Header, terminal bool, now time.Time) CredentialFailureObservation {
	o := CredentialFailureObservation{Origin: "UPSTREAM", Scope: "UNKNOWN", PrincipalID: snapshot.Lease.PrincipalID, InstanceID: snapshot.Lease.InstanceID, Generation: snapshot.Lease.Generation, CredentialVersion: snapshot.CredentialVersion, Status: status, MayHaveExecuted: !terminal, Confidence: "OBSERVED"}
	switch status {
	case 0:
		o.Origin = "TRANSPORT"
		o.Code = "UPSTREAM_RESULT_UNKNOWN"
	case 400:
		o.Scope = "REQUEST"
		o.Code = "UPSTREAM_REQUEST_REJECTED"
	case 401:
		o.Scope = "INSTANCE"
		o.Code = "CREDENTIAL_REJECTED"
	case 429:
		o.Code = "UPSTREAM_RATE_LIMITED"
		o.RetryAt = now.Add(30 * time.Second)
	default:
		if status >= 500 {
			o.Scope = "PROVIDER"
			o.Code = "UPSTREAM_UNAVAILABLE"
		}
	}
	if status == 429 {
		value := headers.Get("Retry-After")
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
			// A huge provider delay is not truncated to a shorter local backoff.
			if seconds > int64((time.Duration(1<<63-1))/time.Second) {
				o.RetryAt = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
			} else {
				o.RetryAt = now.Add(time.Duration(seconds) * time.Second)
			}
		} else if date, err := http.ParseTime(value); err == nil && date.After(now) {
			o.RetryAt = date
		}
	}
	return o
}
