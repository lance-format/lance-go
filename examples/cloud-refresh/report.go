package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

type report struct {
	SchemaVersion       int              `json:"schema_version"`
	Kind                string           `json:"kind"`
	Mode                string           `json:"mode"`
	Status              string           `json:"status"`
	Stage               string           `json:"stage,omitempty"`
	Error               *reportError     `json:"error,omitempty"`
	RunURI              string           `json:"run_uri"`
	Region              string           `json:"region"`
	StartedAt           time.Time        `json:"started_at"`
	FinishedAt          *time.Time       `json:"finished_at,omitempty"`
	DeadlineMillis      int64            `json:"deadline_millis"`
	HoldMillis          int64            `json:"hold_millis"`
	RefreshOffsetMillis int64            `json:"refresh_offset_millis"`
	NativeRefreshDueAt  *time.Time       `json:"native_refresh_due_at,omitempty"`
	Seed                *seedReport      `json:"seed,omitempty"`
	NativeFetchesBefore []nativeFetch    `json:"native_fetches_before_mutation"`
	Rotation            *rotationReport  `json:"rotation,omitempty"`
	Mutation            *mutationReport  `json:"mutation,omitempty"`
	OldSnapshot         *datasetCheck    `json:"old_snapshot,omitempty"`
	OldSnapshotReopened *datasetCheck    `json:"old_snapshot_reopened,omitempty"`
	Committed           *datasetCheck    `json:"committed,omitempty"`
	Reopen              *reopenReport    `json:"reopen,omitempty"`
	Namespace           *namespaceReport `json:"namespace,omitempty"`
	Resources           resourceReport   `json:"resources"`
	Limits              limitsReport     `json:"limits"`
	Notes               []string         `json:"notes"`
}

type reportError struct {
	Category string `json:"category"`
	Message  string `json:"message"`
}

type seedReport struct {
	Source          string    `json:"source"`
	FetchedAt       time.Time `json:"fetched_at"`
	Fingerprint     string    `json:"fingerprint"`
	ExpiresAt       time.Time `json:"expires_at"`
	HasSessionToken bool      `json:"has_session_token"`
}

type rotationReport struct {
	Fingerprint        string         `json:"fingerprint"`
	ExpiresAt          time.Time      `json:"expires_at"`
	ObservedAt         time.Time      `json:"observed_at"`
	HasSessionToken    bool           `json:"has_session_token"`
	DistinctFromSeed   bool           `json:"distinct_from_seed"`
	AStillValidAtFetch bool           `json:"a_still_valid_at_fetch"`
	NativeFetchTotal   int            `json:"native_fetch_total"`
	NativeFingerprints map[string]int `json:"native_fingerprints"`
}

type mutationReport struct {
	Operation               string         `json:"operation"`
	Predicate               string         `json:"predicate"`
	DeletedRows             uint64         `json:"deleted_rows"`
	PreVersion              uint64         `json:"pre_version"`
	PostVersion             uint64         `json:"post_version"`
	NativeFetches           []nativeFetch  `json:"native_fetches"`
	NativeFetchTotal        int            `json:"native_fetch_total"`
	NativeFingerprints      map[string]int `json:"native_fingerprints"`
	AllNativeFetchesRotated bool           `json:"all_native_fetches_rotated"`
}

type datasetCheck struct {
	Version  uint64  `json:"version"`
	RowCount uint64  `json:"row_count"`
	Rows     []int64 `json:"rows"`
}

type reopenReport struct {
	FreshAccessor           bool           `json:"fresh_accessor"`
	FreshProvider           bool           `json:"fresh_provider"`
	NativeFetches           []nativeFetch  `json:"native_fetches"`
	NativeFetchTotal        int            `json:"native_fetch_total"`
	NativeFingerprints      map[string]int `json:"native_fingerprints"`
	AllNativeFetchesRotated bool           `json:"all_native_fetches_rotated"`
	Dataset                 *datasetCheck  `json:"dataset,omitempty"`
}

type resourceReport struct {
	PublicAccessorClosedBeforeRefresh bool                     `json:"public_accessor_closed_before_refresh"`
	CreatedDatasetRetainedAcrossRun   bool                     `json:"created_dataset_retained_across_refresh"`
	MutatedDatasetClosed              bool                     `json:"mutated_dataset_closed"`
	CreatedDatasetClosed              bool                     `json:"created_dataset_closed"`
	ReopenedDatasetClosed             bool                     `json:"reopened_dataset_closed"`
	ReopenedOldDatasetClosed          bool                     `json:"reopened_old_dataset_closed"`
	Namespace                         *namespaceResourceReport `json:"namespace,omitempty"`
	Retained                          string                   `json:"retained"`
	Recreated                         string                   `json:"recreated"`
}

type namespaceResourceReport struct {
	FixtureDatasetClosed                   bool `json:"fixture_dataset_closed"`
	PublicRESTNamespaceClosedBeforeRefresh bool `json:"public_rest_namespace_closed_before_refresh"`
	CustomNamespaceClosedBeforeRefresh     bool `json:"custom_namespace_closed_before_refresh"`
	AdapterAliveDuringMutation             bool `json:"adapter_alive_during_mutation"`
	MutatedDatasetClosed                   bool `json:"mutated_dataset_closed"`
	InitialDatasetClosed                   bool `json:"initial_dataset_closed"`
	ReopenedOldDatasetClosed               bool `json:"reopened_old_dataset_closed"`
	ReopenedDatasetClosed                  bool `json:"reopened_dataset_closed"`
	ReopenRESTNamespaceClosed              bool `json:"reopen_rest_namespace_closed"`
	AdapterClosed                          bool `json:"adapter_closed"`
}

