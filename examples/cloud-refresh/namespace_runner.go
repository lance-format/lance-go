package main

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/namespace"
)

const (
	namespaceDescriptorID = "operator-owned-cloud-refresh-table"
	namespaceAdapterHost  = "127.0.0.1"
	namespaceModeScope    = "operator-owned, self-hosted table descriptor served over a loopback REST adapter; data access is REAL AWS S3; this is not a commercial or cloud-native namespace service, and managed_versioning=false keeps publication in native file manifests rather than namespace-managed table versions"
)

func namespaceModeNotes() []string {
	return []string{
		"Namespace mode uses an operator-owned, self-hosted table descriptor served over a loopback REST adapter. It is not a commercial or cloud-native namespace service.",
		"Data access and mutation use REAL AWS S3 at the exact run URI. The descriptor returns ManagedVersioning=false, so native publishes normal file manifests rather than namespace-managed table versions.",
		"The initial DescribeTable has no vend_credentials and returns seed A. Native's storage-options provider later sends vend_credentials=true for the exact table ID over the real loopback REST transport and receives stable B from the operator process.",
		"The public REST namespace and the custom namespace owner are closed before the refresh window. The retained Dataset keeps the REST client and native provider; the REST adapter keeps the descriptor backend alive.",
		"The deadline bounds credential subprocesses and runner waits; the synchronous native DescribeTable/S3 call in progress is not interruptible by this runner.",
		"Unit tests cover configuration, descriptor state, exact table-ID rejection, and redaction only; they do not execute native or AWS.",
	}
}

