package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/lance-format/lance-go"
)

var fixtureRows = []int64{10, 20, 30}
var fixtureAfterDelete = []int64{10, 30}

func execute(cfg config, prefix targetPrefix) *report {
	startedAt := time.Now()
	deadlineAt := startedAt.Add(cfg.deadline)
	ctx, cancel := context.WithDeadline(context.Background(), deadlineAt)
	defer cancel()

	suffix, err := randomRunSuffix()
	if err != nil {
		rep := newReport("", cfg.region, startedAt, cfg.deadline)
		rep.fail(newRedactor(), "setup", "random_suffix", err)
		return rep
	}
	runURI := prefix.withSuffix(suffix)
	rep := newReport(runURI, cfg.region, startedAt, cfg.deadline)
	red := newRedactor()
	source := newCredentialSource(cfg.credentialExecutable, cfg.credentialArgs, cfg.fetchTimeout, deadlineAt, red)
	providerID := stableProviderID(cfg.credentialExecutable, cfg.credentialArgs, cfg.region)
	provider := newRotatingProvider(providerID, cfg.region, source)

	var accessor *lance.StorageOptionsAccessor
	var reopenAccessor *lance.StorageOptionsAccessor
	var created *lance.Dataset
	var mutated *lance.Dataset
	var reopened *lance.Dataset
	var oldReopened *lance.Dataset

	defer func() {
		for _, resource := range []struct {
			dataset *lance.Dataset
			closed  *bool
			name    string
		}{
			{oldReopened, &rep.Resources.ReopenedOldDatasetClosed, "reopened_old"},
			{reopened, &rep.Resources.ReopenedDatasetClosed, "reopened"},
			{mutated, &rep.Resources.MutatedDatasetClosed, "mutated"},
			{created, &rep.Resources.CreatedDatasetClosed, "created"},
		} {
			if resource.dataset == nil {
				continue
			}
			err := resource.dataset.Close()
			*resource.closed = err == nil
			if err != nil {
				rep.Notes = append(rep.Notes, red.redact(fmt.Sprintf("close %s: %v", resource.name, err)))
				if rep.Status == "passed" {
					rep.fail(red, "cleanup", "close_"+resource.name, err)
				}
			}
		}
		if accessor != nil {
			accessor.Close()
		}
		if reopenAccessor != nil {
			reopenAccessor.Close()
		}
	}()

	fail := func(stage, category string, err error) *report {
		rep.fail(red, stage, category, err)
		return rep
	}

	seed, err := source.Fetch(ctx)
	if err != nil {
		return fail("seed", "credential_fetch", err)
	}
	seedFetchedAt := time.Now()
	seedFingerprint := credentialFingerprint(seed.AccessKeyID)
	rep.Seed = &seedReport{
		Source:          "operator_process_seed",
		FetchedAt:       seedFetchedAt.UTC(),
		Fingerprint:     seedFingerprint,
		ExpiresAt:       seed.Expiration.UTC(),
		HasSessionToken: seed.SessionToken != "",
	}

	hold, refreshOffset, refreshDue, err := computeSchedule(seed.Expiration, seedFetchedAt, cfg.hold)
	if err != nil {
		return fail("schedule", "credential_lifetime", err)
	}
	rep.HoldMillis = hold.Milliseconds()
	rep.RefreshOffsetMillis = refreshOffset.Milliseconds()
	refreshDueUTC := refreshDue.UTC()
	rep.NativeRefreshDueAt = &refreshDueUTC
	if !refreshDue.Add(verificationReserve).Before(deadlineAt) {
		return fail("schedule", "deadline", fmt.Errorf("native refresh plus verification reserve does not fit before the run deadline; increase --deadline or shorten --hold"))
	}

	initial := map[string]string{
		"region":                cfg.region,
		"aws_access_key_id":     seed.AccessKeyID,
		"aws_secret_access_key": seed.SecretAccessKey,
		"aws_session_token":     seed.SessionToken,
		"expires_at_millis":     strconv.FormatInt(seed.Expiration.UnixMilli(), 10),
		"refresh_offset_millis": strconv.FormatInt(refreshOffset.Milliseconds(), 10),
	}
	accessor, err = lance.NewStorageOptionsAccessor(initial, provider)
	clearStorageOptions(initial)
	initial = nil
	seed.AccessKeyID = ""
	seed.SecretAccessKey = ""
	seed.SessionToken = ""
	if err != nil {
		return fail("create", "accessor", err)
	}

	reader, err := newFixtureReader()
	if err != nil {
		return fail("create", "fixture", err)
	}
	created, err = lance.Create(
		ctx,
		runURI,
		reader,
		lance.WithWriteStorageOptionsAccessor(accessor),
		lance.WithSkipAutoCleanup(true),
	)
	reader.Release()
	if err != nil {
		return fail("create", "create_dataset", err)
	}

	baseline, err := checkDataset(ctx, created, fixtureRows, 1)
	if err != nil {
		return fail("baseline", "baseline_dataset", err)
	}
	rep.OldSnapshot = baseline

	// The initial accessor options carry A and a future expires_at_millis, so
	// native must not have called the provider while creating or scanning A.
	baselineStats := provider.Stats()
	rep.NativeFetchesBefore = append(rep.NativeFetchesBefore, baselineStats.Records...)
	if baselineStats.Total != 0 {
		return fail("baseline", "unexpected_native_fetch", fmt.Errorf("native fetched credentials before the refresh window; A was not used as the initial credential"))
	}

	accessor.Close()
	accessor = nil
	rep.Resources.PublicAccessorClosedBeforeRefresh = true
	rep.Resources.CreatedDatasetRetainedAcrossRun = true

	if err := waitUntil(ctx, refreshDue); err != nil {
		return fail("wait", "deadline", err)
	}

	if stats := provider.Stats(); stats.Total != 0 {
		return fail("mutation", "unexpected_native_fetch", fmt.Errorf("native fetched credentials before the mutation"))
	}

	next, deleted, err := created.Delete(ctx, "id = 20")
	mutated = next
	if err != nil {
		return fail("mutation", "delete", err)
	}
	if deleted != 1 {
		return fail("mutation", "deleted_rows", fmt.Errorf("deleted rows = %d, want 1", deleted))
	}
	if err := ctx.Err(); err != nil {
		return fail("mutation", "deadline", err)
	}

	rotationStats := provider.Stats()
	rotationFetch, err := requireRotated(rotationStats, seedFingerprint)
	if err != nil {
		return fail("rotation", "native_rotation", err)
	}
	rep.Rotation = &rotationReport{
		Fingerprint:        rotationFetch.Fingerprint,
		ExpiresAt:          rotationFetch.ExpiresAt.UTC(),
		ObservedAt:         rotationFetch.At.UTC(),
		HasSessionToken:    rotationFetch.HasSessionToken,
		DistinctFromSeed:   true,
		AStillValidAtFetch: rotationFetch.At.Before(seed.Expiration),
		NativeFetchTotal:   rotationStats.Total,
		NativeFingerprints: rotationStats.Fingerprints,
	}

	committed, err := checkDataset(ctx, mutated, fixtureAfterDelete, 2)
	if err != nil {
		return fail("verification", "committed_dataset", err)
	}
	rep.Committed = committed
	old, err := checkDataset(ctx, created, fixtureRows, 1)
	if err != nil {
		return fail("verification", "old_snapshot", err)
	}
	rep.OldSnapshot = old
	if err := ctx.Err(); err != nil {
		return fail("verification", "deadline", err)
	}

	finalRotationStats := provider.Stats()
	if err := requireAllRotated(finalRotationStats, rotationFetch.Fingerprint); err != nil {
		return fail("verification", "rotation_stability", err)
	}
	rep.Mutation = &mutationReport{
		Operation:               "delete",
		Predicate:               "id = 20",
		DeletedRows:             deleted,
		PreVersion:              old.Version,
		PostVersion:             committed.Version,
		NativeFetches:           finalRotationStats.Records,
		NativeFetchTotal:        finalRotationStats.Total,
		NativeFingerprints:      finalRotationStats.Fingerprints,
		AllNativeFetchesRotated: true,
	}

	if err := mutated.Close(); err != nil {
		return fail("verification", "close_mutated", err)
	}
	mutated = nil
	rep.Resources.MutatedDatasetClosed = true
	if err := created.Close(); err != nil {
		return fail("verification", "close_created", err)
	}
	created = nil
	rep.Resources.CreatedDatasetClosed = true

	if err := ctx.Err(); err != nil {
		return fail("reopen", "deadline", err)
	}
	reopenProvider := newRotatingProvider(providerID, cfg.region, source)
	reopenInitial := map[string]string{"region": cfg.region}
	reopenAccessor, err = lance.NewStorageOptionsAccessor(reopenInitial, reopenProvider)
	clearStorageOptions(reopenInitial)
	reopenInitial = nil
	if err != nil {
		return fail("reopen", "accessor", err)
	}
	reopened, err = lance.Open(ctx, runURI, lance.WithStorageOptionsAccessor(reopenAccessor))
	if err != nil {
		return fail("reopen", "open_dataset", err)
	}
	reopenCheck, err := checkDataset(ctx, reopened, fixtureAfterDelete, 2)
	if err != nil {
		return fail("reopen", "reopened_dataset", err)
	}
	oldReopened, err = reopened.CheckoutVersion(ctx, 1)
	if err != nil {
		return fail("reopen", "old_version_checkout", err)
	}
	oldReopenedCheck, err := checkDataset(ctx, oldReopened, fixtureRows, 1)
	if err != nil {
		return fail("reopen", "reopened_old_dataset", err)
	}
	if err := oldReopened.Close(); err != nil {
		return fail("reopen", "close_reopened_old", err)
	}
	oldReopened = nil
	rep.Resources.ReopenedOldDatasetClosed = true
	rep.OldSnapshotReopened = oldReopenedCheck
	if err := ctx.Err(); err != nil {
		return fail("reopen", "deadline", err)
	}
	reopenStats := reopenProvider.Stats()
	reopenFetch, err := requireRotated(reopenStats, seedFingerprint)
	if err != nil {
		return fail("reopen", "native_rotation", err)
	}
	if err := requireAllRotated(reopenStats, reopenFetch.Fingerprint); err != nil {
		return fail("reopen", "rotation_stability", err)
	}
	if reopenFetch.Fingerprint != rotationFetch.Fingerprint {
		return fail("reopen", "rotation_identity", fmt.Errorf("reopen native credential fingerprint differs from the mutation credential"))
	}
	rep.Reopen = &reopenReport{
		FreshAccessor:           true,
		FreshProvider:           true,
		NativeFetches:           reopenStats.Records,
		NativeFetchTotal:        reopenStats.Total,
		NativeFingerprints:      reopenStats.Fingerprints,
		AllNativeFetchesRotated: true,
		Dataset:                 reopenCheck,
	}

	if err := reopened.Close(); err != nil {
		return fail("reopen", "close_reopened", err)
	}
	reopened = nil
	rep.Resources.ReopenedDatasetClosed = true
	reopenAccessor.Close()
	reopenAccessor = nil

	rep.pass()
	return rep
}

