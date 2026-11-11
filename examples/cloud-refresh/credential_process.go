package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxCredentialOutput = 64 << 10
	minSeedLifetime     = 20 * time.Second
)

var (
	errCredentialProcess = errors.New("credential process failed")
	errCredentialOutput  = errors.New("credential process output is invalid")
	errCredentialTimeout = errors.New("credential process timed out")
	errRunDeadline       = errors.New("run deadline exceeded")
)

type credential struct {
	Version         int       `json:"Version"`
	AccessKeyID     string    `json:"AccessKeyId"`
	SecretAccessKey string    `json:"SecretAccessKey"`
	SessionToken    string    `json:"SessionToken"`
	Expiration      time.Time `json:"-"`
}

type credentialSource struct {
	executable    string
	args          []string
	fetchTimeout  time.Duration
	totalDeadline time.Time
	redactor      *redactor

	execMu sync.Mutex
}

func newCredentialSource(executable string, args []string, fetchTimeout time.Duration, totalDeadline time.Time, redactor *redactor) *credentialSource {
	return &credentialSource{
		executable:    executable,
		args:          append([]string(nil), args...),
		fetchTimeout:  fetchTimeout,
		totalDeadline: totalDeadline,
		redactor:      redactor,
	}
}

func (s *credentialSource) Fetch(ctx context.Context) (credential, error) {
	// Serialize invocations so a call-count driven operator process returns
	// credentials in a deterministic order when native calls concurrently.
	s.execMu.Lock()
	defer s.execMu.Unlock()

	now := time.Now()
	if !s.totalDeadline.IsZero() && !now.Before(s.totalDeadline) {
		return credential{}, errRunDeadline
	}
	timeout := s.fetchTimeout
	if !s.totalDeadline.IsZero() {
		remaining := time.Until(s.totalDeadline)
		if remaining <= 0 {
			return credential{}, errRunDeadline
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		return credential{}, errRunDeadline
	}

	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(fetchCtx, s.executable, s.args...)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stderr = io.Discard
	output := &limitedBuffer{limit: maxCredentialOutput}
	cmd.Stdout = output

	err := cmd.Run()
	finishedAt := time.Now()
	if output.overflow {
		return credential{}, fmt.Errorf("%w: output exceeds %d bytes", errCredentialOutput, maxCredentialOutput)
	}
	if err != nil {
		if fetchCtx.Err() != nil {
			if errors.Is(fetchCtx.Err(), context.DeadlineExceeded) {
				if !s.totalDeadline.IsZero() && !finishedAt.Before(s.totalDeadline) {
					return credential{}, errRunDeadline
				}
				return credential{}, errCredentialTimeout
			}
			return credential{}, fmt.Errorf("%w: invocation was canceled", errCredentialProcess)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return credential{}, fmt.Errorf("%w: executable exited with status %d", errCredentialProcess, exitErr.ExitCode())
		}
		return credential{}, fmt.Errorf("%w: executable could not be started", errCredentialProcess)
	}

	cred, err := parseCredentialProcessJSON(output.Bytes(), finishedAt)
	if err != nil {
		return credential{}, err
	}
	s.redactor.add(cred.AccessKeyID, cred.SecretAccessKey, cred.SessionToken)
	return cred, nil
}

func parseCredentialProcessJSON(data []byte, now time.Time) (credential, error) {
	var raw struct {
		Version         int    `json:"Version"`
		AccessKeyID     string `json:"AccessKeyId"`
		SecretAccessKey string `json:"SecretAccessKey"`
		SessionToken    string `json:"SessionToken"`
		Expiration      string `json:"Expiration"`
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return credential{}, fmt.Errorf("%w: empty output", errCredentialOutput)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return credential{}, fmt.Errorf("%w: not a JSON credential_process v1 document", errCredentialOutput)
	}
	if raw.Version != 1 {
		return credential{}, fmt.Errorf("%w: Version must be 1", errCredentialOutput)
	}
	if raw.AccessKeyID == "" || strings.TrimSpace(raw.AccessKeyID) != raw.AccessKeyID {
		return credential{}, fmt.Errorf("%w: AccessKeyId must be non-empty and have no surrounding whitespace", errCredentialOutput)
	}
	if raw.SecretAccessKey == "" || strings.TrimSpace(raw.SecretAccessKey) != raw.SecretAccessKey {
		return credential{}, fmt.Errorf("%w: SecretAccessKey must be non-empty and have no surrounding whitespace", errCredentialOutput)
	}
	if raw.SessionToken == "" || strings.TrimSpace(raw.SessionToken) != raw.SessionToken {
		return credential{}, fmt.Errorf("%w: SessionToken is required for temporary credentials", errCredentialOutput)
	}
	expiration, err := time.Parse(time.RFC3339, strings.TrimSpace(raw.Expiration))
	if err != nil {
		return credential{}, fmt.Errorf("%w: Expiration must be an RFC3339 timestamp", errCredentialOutput)
	}
	if !expiration.After(now) {
		return credential{}, fmt.Errorf("%w: Expiration must be in the future", errCredentialOutput)
	}
	return credential{
		Version:         raw.Version,
		AccessKeyID:     raw.AccessKeyID,
		SecretAccessKey: raw.SecretAccessKey,
		SessionToken:    raw.SessionToken,
		Expiration:      expiration,
	}, nil
}

func credentialFingerprint(accessKeyID string) string {
	sum := sha256.Sum256([]byte(accessKeyID))
	return hex.EncodeToString(sum[:])
}

type redactor struct {
	mu     sync.RWMutex
	values map[string]struct{}
}

func newRedactor() *redactor {
	return &redactor{values: make(map[string]struct{})}
}

func (r *redactor) add(values ...string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, value := range values {
		if value != "" {
			r.values[value] = struct{}{}
		}
	}
}

func (r *redactor) redact(text string) string {
	if r == nil {
		return text
	}
	r.mu.RLock()
	values := make([]string, 0, len(r.values))
	for value := range r.values {
		values = append(values, value)
	}
	r.mu.RUnlock()
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, value := range values {
		text = strings.ReplaceAll(text, value, "[REDACTED]")
	}
	return text
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		b.overflow = true
		return 0, fmt.Errorf("output exceeds %d bytes", b.limit)
	}
	return b.Buffer.Write(p)
}
