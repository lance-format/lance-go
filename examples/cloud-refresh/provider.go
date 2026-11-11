package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const maxReportedNativeFetches = 64

type nativeFetch struct {
	At              time.Time `json:"at"`
	Fingerprint     string    `json:"fingerprint"`
	ExpiresAt       time.Time `json:"expires_at"`
	HasSessionToken bool      `json:"has_session_token"`
	DurationMillis  int64     `json:"duration_millis"`
}

type nativeFetchStats struct {
	Total        int
	Records      []nativeFetch
	Fingerprints map[string]int
}

type rotatingProvider struct {
	id     string
	region string
	source *credentialSource

	mu    sync.Mutex
	total int
	fetch []nativeFetch
	seen  map[string]int
}

func newRotatingProvider(id, region string, source *credentialSource) *rotatingProvider {
	return &rotatingProvider{
		id:     id,
		region: region,
		source: source,
		seen:   make(map[string]int),
	}
}

func (p *rotatingProvider) StorageOptionsProviderID() string {
	return p.id
}

func (p *rotatingProvider) FetchStorageOptions(ctx context.Context) (map[string]string, error) {
	start := time.Now()
	cred, err := p.source.Fetch(ctx)
	if err != nil {
		// The source never returns command output, but redact anyway because the
		// error may cross native code and be surfaced by a later caller.
		return nil, errors.New(p.source.redactor.redact(err.Error()))
	}
	finishedAt := time.Now()
	fetch := nativeFetch{
		At:              finishedAt,
		Fingerprint:     credentialFingerprint(cred.AccessKeyID),
		ExpiresAt:       cred.Expiration,
		HasSessionToken: cred.SessionToken != "",
		DurationMillis:  finishedAt.Sub(start).Milliseconds(),
	}

	p.mu.Lock()
	p.total++
	p.seen[fetch.Fingerprint]++
	if len(p.fetch) < maxReportedNativeFetches {
		p.fetch = append(p.fetch, fetch)
	}
	p.mu.Unlock()

	// Native replaces the accessor's initial options with this map. Return the
	// region too, because the provider result is not merged with the initial map.
	return map[string]string{
		"region":                p.region,
		"aws_access_key_id":     cred.AccessKeyID,
		"aws_secret_access_key": cred.SecretAccessKey,
		"aws_session_token":     cred.SessionToken,
		"expires_at_millis":     fmt.Sprintf("%d", cred.Expiration.UnixMilli()),
	}, nil
}

func (p *rotatingProvider) Stats() nativeFetchStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	stats := nativeFetchStats{
		Total:        p.total,
		Records:      append([]nativeFetch(nil), p.fetch...),
		Fingerprints: make(map[string]int, len(p.seen)),
	}
	for fingerprint, count := range p.seen {
		stats.Fingerprints[fingerprint] = count
	}
	return stats
}

func stableProviderID(executable string, args []string, region string) string {
	identity := executable + "\x00" + strings.Join(args, "\x00") + "\x00" + region
	sum := sha256.Sum256([]byte(identity))
	return "cloud-refresh-acceptance:" + hex.EncodeToString(sum[:16])
}