func requireRotated(stats nativeFetchStats, seedFingerprint string) (nativeFetch, error) {
	if stats.Total == 0 {
		return nativeFetch{}, fmt.Errorf("native did not fetch a credential")
	}
	if len(stats.Fingerprints) != 1 {
		return nativeFetch{}, fmt.Errorf("native fetched multiple credential fingerprints; rotation is not stable")
	}
	var fingerprint string
	for candidate := range stats.Fingerprints {
		fingerprint = candidate
	}
	if fingerprint == seedFingerprint {
		return nativeFetch{}, fmt.Errorf("native fetched the seed A fingerprint again; no rotation was observed")
	}
	if len(stats.Records) == 0 {
		return nativeFetch{}, fmt.Errorf("native fetch records are unavailable")
	}
	for _, record := range stats.Records {
		if record.Fingerprint != fingerprint {
			return nativeFetch{}, fmt.Errorf("native fetch records contain more than one fingerprint")
		}
	}
	return stats.Records[0], nil
}

func requireAllRotated(stats nativeFetchStats, fingerprint string) error {
	if stats.Total == 0 {
		return fmt.Errorf("native did not fetch a credential")
	}
	if len(stats.Fingerprints) != 1 {
		return fmt.Errorf("native fetched multiple credential fingerprints after rotation")
	}
	if _, ok := stats.Fingerprints[fingerprint]; !ok {
		return fmt.Errorf("native no longer fetched the rotation credential")
	}
	return nil
}

