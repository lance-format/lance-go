package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lance-format/lance-go/namespace"
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

func TestParseNamespaceTableID(t *testing.T) {
	got, err := parseNamespaceTableID("acceptance$table-1$v2")
	if err != nil {
		t.Fatalf("parse table ID: %v", err)
	}
	if want := []string{"acceptance", "table-1", "v2"}; !slices.Equal(got, want) {
		t.Fatalf("table ID = %v, want %v", got, want)
	}
	for _, raw := range []string{
		"",
		"$table",
		"table$",
		"acceptance$$table",
		"acceptance$bad/segment",
		"acceptance$bad%2Fsegment",
		"acceptance$.",
		"acceptance$..",
		strings.Repeat("a", maxTableIDBytes+1),
	} {
		if _, err := parseNamespaceTableID(raw); err == nil {
			t.Fatalf("expected an error for %q", raw)
		}
	}
}

func TestParseConfigSelectsMode(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	base := []string{
		"--uri", "s3://example-bucket/acceptance/cloud-refresh",
		"--region", "us-east-1",
		"--credential-executable", executable,
		"--deadline", "2m",
	}

	cfg, _, err := parseConfig(base)
	if err != nil {
		t.Fatalf("parse direct config: %v", err)
	}
	if cfg.mode != modeDirectAccessor || len(cfg.namespaceTableID) != 0 {
		t.Fatalf("direct mode = %q, table ID = %v", cfg.mode, cfg.namespaceTableID)
	}

	namespaceArgs := append(append([]string{}, base...), "--mode", modeNamespace, "--namespace-table-id", "acceptance$table-1")
	cfg, _, err = parseConfig(namespaceArgs)
	if err != nil {
		t.Fatalf("parse namespace config: %v", err)
	}
	if cfg.mode != modeNamespace {
		t.Fatalf("mode = %q", cfg.mode)
	}
	if want := []string{"acceptance", "table-1"}; !slices.Equal(cfg.namespaceTableID, want) {
		t.Fatalf("table ID = %v, want %v", cfg.namespaceTableID, want)
	}

	for name, args := range map[string][]string{
		"table id without namespace mode": append(append([]string{}, base...), "--namespace-table-id", "acceptance$table"),
		"namespace mode without table id": append(append([]string{}, base...), "--mode", modeNamespace),
		"unknown mode":                    append(append([]string{}, base...), "--mode", "managed"),
		"invalid table id":                append(append([]string{}, base...), "--mode", modeNamespace, "--namespace-table-id", "acceptance$$table"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseConfig(args); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestNamespaceRESTPropertiesUseMatchingDelimiter(t *testing.T) {
	properties := namespaceRESTProperties("http://127.0.0.1:12345")
	if properties["uri"] != "http://127.0.0.1:12345" {
		t.Fatalf("uri = %q", properties["uri"])
	}
	if properties["delimiter"] != tableIDSeparator {
		t.Fatalf("delimiter = %q, want %q", properties["delimiter"], tableIDSeparator)
	}
}

func TestTableDescriptorInitialAndRefreshState(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	seed := credential{
		AccessKeyID:     testAccessKey,
		SecretAccessKey: testSecret,
		SessionToken:    testToken,
		Expiration:      now.Add(20 * time.Minute),
	}
	next := credential{
		AccessKeyID:     "ASIAEDGKDAXUNDKTIUGJ",
		SecretAccessKey: "next-secret",
		SessionToken:    "next-token",
		Expiration:      now.Add(40 * time.Minute),
	}
	fetcher := &fakeCredentialFetcher{credentials: []credential{next}}
	red := newRedactor()
	red.add(seed.SecretAccessKey)
	red.add(seed.SessionToken)
	red.add(next.SecretAccessKey)
	red.add(next.SessionToken)
	client := newTableDescriptorClient(
		[]string{"acceptance", "table-1"},
		"s3://example-bucket/acceptance/cloud-refresh/run",
		"us-east-1",
		seed,
		2*time.Minute,
		fetcher,
		red,
	)
	tableID := []string{"acceptance", "table-1"}
	initial := false
	response, err := client.DescribeTable(&namespace.DescribeTableRequest{ID: &tableID, VendCredentials: &initial})
	if err != nil {
		t.Fatalf("initial describe: %v", err)
	}
	if fetcher.calls != 0 {
		t.Fatalf("initial describe fetched credentials %d time(s)", fetcher.calls)
	}
	if response.Location == nil || *response.Location != "s3://example-bucket/acceptance/cloud-refresh/run" {
		t.Fatalf("location = %v", response.Location)
	}
	if response.ManagedVersioning == nil || *response.ManagedVersioning {
		t.Fatalf("managed versioning = %v, want false", response.ManagedVersioning)
	}
	options := *response.StorageOptions
	if options["aws_access_key_id"] != seed.AccessKeyID || options["aws_secret_access_key"] != seed.SecretAccessKey || options["aws_session_token"] != seed.SessionToken {
		t.Fatalf("initial storage options do not contain seed A")
	}
	if options["expires_at_millis"] != strconv.FormatInt(seed.Expiration.UnixMilli(), 10) {
		t.Fatalf("initial expiration = %q", options["expires_at_millis"])
	}
	if options["refresh_offset_millis"] != strconv.FormatInt((2*time.Minute).Milliseconds(), 10) {
		t.Fatalf("initial refresh offset = %q", options["refresh_offset_millis"])
	}

	// A caller mutating its response must not change the descriptor's state.
	options["aws_access_key_id"] = "tampered"
	second, err := client.DescribeTable(&namespace.DescribeTableRequest{ID: &tableID, VendCredentials: &initial})
	if err != nil {
		t.Fatalf("second initial describe: %v", err)
	}
	if (*second.StorageOptions)["aws_access_key_id"] != seed.AccessKeyID {
		t.Fatalf("descriptor options were aliased to a response map")
	}

	vend := true
	refreshed, err := client.DescribeTable(&namespace.DescribeTableRequest{ID: &tableID, VendCredentials: &vend})
	if err != nil {
		t.Fatalf("vend describe: %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("vend describe fetched credentials %d time(s), want 1", fetcher.calls)
	}
	refreshedOptions := *refreshed.StorageOptions
	if refreshedOptions["aws_access_key_id"] != next.AccessKeyID || refreshedOptions["aws_secret_access_key"] != next.SecretAccessKey || refreshedOptions["aws_session_token"] != next.SessionToken {
		t.Fatalf("vend storage options do not contain B")
	}
	if refreshedOptions["expires_at_millis"] != strconv.FormatInt(next.Expiration.UnixMilli(), 10) {
		t.Fatalf("vend expiration = %q", refreshedOptions["expires_at_millis"])
	}
	if refreshedOptions["refresh_offset_millis"] != strconv.FormatInt((2*time.Minute).Milliseconds(), 10) {
		t.Fatalf("vend refresh offset = %q", refreshedOptions["refresh_offset_millis"])
	}
	if refreshed.ManagedVersioning == nil || *refreshed.ManagedVersioning {
		t.Fatalf("vend managed versioning = %v, want false", refreshed.ManagedVersioning)
	}

	afterVend, err := client.DescribeTable(&namespace.DescribeTableRequest{ID: &tableID, VendCredentials: &initial})
	if err != nil {
		t.Fatalf("post-vend initial describe: %v", err)
	}
	if (*afterVend.StorageOptions)["aws_access_key_id"] != next.AccessKeyID {
		t.Fatalf("post-vend initial describe did not serve the remembered B")
	}
	if fetcher.calls != 1 {
		t.Fatalf("post-vend initial describe fetched credentials again")
	}

	stats := client.Snapshot()
	if stats.Total != 4 || stats.Initial != 3 || stats.Vend != 1 || stats.ForeignID != 0 || stats.Truncated {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if len(stats.Records) != 4 {
		t.Fatalf("records = %d, want 4", len(stats.Records))
	}
	if stats.Records[0].VendCredentials || stats.Records[1].VendCredentials || !stats.Records[2].VendCredentials || stats.Records[3].VendCredentials {
		t.Fatalf("unexpected vend flags: %+v", stats.Records)
	}
	if stats.Records[2].Fingerprint != credentialFingerprint(next.AccessKeyID) {
		t.Fatalf("vend fingerprint = %q", stats.Records[2].Fingerprint)
	}
}

func TestTableDescriptorRejectsForeignID(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	seed := credential{AccessKeyID: testAccessKey, SecretAccessKey: testSecret, SessionToken: testToken, Expiration: now.Add(20 * time.Minute)}
	next := credential{AccessKeyID: "ASIAEDGKDAXUNDKTIUGJ", SecretAccessKey: "next-secret", SessionToken: "next-token", Expiration: now.Add(40 * time.Minute)}
	fetcher := &fakeCredentialFetcher{credentials: []credential{next}}
	client := newTableDescriptorClient([]string{"acceptance", "table-1"}, "s3://example-bucket/prefix/run", "us-east-1", seed, time.Minute, fetcher, newRedactor())

	foreign := []string{"acceptance", "other-table"}
	vend := true
	if _, err := client.DescribeTable(&namespace.DescribeTableRequest{ID: &foreign, VendCredentials: &vend}); err == nil {
		t.Fatal("expected an error for a foreign table ID")
	} else {
		var customErr *namespace.CustomClientError
		if !errors.As(err, &customErr) || customErr.Code != namespace.CustomClientErrorNotFound {
			t.Fatalf("foreign table error = %v", err)
		}
	}
	if _, err := client.DescribeTable(&namespace.DescribeTableRequest{VendCredentials: &vend}); err == nil {
		t.Fatal("expected an error for a nil table ID")
	} else {
		var customErr *namespace.CustomClientError
		if !errors.As(err, &customErr) || customErr.Code != namespace.CustomClientErrorInvalidInput {
			t.Fatalf("nil table error = %v", err)
		}
	}
	if fetcher.calls != 0 {
		t.Fatalf("foreign table requests fetched credentials %d time(s)", fetcher.calls)
	}
	stats := client.Snapshot()
	if stats.ForeignID != 1 || stats.Total != 0 || stats.Initial != 0 || stats.Vend != 0 {
		t.Fatalf("unexpected stats after foreign requests: %+v", stats)
	}
}

func TestTableDescriptorRedactsFetchError(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	seed := credential{AccessKeyID: testAccessKey, SecretAccessKey: testSecret, SessionToken: testToken, Expiration: now.Add(20 * time.Minute)}
	fetcher := &fakeCredentialFetcher{err: fmt.Errorf("helper failed with %s and %s", testSecret, testToken)}
	red := newRedactor()
	red.add(testAccessKey)
	red.add(testSecret)
	red.add(testToken)
	client := newTableDescriptorClient([]string{"acceptance", "table-1"}, "s3://example-bucket/prefix/run", "us-east-1", seed, time.Minute, fetcher, red)

	tableID := []string{"acceptance", "table-1"}
	vend := true
	_, err := client.DescribeTable(&namespace.DescribeTableRequest{ID: &tableID, VendCredentials: &vend})
	if err == nil {
		t.Fatal("expected a fetch error")
	}
	var customErr *namespace.CustomClientError
	if !errors.As(err, &customErr) || customErr.Code != namespace.CustomClientErrorIO {
		t.Fatalf("fetch error = %v", err)
	}
	message := err.Error()
	for _, secret := range []string{testAccessKey, testSecret, testToken} {
		if strings.Contains(message, secret) {
			t.Fatalf("error contains a credential")
		}
	}
	if !strings.Contains(message, "[REDACTED]") {
		t.Fatalf("error has no redaction marker: %q", message)
	}

	// A failed vend leaves the service state on A.
	initial := false
	response, describeErr := client.DescribeTable(&namespace.DescribeTableRequest{ID: &tableID, VendCredentials: &initial})
	if describeErr != nil {
		t.Fatalf("initial describe after failed vend: %v", describeErr)
	}
	if (*response.StorageOptions)["aws_access_key_id"] != seed.AccessKeyID {
		t.Fatalf("descriptor served a credential after a failed vend")
	}
}

func TestTableDescriptorManagedVersionMethodsAreUnsupported(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	seed := credential{AccessKeyID: testAccessKey, SecretAccessKey: testSecret, SessionToken: testToken, Expiration: now.Add(20 * time.Minute)}
	client := newTableDescriptorClient([]string{"acceptance", "table-1"}, "s3://example-bucket/prefix/run", "us-east-1", seed, time.Minute, &fakeCredentialFetcher{}, newRedactor())

	for name, call := range map[string]func() error{
		"ListTableVersions": func() error {
			_, err := client.ListTableVersions(&namespace.ListTableVersionsRequest{})
			return err
		},
		"CreateTableVersion": func() error {
			_, err := client.CreateTableVersion(&namespace.CreateTableVersionRequest{})
			return err
		},
		"DescribeTableVersion": func() error {
			_, err := client.DescribeTableVersion(&namespace.DescribeTableVersionRequest{})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			var customErr *namespace.CustomClientError
			if !errors.As(err, &customErr) || customErr.Code != namespace.CustomClientErrorUnsupported {
				t.Fatalf("%s error = %v", name, err)
			}
		})
	}
}

type fakeCredentialFetcher struct {
	credentials []credential
	err         error
	calls       int
}

func (f *fakeCredentialFetcher) Fetch(context.Context) (credential, error) {
	f.calls++
	if f.err != nil {
		return credential{}, f.err
	}
	if len(f.credentials) == 0 {
		return credential{}, fmt.Errorf("no fixture credential")
	}
	next := f.credentials[0]
	if len(f.credentials) > 1 {
		f.credentials = f.credentials[1:]
	}
	return next, nil
}
