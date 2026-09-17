package service

import "strings"

const credentialBillingPrefix = "credential-lease:"

func CredentialBillingRequestID(lease string) string { return credentialBillingPrefix + lease }
func CredentialBillingLeaseID(request string) (string, bool) {
	if !strings.HasPrefix(request, credentialBillingPrefix) {
		return "", false
	}
	return strings.TrimPrefix(request, credentialBillingPrefix), true
}
