package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	testAccessKey = "ASIAEXAMPLEACCESSKEY"
	testSecret    = "secret-access-key-value"
	testToken     = "session-token-value"
)

func TestParseCredentialProcessJSON(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	valid := credentialJSON(testAccessKey, testSecret, testToken, now.Add(15*time.Minute))

	got, err := parseCredentialProcessJSON([]byte(valid), now)
	if err != nil {
		t.Fatalf("parse valid credential: %v", err)
	}
	if got.AccessKeyID != testAccessKey || got.SecretAccessKey != testSecret || got.SessionToken != testToken {
		t.Fatalf("parsed credential fields do not match")
	}
	if !got.Expiration.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("expiration = %s", got.Expiration)
	}

	for name, body := range map[string]string{
		"malformed":      `{`,
		"wrong version":  strings.Replace(valid, `"Version":1`, `"Version":2`, 1),
		"missing token":  credentialJSON(testAccessKey, testSecret, "", now.Add(15*time.Minute)),
		"expired":        credentialJSON(testAccessKey, testSecret, testToken, now.Add(-time.Minute)),
		"leading space":  strings.Replace(valid, testAccessKey, " "+testAccessKey, 1),
		"missing secret": credentialJSON(testAccessKey, "", testToken, now.Add(15*time.Minute)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCredentialProcessJSON([]byte(body), now); err == nil {
				t.Fatalf("expected an error")
			}
		})
	}
}

func TestRedactorRemovesKnownSecrets(t *testing.T) {
	red := newRedactor()
	red.add(testAccessKey)
	red.add(testSecret)
	red.add(testToken)

	message := red.redact("request failed for " + testAccessKey + " with " + testSecret + " and " + testToken)
	for _, secret := range []string{testAccessKey, testSecret, testToken} {
		if strings.Contains(message, secret) {
			t.Fatalf("redacted message contains a credential")
		}
	}
	if !strings.Contains(message, "[REDACTED]") {
		t.Fatalf("redacted message does not show a marker: %q", message)
	}
}

func TestComputeScheduleUsesRealExpiration(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, _, _, err := computeSchedule(now.Add(time.Hour), now, 3*time.Second); err == nil || !strings.Contains(err.Error(), "hold is shorter") {
		t.Fatalf("short hold error = %v", err)
	}

	hold, offset, due, err := computeSchedule(now.Add(10*time.Minute), now, 30*time.Second)
	if err != nil {
		t.Fatalf("compute schedule: %v", err)
	}
	if hold != 30*time.Second || offset != 9*time.Minute+30*time.Second || !due.Equal(now.Add(30*time.Second)) {
		t.Fatalf("unexpected schedule: hold=%s offset=%s due=%s", hold, offset, due)
	}

	hold, offset, due, err = computeSchedule(now.Add(21*time.Second), now, 30*time.Second)
	if err != nil {
		t.Fatalf("compute short schedule: %v", err)
	}
	if hold != 7*time.Second || offset != 14*time.Second || !due.Equal(now.Add(7*time.Second)) {
		t.Fatalf("unexpected short schedule: hold=%s offset=%s due=%s", hold, offset, due)
	}
	if _, _, _, err := computeSchedule(now.Add(19*time.Second), now, 30*time.Second); err == nil {
		t.Fatal("expected an error for a credential below the minimum lifetime")
	}
	if _, _, _, err := computeSchedule(now.Add(time.Minute), now, 0); err == nil {
		t.Fatal("expected an error for a zero hold")
	}
}

func TestParseTargetPrefix(t *testing.T) {
	prefix, err := parseTargetPrefix("s3://example-bucket/acceptance/cloud-refresh")
	if err != nil {
		t.Fatalf("parse prefix: %v", err)
	}
	if prefix.bucket != "example-bucket" || prefix.path != "acceptance/cloud-refresh" {
		t.Fatalf("unexpected prefix: %+v", prefix)
	}
	if got := prefix.withSuffix("abc123"); got != "s3://example-bucket/acceptance/cloud-refresh/abc123" {
		t.Fatalf("run URI = %q", got)
	}

	for _, raw := range []string{
		"s3://example-bucket",
		"s3://example-bucket/",
		"s3://example-bucket//prefix",
		"s3://example-bucket/prefix/",
		"s3://example-bucket/prefix?query=1",
		"s3://example-bucket/prefix?",
		"s3://example-bucket/prefix#fragment",
		"https://example-bucket/prefix",
		"s3://example-bucket/a%2Fb",
	} {
		if _, err := parseTargetPrefix(raw); err == nil {
			t.Fatalf("expected an error for %q", raw)
		}
	}
}

func TestStableProviderID(t *testing.T) {
	first := stableProviderID("/usr/local/bin/credential-process", []string{"--profile", "acceptance"}, "us-east-1")
	second := stableProviderID("/usr/local/bin/credential-process", []string{"--profile", "acceptance"}, "us-east-1")
	other := stableProviderID("/usr/local/bin/credential-process", []string{"--profile", "acceptance"}, "us-west-2")
	if first == "" || first != second {
		t.Fatalf("provider id is not stable")
	}
	if first == other {
		t.Fatalf("provider id does not include the region")
	}
}

func credentialJSON(accessKey, secret, token string, expiration time.Time) string {
	return fmt.Sprintf(
		`{"Version":1,"AccessKeyId":%q,"SecretAccessKey":%q,"SessionToken":%q,"Expiration":%q}`,
		accessKey,
		secret,
		token,
		expiration.UTC().Format(time.RFC3339),
	)
}
