package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/lance-format/lance-go/namespace"
)

// maxNamespaceDescribeRecords bounds the evidence retained in the report. A
// healthy run performs one initial describe and at least one vend describe.
const maxNamespaceDescribeRecords = 64

// credentialFetcher is the credential_process contract used by the descriptor
// for a vend request. The runner seeds A separately and passes it to the
// descriptor, so the first call through this interface is the helper's second
// invocation and must return B.
type credentialFetcher interface {
	Fetch(context.Context) (credential, error)
}

// tableDescriptorClient is a tiny, operator-owned namespace service for one
// unmanaged S3 table. It implements only DescribeTable. The three
// managed-version methods return typed unsupported errors instead of pretending
// to coordinate versions.
//
// The descriptor is service state, not a client-side credential cache: it
// serves the last credential it returned until a vend request asks it to
// obtain the next stable credential from the operator process. Initial
// describes before the first vend return the seed A supplied by the runner.
type tableDescriptorClient struct {
	tableID       []string
	location      string
	region        string
	refreshOffset time.Duration
	fetcher       credentialFetcher
	redactor      *redactor

	mu              sync.Mutex
	options         map[string]string
	fingerprint     string
	expiresAt       time.Time
	hasSessionToken bool
	initialCalls    int
	vendCalls       int
	foreignIDCalls  int
	records         []namespaceDescribeRecord
	recordsDropped  bool
}

type namespaceDescribeStats struct {
	Total     int
	Initial   int
	Vend      int
	ForeignID int
	Records   []namespaceDescribeRecord
	Truncated bool
}

func newTableDescriptorClient(
	tableID []string,
	location string,
	region string,
	seed credential,
	refreshOffset time.Duration,
	fetcher credentialFetcher,
	redactor *redactor,
) *tableDescriptorClient {
	if redactor == nil {
		redactor = newRedactor()
	}
	return &tableDescriptorClient{
		tableID:         append([]string(nil), tableID...),
		location:        location,
		region:          region,
		refreshOffset:   refreshOffset,
		fetcher:         fetcher,
		redactor:        redactor,
		options:         credentialStorageOptions(region, seed, refreshOffset),
		fingerprint:     credentialFingerprint(seed.AccessKeyID),
		expiresAt:       seed.Expiration,
		hasSessionToken: seed.SessionToken != "",
	}
}

func (c *tableDescriptorClient) DescribeTable(request *namespace.DescribeTableRequest) (*namespace.DescribeTableResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	requestedAt := time.Now()
	if request == nil {
		return nil, &namespace.CustomClientError{
			Code:    namespace.CustomClientErrorInvalidInput,
			Message: "describe table request is nil",
		}
	}
	if request.ID == nil {
		return nil, &namespace.CustomClientError{
			Code:    namespace.CustomClientErrorInvalidInput,
			Message: "describe table request has no table identifier",
		}
	}
	if !slices.Equal(*request.ID, c.tableID) {
		c.foreignIDCalls++
		return nil, &namespace.CustomClientError{
			Code:    namespace.CustomClientErrorNotFound,
			Message: "table identifier does not match the operator-owned descriptor",
		}
	}

	vend := request.VendCredentials != nil && *request.VendCredentials
	if vend {
		next, err := c.fetcher.Fetch(context.Background())
		if err != nil {
			message := "credential process failed"
			if c.redactor != nil {
				message = c.redactor.redact(err.Error())
			}
			return nil, &namespace.CustomClientError{
				Code:    namespace.CustomClientErrorIO,
				Message: message,
			}
		}
		c.replaceCredential(next)
	}
	servedAt := time.Now()

	options := cloneStorageOptions(c.options)
	record := namespaceDescribeRecord{
		RequestedAt:     requestedAt.UTC(),
		ServedAt:        servedAt.UTC(),
		DurationMillis:  servedAt.Sub(requestedAt).Milliseconds(),
		VendCredentials: vend,
		TableID:         append([]string(nil), (*request.ID)...),
		Fingerprint:     c.fingerprint,
		ExpiresAt:       c.expiresAt.UTC(),
		HasSessionToken: c.hasSessionToken,
	}
	if vend {
		c.vendCalls++
	} else {
		c.initialCalls++
	}
	c.appendRecord(record)

	managedVersioning := false
	return &namespace.DescribeTableResponse{
		Location:          stringPointer(c.location),
		StorageOptions:    &options,
		ManagedVersioning: &managedVersioning,
	}, nil
}

func (c *tableDescriptorClient) ListTableVersions(*namespace.ListTableVersionsRequest) (*namespace.ListTableVersionsResponse, error) {
	return nil, unsupportedTableDescriptorOperation("ListTableVersions")
}

func (c *tableDescriptorClient) CreateTableVersion(*namespace.CreateTableVersionRequest) (*namespace.CreateTableVersionResponse, error) {
	return nil, unsupportedTableDescriptorOperation("CreateTableVersion")
}

func (c *tableDescriptorClient) DescribeTableVersion(*namespace.DescribeTableVersionRequest) (*namespace.DescribeTableVersionResponse, error) {
	return nil, unsupportedTableDescriptorOperation("DescribeTableVersion")
}

// replaceCredential installs the next service-side credential. The caller
// holds c.mu.
func (c *tableDescriptorClient) replaceCredential(next credential) {
	clearStorageOptions(c.options)
	c.options = credentialStorageOptions(c.region, next, c.refreshOffset)
	c.fingerprint = credentialFingerprint(next.AccessKeyID)
	c.expiresAt = next.Expiration
	c.hasSessionToken = next.SessionToken != ""
}

// appendRecord stores bounded evidence. The caller holds c.mu.
func (c *tableDescriptorClient) appendRecord(record namespaceDescribeRecord) {
	if len(c.records) >= maxNamespaceDescribeRecords {
		c.recordsDropped = true
		return
	}
	c.records = append(c.records, record)
}

func (c *tableDescriptorClient) Snapshot() namespaceDescribeStats {
	c.mu.Lock()
	defer c.mu.Unlock()

	records := make([]namespaceDescribeRecord, len(c.records))
	for i, record := range c.records {
		record.TableID = append([]string(nil), record.TableID...)
		records[i] = record
	}
	return namespaceDescribeStats{
		Total:     c.initialCalls + c.vendCalls,
		Initial:   c.initialCalls,
		Vend:      c.vendCalls,
		ForeignID: c.foreignIDCalls,
		Records:   records,
		Truncated: c.recordsDropped,
	}
}

func credentialStorageOptions(region string, cred credential, refreshOffset time.Duration) map[string]string {
	return map[string]string{
		"region":                region,
		"aws_access_key_id":     cred.AccessKeyID,
		"aws_secret_access_key": cred.SecretAccessKey,
		"aws_session_token":     cred.SessionToken,
		"expires_at_millis":     strconv.FormatInt(cred.Expiration.UnixMilli(), 10),
		"refresh_offset_millis": strconv.FormatInt(refreshOffset.Milliseconds(), 10),
	}
}

func cloneStorageOptions(options map[string]string) map[string]string {
	if options == nil {
		return nil
	}
	cloned := make(map[string]string, len(options))
	for key, value := range options {
		cloned[key] = value
	}
	return cloned
}

func unsupportedTableDescriptorOperation(operation string) error {
	return &namespace.CustomClientError{
		Code:    namespace.CustomClientErrorUnsupported,
		Message: fmt.Sprintf("%s is not implemented by the unmanaged table descriptor", operation),
	}
}

func stringPointer(value string) *string {
	return &value
}
