package kiwoom

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func mockSyncSecret(account string) []byte {
	raw, _ := json.Marshal(MockCredentials{
		SchemaVersion: "kiwoom-mock-read.v1", AppKey: "synthetic-app-key", SecretKey: "synthetic-secret-key",
		AliasKeyBase64: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")), AccountNumber: account,
	})
	return raw
}
func TestKiwoomMockSyncCredentialBoundary(t *testing.T) {
	valid := string(mockSyncSecret("9876543210"))
	for name, raw := range map[string]string{
		"empty": "", "array": "[]", "unknown": strings.Replace(valid, `"app_key":`, `"mode":"production","app_key":`, 1),
		"duplicate":         strings.Replace(valid, `"app_key":`, `"app_key":"other","app_key":`, 1),
		"escaped duplicate": strings.Replace(valid, `"app_key":`, `"app_\u006bey":"other","app_key":`, 1),
		"missing":           strings.Replace(valid, `"schema_version":"kiwoom-mock-read.v1",`, "", 1),
		"wrong schema":      strings.Replace(valid, "kiwoom-mock-read.v1", "kiwoom-live.v1", 1),
		"null":              strings.Replace(valid, `"synthetic-app-key"`, "null", 1),
		"blank key":         strings.Replace(valid, "synthetic-app-key", "   ", 1),
		"control":           strings.Replace(valid, "synthetic-app-key", `app\nkey`, 1),
		"long key":          strings.Replace(valid, "synthetic-app-key", strings.Repeat("k", 1025), 1),
		"bad account":       strings.Replace(valid, "9876543210", "123456789", 1),
		"large":             strings.Repeat(" ", kiwoomMockSecretLimit+1), "trailing": valid + "{}",
		"bad base64":   strings.Replace(valid, "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWZ=", 1),
		"short alias":  strings.Replace(valid, "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", base64.StdEncoding.EncodeToString([]byte("short")), 1),
		"invalid UTF8": valid + string([]byte{0xff}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := DecodeMockCredentials([]byte(raw)); err == nil || err.Error() != "kiwoom mock credential record is invalid" {
				t.Fatalf("invalid credential boundary accepted or leaked input: %v", err)
			}
		})
	}
	parsed, key, err := DecodeMockCredentials([]byte(valid + "\n"))
	if err != nil || parsed.AccountNumber != "9876543210" || len(key) != 32 {
		t.Fatalf("valid bounded credential record rejected: %v", err)
	}
}