func computeSchedule(expiration, fetchedAt time.Time, requestedHold time.Duration) (time.Duration, time.Duration, time.Time, error) {
	remaining := expiration.Sub(fetchedAt)
	if remaining < minSeedLifetime {
		return 0, 0, time.Time{}, fmt.Errorf("credential lifetime remaining %s is shorter than the %s minimum", remaining.Round(time.Second), minSeedLifetime)
	}
	if requestedHold < 5*time.Second {
		return 0, 0, time.Time{}, fmt.Errorf("hold is shorter than the 5s minimum")
	}
	hold := requestedHold
	if hold >= remaining-5*time.Second {
		hold = remaining / 3
	}
	if hold < 5*time.Second {
		return 0, 0, time.Time{}, fmt.Errorf("credential lifetime is too short for a bounded hold")
	}
	offset := remaining - hold
	if offset <= 0 {
		return 0, 0, time.Time{}, fmt.Errorf("refresh offset must be greater than zero")
	}
	return hold, offset, expiration.Add(-offset), nil
}

func randomRunSuffix() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate run suffix: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func waitUntil(ctx context.Context, deadline time.Time) error {
	delay := time.Until(deadline)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func clearStorageOptions(options map[string]string) {
	for key := range options {
		options[key] = ""
	}
}

func newFixtureReader() (array.RecordReader, error) {
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	builder.Field(0).(*array.Int64Builder).AppendValues(fixtureRows, nil)
	batch := builder.NewRecordBatch()
	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{batch})
	batch.Release()
	if err != nil {
		return nil, err
	}
	return reader, nil
}