func executeNamespace(cfg config, prefix targetPrefix) *report {
	startedAt := time.Now()
	deadlineAt := startedAt.Add(cfg.deadline)
	ctx, cancel := context.WithDeadline(context.Background(), deadlineAt)
	defer cancel()

	suffix, err := randomRunSuffix()
	if err != nil {
		rep := newReport("", cfg.region, startedAt, cfg.deadline)
		rep.Mode = modeNamespace
		rep.fail(newRedactor(), "setup", "random_suffix", err)
		return rep
	}
	runURI := prefix.withSuffix(suffix)
	rep := newReport(runURI, cfg.region, startedAt, cfg.deadline)
	rep.Mode = modeNamespace
	rep.Notes = namespaceModeNotes()
	rep.Resources.Retained = "The namespace-opened Dataset retained the native REST namespace client and storage-options provider after the public REST namespace and custom namespace owner were closed; Delete ran through that retained Dataset."
	rep.Resources.Recreated = "A fresh REST namespace client opened the same table for the final reopen verification; the adapter and descriptor backend stayed alive."
	rep.Namespace = &namespaceReport{
		Scope:             namespaceModeScope,
		BackendKind:       "operator_owned_table_descriptor",
		Transport:         "loopback_http_rest",
		CustomNamespaceID: namespaceDescriptorID,
		RESTAdapterHost:   namespaceAdapterHost,
		TableID:           append([]string(nil), cfg.namespaceTableID...),
		Location:          runURI,
		ManagedVersioning: false,
	}
	nsReport := rep.Namespace
	nsResources := &namespaceResourceReport{}
	rep.Resources.Namespace = nsResources

	red := newRedactor()
	source := newCredentialSource(cfg.credentialExecutable, cfg.credentialArgs, cfg.fetchTimeout, deadlineAt, red)

	var (
		client      *tableDescriptorClient
		customNS    namespace.Namespace
		adapter     *namespace.RESTAdapter
		publicNS    namespace.Namespace
		reopenNS    namespace.Namespace
		fixture     *lance.Dataset
		initial     *lance.Dataset
		mutated     *lance.Dataset
		reopened    *lance.Dataset
		reopenedOld *lance.Dataset
	)

	defer func() {
		if client != nil {
			populateNamespaceDescribeReport(nsReport, client.Snapshot())
		}
		for _, resource := range []struct {
			dataset **lance.Dataset
			closed  *bool
			name    string
		}{
			{&reopenedOld, &nsResources.ReopenedOldDatasetClosed, "reopened_old"},
			{&reopened, &nsResources.ReopenedDatasetClosed, "reopened"},
			{&mutated, &nsResources.MutatedDatasetClosed, "mutated"},
			{&initial, &nsResources.InitialDatasetClosed, "initial"},
			{&fixture, &nsResources.FixtureDatasetClosed, "fixture"},
		} {
			if *resource.dataset == nil {
				continue
			}
			err := (*resource.dataset).Close()
			*resource.closed = err == nil
			*resource.dataset = nil
			if err != nil {
				recordNamespaceCleanupError(rep, red, "close "+resource.name, err)
			}
		}
		for _, resource := range []struct {
			rest *namespace.Namespace
			name string
		}{
			{&reopenNS, "reopen_rest_namespace"},
			{&publicNS, "public_rest_namespace"},
		} {
			if *resource.rest == nil {
				continue
			}
			err := (*resource.rest).Close()
			*resource.rest = nil
			if err != nil {
				recordNamespaceCleanupError(rep, red, "close "+resource.name, err)
			}
		}
		if adapter != nil {
			err := adapter.Close()
			nsResources.AdapterClosed = err == nil
			adapter = nil
			if err != nil {
				recordNamespaceCleanupError(rep, red, "close rest adapter", err)
			}
		}
		if customNS != nil {
			err := customNS.Close()
			customNS = nil
			if err != nil {
				recordNamespaceCleanupError(rep, red, "close custom namespace", err)
			}
		}
		finished := time.Now().UTC()
		rep.FinishedAt = &finished
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
	seedExpiration := seed.Expiration
	seedFingerprint := credentialFingerprint(seed.AccessKeyID)
	rep.Seed = &seedReport{
		Source:          "operator_process_seed",
		FetchedAt:       seedFetchedAt.UTC(),
		Fingerprint:     seedFingerprint,
		ExpiresAt:       seedExpiration.UTC(),
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

	// The fixture is created directly with seed A and no provider. The
	// namespace path starts after the table exists; it must never receive a
	// direct StorageOptionsAccessor.
	fixtureOptions := map[string]string{
		"region":                cfg.region,
		"aws_access_key_id":     seed.AccessKeyID,
		"aws_secret_access_key": seed.SecretAccessKey,
		"aws_session_token":     seed.SessionToken,
	}
	reader, err := newFixtureReader()
	if err != nil {
		return fail("create", "fixture", err)
	}
	fixture, err = lance.Create(
		ctx,
		runURI,
		reader,
		lance.WithWriteStorageOptions(fixtureOptions),
		lance.WithSkipAutoCleanup(true),
	)
	reader.Release()
	clearStorageOptions(fixtureOptions)
	fixtureOptions = nil
	if err != nil {
		return fail("create", "create_fixture", err)
	}
	if err := fixture.Close(); err != nil {
		fixture = nil
		return fail("create", "close_fixture", err)
	}
	fixture = nil
	nsResources.FixtureDatasetClosed = true

	// The descriptor's initial state is the same A. A vend request is the
	// helper's next invocation and must return stable B.
	client = newTableDescriptorClient(cfg.namespaceTableID, runURI, cfg.region, seed, refreshOffset, source, red)
	seed.AccessKeyID = ""
	seed.SecretAccessKey = ""
	seed.SessionToken = ""

	customNS, err = namespace.NewCustom(client, namespaceDescriptorID)
	if err != nil {
		return fail("namespace", "new_custom_namespace", err)
	}
	adapter, err = customNS.StartRESTAdapter(namespaceAdapterHost, 0)
	if err != nil {
		return fail("namespace", "start_rest_adapter", err)
	}
	port, err := adapter.Port()
	if err != nil {
		return fail("namespace", "rest_adapter_port", err)
	}
	endpoint := fmt.Sprintf("http://%s:%d", namespaceAdapterHost, port)
	nsReport.RESTAdapterEndpoint = endpoint

	publicNS, err = namespace.NewRest(namespaceRESTProperties(endpoint))
	if err != nil {
		return fail("namespace", "new_rest_namespace", err)
	}
	initial, err = lance.OpenFromNamespace(ctx, publicNS, cfg.namespaceTableID)
	if err != nil {
		return fail("namespace", "open_from_namespace", err)
	}

	initialStats := client.Snapshot()
	nsReport.InitialDescribes = initialStats.Initial
	nsReport.VendDescribes = initialStats.Vend
	nsReport.ForeignIDRejections = initialStats.ForeignID
	nsReport.DescribeRecords = initialStats.Records
	if initialStats.ForeignID != 0 {
		return fail("namespace", "foreign_table_id", fmt.Errorf("the descriptor rejected %d request(s) for a foreign table ID", initialStats.ForeignID))
	}
	if initialStats.Initial != 1 || initialStats.Vend != 0 || len(initialStats.Records) != 1 {
		return fail("namespace", "initial_describe", fmt.Errorf("initial DescribeTable calls = %d, vend calls = %d, records = %d; want one non-vend describe and no vend", initialStats.Initial, initialStats.Vend, len(initialStats.Records)))
	}
	initialRecord := initialStats.Records[0]
	if err := requireNamespaceDescribe(initialRecord, cfg.namespaceTableID, false, seedFingerprint, seedExpiration); err != nil {
		return fail("namespace", "initial_describe_evidence", err)
	}
	nsReport.InitialDescribe = &initialRecord
	nsReport.InitialCredentialMatchesSeed = initialRecord.Fingerprint == seedFingerprint

	// The Dataset retains the native REST client and provider. The adapter
	// retains the descriptor backend. Both public owners can close now.
	publicCloseErr := publicNS.Close()
	publicNS = nil
	if publicCloseErr != nil {
		return fail("retention", "close_public_rest_namespace", publicCloseErr)
	}
	nsResources.PublicRESTNamespaceClosedBeforeRefresh = true
	customCloseErr := customNS.Close()
	customNS = nil
	if customCloseErr != nil {
		return fail("retention", "close_custom_namespace", customCloseErr)
	}
	nsResources.CustomNamespaceClosedBeforeRefresh = true

	baseline, err := checkDataset(ctx, initial, fixtureRows, 1)
	if err != nil {
		return fail("baseline", "baseline_dataset", err)
	}
	rep.OldSnapshot = baseline
	if stats := client.Snapshot(); stats.Vend != 0 {
		return fail("baseline", "unexpected_native_fetch", fmt.Errorf("native vended credentials before the refresh window; initial storage options did not remain in use"))
	}

	if err := waitUntil(ctx, refreshDue); err != nil {
		return fail("wait", "deadline", err)
	}

	before := client.Snapshot()
	if before.Vend != 0 {
		return fail("mutation", "unexpected_native_fetch", fmt.Errorf("native vended credentials before the mutation"))
	}
	if before.Truncated || before.ForeignID != 0 {
		return fail("mutation", "describe_evidence", fmt.Errorf("namespace describe evidence is incomplete or includes a foreign table ID"))
	}

	mutationStart := time.Now().UTC()
	next, deleted, err := initial.Delete(ctx, "id = 20")
	mutated = next
	mutationEnd := time.Now().UTC()
	if err != nil {
		return fail("mutation", "delete", err)
	}

	after := client.Snapshot()
	populateNamespaceDescribeReport(nsReport, after)
	if after.ForeignID != 0 {
		return fail("mutation", "foreign_table_id", fmt.Errorf("the descriptor rejected %d request(s) for a foreign table ID", after.ForeignID))
	}
	if after.Truncated || len(after.Records) < len(before.Records) {
		return fail("mutation", "describe_evidence", fmt.Errorf("namespace describe evidence is incomplete"))
	}
	newRecords := after.Records[len(before.Records):]
	newVend := after.Vend - before.Vend
	if newVend == 0 {
		return fail("mutation", "native_refresh_missing", fmt.Errorf("native did not send a vend_credentials=true DescribeTable during the mutation"))
	}
	if len(newRecords) != newVend {
		return fail("mutation", "native_refresh_evidence", fmt.Errorf("new describe records = %d, new vend calls = %d; expected only vend requests", len(newRecords), newVend))
	}
	mutationCredential := newRecords[0]
	if err := requireNamespaceDescribeDuring(mutationCredential, cfg.namespaceTableID, seedExpiration, mutationStart, mutationEnd); err != nil {
		return fail("mutation", "native_refresh_evidence", err)
	}
	if mutationCredential.Fingerprint == seedFingerprint {
		return fail("mutation", "native_refresh_identity", fmt.Errorf("native vended the seed A fingerprint again; no rotation was observed"))
	}
	for _, record := range after.Records {
		if record.VendCredentials && record.Fingerprint != mutationCredential.Fingerprint {
			return fail("mutation", "rotation_stability", fmt.Errorf("native vended more than one credential fingerprint"))
		}
	}
	nsReport.MutationDescribe = &mutationCredential
	nsReport.MutationVendCalls = newVend
	nsReport.VendRequestedDuringMutation = mutationCredential.VendCredentials
	nsReport.MutationCredentialDiffersFromSeed = mutationCredential.Fingerprint != seedFingerprint

	if deleted != 1 {
		return fail("mutation", "deleted_rows", fmt.Errorf("deleted rows = %d, want 1", deleted))
	}
	if err := ctx.Err(); err != nil {
		return fail("mutation", "deadline", err)
	}
	if _, err := adapter.Port(); err != nil {
		return fail("mutation", "adapter_liveness", err)
	}
	nsResources.AdapterAliveDuringMutation = true

	committed, err := checkDataset(ctx, mutated, fixtureAfterDelete, 2)
	if err != nil {
		return fail("verification", "committed_dataset", err)
	}
	rep.Committed = committed
	old, err := checkDataset(ctx, initial, fixtureRows, 1)
	if err != nil {
		return fail("verification", "old_snapshot", err)
	}
	rep.OldSnapshot = old
	if err := ctx.Err(); err != nil {
		return fail("verification", "deadline", err)
	}

	if err := mutated.Close(); err != nil {
		mutated = nil
		return fail("verification", "close_mutated", err)
	}
	mutated = nil
	nsResources.MutatedDatasetClosed = true
	if err := initial.Close(); err != nil {
		initial = nil
		return fail("verification", "close_initial", err)
	}
	initial = nil
	nsResources.InitialDatasetClosed = true

	if err := ctx.Err(); err != nil {
		return fail("reopen", "deadline", err)
	}
	reopenStart := time.Now().UTC()
	reopenNS, err = namespace.NewRest(namespaceRESTProperties(endpoint))
	if err != nil {
		return fail("reopen", "new_rest_namespace", err)
	}
	reopened, err = lance.OpenFromNamespace(ctx, reopenNS, cfg.namespaceTableID)
	if err != nil {
		return fail("reopen", "open_from_namespace", err)
	}
	reopenStats := client.Snapshot()
	reopenRecord := firstNamespaceDescribeAfter(reopenStats, reopenStart)
	if reopenRecord == nil {
		return fail("reopen", "initial_describe", fmt.Errorf("the fresh REST client did not produce a DescribeTable record"))
	}
	if reopenRecord.VendCredentials {
		return fail("reopen", "initial_describe", fmt.Errorf("the fresh REST client's first DescribeTable used vend_credentials; expected a non-vend initial describe"))
	}
	if err := requireNamespaceDescribe(*reopenRecord, cfg.namespaceTableID, false, mutationCredential.Fingerprint, mutationCredential.ExpiresAt); err != nil {
		return fail("reopen", "initial_describe_evidence", err)
	}
	nsReport.ReopenDescribe = reopenRecord
	nsReport.ReopenInitialMatchesMutation = reopenRecord.Fingerprint == mutationCredential.Fingerprint

	reopenCheck, err := checkDataset(ctx, reopened, fixtureAfterDelete, 2)
	if err != nil {
		return fail("reopen", "reopened_dataset", err)
	}
	nsReport.ReopenCommitted = reopenCheck
	reopenedOld, err = reopened.CheckoutVersion(ctx, 1)
	if err != nil {
		return fail("reopen", "old_version_checkout", err)
	}
	reopenedOldCheck, err := checkDataset(ctx, reopenedOld, fixtureRows, 1)
	if err != nil {
		return fail("reopen", "reopened_old_dataset", err)
	}
	if err := reopenedOld.Close(); err != nil {
		reopenedOld = nil
		return fail("reopen", "close_reopened_old", err)
	}
	reopenedOld = nil
	nsResources.ReopenedOldDatasetClosed = true
	rep.OldSnapshotReopened = reopenedOldCheck
	if err := ctx.Err(); err != nil {
		return fail("reopen", "deadline", err)
	}
	if err := reopened.Close(); err != nil {
		reopened = nil
		return fail("reopen", "close_reopened", err)
	}
	reopened = nil
	nsResources.ReopenedDatasetClosed = true
	if err := reopenNS.Close(); err != nil {
		reopenNS = nil
		return fail("reopen", "close_reopen_rest_namespace", err)
	}
	reopenNS = nil
	nsResources.ReopenRESTNamespaceClosed = true

	if err := adapter.Close(); err != nil {
		adapter = nil
		return fail("cleanup", "close_rest_adapter", err)
	}
	adapter = nil
	nsResources.AdapterClosed = true

	finalStats := client.Snapshot()
	populateNamespaceDescribeReport(nsReport, finalStats)
	if finalStats.Truncated || finalStats.ForeignID != 0 {
		return fail("verification", "describe_evidence", fmt.Errorf("namespace describe evidence is incomplete or includes a foreign table ID"))
	}
	if finalStats.Initial < 2 {
		return fail("verification", "reopen_initial_describe", fmt.Errorf("initial DescribeTable count = %d, want at least the initial open and the fresh reopen", finalStats.Initial))
	}
	for _, record := range finalStats.Records {
		if record.VendCredentials && record.Fingerprint != mutationCredential.Fingerprint {
			return fail("verification", "rotation_stability", fmt.Errorf("native vended more than one credential fingerprint"))
		}
		if !slices.Equal(record.TableID, cfg.namespaceTableID) {
			return fail("verification", "table_id", fmt.Errorf("a DescribeTable response used a table ID other than the configured ID"))
		}
	}

	proven := nsReport.InitialCredentialMatchesSeed &&
		nsReport.MutationCredentialDiffersFromSeed &&
		nsReport.VendRequestedDuringMutation &&
		nsReport.MutationVendCalls > 0 &&
		nsReport.ReopenInitialMatchesMutation &&
		nsResources.PublicRESTNamespaceClosedBeforeRefresh &&
		nsResources.CustomNamespaceClosedBeforeRefresh &&
		nsResources.AdapterAliveDuringMutation
	rep.Limits.SelfHostedNamespaceVendingProven = proven
	if !proven {
		return fail("verification", "namespace_proof", fmt.Errorf("namespace vending evidence is incomplete"))
	}

	rep.pass()
	return rep
}

func requireNamespaceDescribe(record namespaceDescribeRecord, tableID []string, vend bool, fingerprint string, expiresAt time.Time) error {
	if !slices.Equal(record.TableID, tableID) {
		return fmt.Errorf("DescribeTable used table ID %v, want %v", record.TableID, tableID)
	}
	if record.VendCredentials != vend {
		return fmt.Errorf("DescribeTable vend_credentials = %t, want %t", record.VendCredentials, vend)
	}
	if record.Fingerprint != fingerprint {
		return fmt.Errorf("DescribeTable credential fingerprint does not match the expected credential")
	}
	if record.ExpiresAt.UnixMilli() != expiresAt.UnixMilli() {
		return fmt.Errorf("DescribeTable credential expiration does not match the expected credential")
	}
	return nil
}

func requireNamespaceDescribeDuring(record namespaceDescribeRecord, tableID []string, seedExpiration, start, end time.Time) error {
	if !slices.Equal(record.TableID, tableID) {
		return fmt.Errorf("vend DescribeTable used table ID %v, want %v", record.TableID, tableID)
	}
	if !record.VendCredentials {
		return fmt.Errorf("the DescribeTable observed during the mutation did not request vend_credentials")
	}
	if record.RequestedAt.Before(start) || record.RequestedAt.After(end) {
		return fmt.Errorf("the vend DescribeTable was not requested during the mutation")
	}
	if !record.ServedAt.Before(seedExpiration) {
		return fmt.Errorf("the vend DescribeTable was served after A expired; the refresh was not early")
	}
	if !record.HasSessionToken {
		return fmt.Errorf("the vended credential has no session token")
	}
	return nil
}

func namespaceRESTProperties(endpoint string) map[string]string {
	return map[string]string{
		"uri":       endpoint,
		"delimiter": tableIDSeparator,
	}
}

func firstNamespaceDescribeAfter(stats namespaceDescribeStats, after time.Time) *namespaceDescribeRecord {
	for _, record := range stats.Records {
		if !record.RequestedAt.Before(after) {
			record.TableID = append([]string(nil), record.TableID...)
			return &record
		}
	}
	return nil
}

func populateNamespaceDescribeReport(nsReport *namespaceReport, stats namespaceDescribeStats) {
	if nsReport == nil {
		return
	}
	nsReport.InitialDescribes = stats.Initial
	nsReport.VendDescribes = stats.Vend
	nsReport.ForeignIDRejections = stats.ForeignID
	nsReport.DescribeRecords = stats.Records
	nsReport.DescribeRecordsTruncated = stats.Truncated
}

func recordNamespaceCleanupError(rep *report, red *redactor, action string, err error) {
	message := red.redact(fmt.Sprintf("%s: %v", action, err))
	rep.Notes = append(rep.Notes, message)
	if rep.Status != "failed" {
		rep.fail(red, "cleanup", "namespace_cleanup", err)
	}
}
