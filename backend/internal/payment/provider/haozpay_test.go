//go:build unit

package provider

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestHaozPayParseRSAPublicKeyAcceptsRawBase64DER(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	rawBase64 := base64.StdEncoding.EncodeToString(pubDER)
	parsed, err := parseRSAPublicKey(rawBase64)
	if err != nil {
		t.Fatalf("parse raw base64 public key: %v", err)
	}

	if parsed.N.Cmp(key.PublicKey.N) != 0 || parsed.E != key.PublicKey.E {
		t.Fatal("parsed public key does not match original")
	}
}

func TestHaozPayParseRSAPrivateKeyAcceptsRawBase64DER(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}

	rawBase64 := base64.StdEncoding.EncodeToString(privDER)
	parsed, err := parseRSAPrivateKey(rawBase64)
	if err != nil {
		t.Fatalf("parse raw base64 private key: %v", err)
	}

	if parsed.N.Cmp(key.N) != 0 || parsed.E != key.E || parsed.D.Cmp(key.D) != 0 {
		t.Fatal("parsed private key does not match original")
	}
}

func TestHaozPayParseRSAKeysStillAcceptPEM(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))

	if _, err := parseRSAPrivateKey(privPEM); err != nil {
		t.Fatalf("parse PEM private key: %v", err)
	}
	if _, err := parseRSAPublicKey(pubPEM); err != nil {
		t.Fatalf("parse PEM public key: %v", err)
	}
}

func TestHaozPaySignRequestSignsSHA256Digest(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	h := &HaozPay{privateKey: key}
	reqBody := map[string]interface{}{
		"merchantNo": "M123456",
		"timestamp":  int64(1710000000123),
		"bizBody": map[string]interface{}{
			"orderAmount":       "10.00",
			"orderTitle":        "Balance recharge",
			"payType":           0,
			"useHaozPayCashier": true,
			"notifyUrl":         "https://example.com/api/v1/payment/webhook/haozpay",
		},
	}

	sign, err := h.signRequest(reqBody)
	if err != nil {
		t.Fatalf("sign request: %v", err)
	}
	signature, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	digest := sha256.Sum256([]byte(haozPayTestSignString(reqBody)))

	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatalf("verify signature: %v", err)
	}
}

func haozPayTestSignString(reqBody map[string]interface{}) string {
	signParams := make(map[string]string)
	if bizBody, ok := reqBody["bizBody"].(map[string]interface{}); ok {
		for k, v := range bizBody {
			if v != nil {
				signParams[k] = fmt.Sprintf("%v", v)
			}
		}
	}
	if merchantNo, ok := reqBody["merchantNo"].(string); ok {
		signParams["merchantNo"] = merchantNo
	}
	if timestamp, ok := reqBody["timestamp"].(int64); ok {
		signParams["timestamp"] = strconv.FormatInt(timestamp, 10)
	}

	keys := make([]string, 0, len(signParams))
	for k := range signParams {
		if signParams[k] != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var buf strings.Builder
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte('&')
		}
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(signParams[k])
	}
	return buf.String()
}