type namespaceReport struct {
	Scope                             string                    `json:"scope"`
	BackendKind                       string                    `json:"backend_kind"`
	Transport                         string                    `json:"transport"`
	CustomNamespaceID                 string                    `json:"custom_namespace_id"`
	RESTAdapterHost                   string                    `json:"rest_adapter_host"`
	RESTAdapterEndpoint               string                    `json:"rest_adapter_endpoint"`
	TableID                           []string                  `json:"table_id"`
	Location                          string                    `json:"location"`
	ManagedVersioning                 bool                      `json:"managed_versioning"`
	InitialDescribes                  int                       `json:"initial_describes"`
	VendDescribes                     int                       `json:"vend_describes"`
	ForeignIDRejections               int                       `json:"foreign_id_rejections"`
	MutationVendCalls                 int                       `json:"mutation_vend_calls"`
	DescribeRecords                   []namespaceDescribeRecord `json:"describe_records"`
	DescribeRecordsTruncated          bool                      `json:"describe_records_truncated"`
	InitialDescribe                   *namespaceDescribeRecord  `json:"initial_describe,omitempty"`
	MutationDescribe                  *namespaceDescribeRecord  `json:"mutation_describe,omitempty"`
	ReopenDescribe                    *namespaceDescribeRecord  `json:"reopen_describe,omitempty"`
	ReopenCommitted                   *datasetCheck             `json:"reopen_committed,omitempty"`
	InitialCredentialMatchesSeed      bool                      `json:"initial_credential_matches_seed"`
	MutationCredentialDiffersFromSeed bool                      `json:"mutation_credential_differs_from_seed"`
	ReopenInitialMatchesMutation      bool                      `json:"reopen_initial_matches_mutation"`
	VendRequestedDuringMutation       bool                      `json:"vend_requested_during_mutation"`
}

type namespaceDescribeRecord struct {
	RequestedAt     time.Time `json:"requested_at"`
	ServedAt        time.Time `json:"served_at"`
	DurationMillis  int64     `json:"duration_millis"`
	VendCredentials bool      `json:"vend_credentials"`
	TableID         []string  `json:"table_id"`
	Fingerprint     string    `json:"credential_fingerprint"`
	ExpiresAt       time.Time `json:"credential_expires_at"`
	HasSessionToken bool      `json:"has_session_token"`
}

type limitsReport struct {
	ExpiredARejectionProven                  bool `json:"expired_a_rejection_proven"`
	CloudNativeNamespaceVendingProven        bool `json:"cloud_native_namespace_vending_proven"`
	SelfHostedNamespaceVendingProven         bool `json:"self_hosted_namespace_vending_proven"`
	NamespaceManagedVersionPublicationProven bool `json:"namespace_managed_version_publication_proven"`
	ProductionAccountSupported               bool `json:"production_account_supported"`
}

func newReport(runURI, region string, startedAt time.Time, deadline time.Duration) *report {
	return &report{
		SchemaVersion:       1,
		Kind:                "lance-go-s3-credential-rotation-acceptance",
		Status:              "running",
		RunURI:              runURI,
		Region:              region,
		StartedAt:           startedAt.UTC(),
		DeadlineMillis:      deadline.Milliseconds(),
		NativeFetchesBefore: []nativeFetch{},
		Notes: []string{
			"A is fetched directly by this runner. Only B is proven to be fetched by native.",
			"The runner requests native refresh with a real refresh_offset_millis derived from A's Expiration; this run does not prove expired-credential rejection.",
			"The deadline bounds credential subprocesses and runner waits; a native S3 call already in progress is synchronous and is not interruptible by this runner.",
			"Cloud-native namespace vending and production account support are outside this runner.",
		},
		Resources: resourceReport{
			Retained:  "The created Dataset, its native handle, and its retained accessor/store stayed open after the public accessor was closed; Delete ran through that owner.",
			Recreated: "A fresh public accessor, provider, and Dataset were created only for the final reopen verification.",
		},
		Limits: limitsReport{},
	}
}

func (r *report) fail(red *redactor, stage, category string, err error) {
	r.Status = "failed"
	r.Stage = stage
	message := "unknown error"
	if err != nil {
		message = err.Error()
	}
	r.Error = &reportError{Category: category, Message: red.redact(message)}
	finished := time.Now().UTC()
	r.FinishedAt = &finished
}

func (r *report) pass() {
	r.Status = "passed"
	r.Stage = "complete"
	finished := time.Now().UTC()
	r.FinishedAt = &finished
}

func writeReport(path string, rep *report) error {
	var writer io.Writer = os.Stdout
	var file *os.File
	if path != "-" {
		var err error
		file, err = os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("open report: %w", err)
		}
		defer file.Close()
		writer = file
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(rep); err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	return nil
}