func checkDataset(ctx context.Context, dataset *lance.Dataset, wantRows []int64, wantVersion uint64) (*datasetCheck, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	version, err := dataset.Version()
	if err != nil {
		return nil, err
	}
	if version != wantVersion {
		return nil, fmt.Errorf("dataset version = %d, want %d", version, wantVersion)
	}
	count, err := dataset.CountRows(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := scanIDs(ctx, dataset)
	if err != nil {
		return nil, err
	}
	if count != uint64(len(wantRows)) {
		return nil, fmt.Errorf("row count = %d, want %d", count, len(wantRows))
	}
	slices.Sort(rows)
	if !slices.Equal(rows, wantRows) {
		return nil, fmt.Errorf("scanned rows = %v, want %v", rows, wantRows)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &datasetCheck{Version: version, RowCount: count, Rows: rows}, nil
}

func scanIDs(ctx context.Context, dataset *lance.Dataset) ([]int64, error) {
	scanner, err := dataset.Scan(lance.WithColumns("id"))
	if err != nil {
		return nil, err
	}
	defer scanner.Close()

	var rows []int64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			return nil, err
		}
		column := batch.Column(0)
		ids, ok := column.(*array.Int64)
		if !ok {
			return nil, fmt.Errorf("column 0 has type %s, want int64", column.DataType())
		}
		for i := 0; i < ids.Len(); i++ {
			rows = append(rows, ids.Value(i))
		}
	}
	return rows, nil
}
