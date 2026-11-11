package ffi

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestBranchJSONCopiesValuesWithoutPrecisionLoss(t *testing.T) {
	const wire = `{
		"parentBranch": "dev",
		"identifier": {"version_mapping": [[1, "uuid-1"], [18446744073709551615, "uuid-max"]]},
		"parentVersion": 9007199254740993,
		"createAt": 1710000000,
		"manifestSize": 9007199254740993,
		"metadata": {"team": "data"}
	}`
	var raw branchJSON
	if err := json.Unmarshal([]byte(wire), &raw); err != nil {
		t.Fatalf("unmarshal branch JSON: %v", err)
	}
	info, err := raw.info("child")
	if err != nil {
		t.Fatalf("branchJSON.info: %v", err)
	}

	if info.Name != "child" || info.ParentBranch != "dev" {
		t.Fatalf("branch identity = %+v", info)
	}
	if info.ParentVersion != 9007199254740993 {
		t.Fatalf("parent version = %d, want exact 2^53+1", info.ParentVersion)
	}
	if info.ManifestSize != 9007199254740993 {
		t.Fatalf("manifest size = %d, want exact 2^53+1", info.ManifestSize)
	}
	if len(info.Identifier.VersionMapping) != 2 {
		t.Fatalf("version mappings = %+v", info.Identifier.VersionMapping)
	}
	if got := info.Identifier.VersionMapping[1].Version; got != math.MaxUint64 {
		t.Fatalf("max version mapping = %d, want %d", got, uint64(math.MaxUint64))
	}
	if got := info.Identifier.VersionMapping[1].UUID; got != "uuid-max" {
		t.Fatalf("max version uuid = %q, want uuid-max", got)
	}
	if info.CreateAt.Unix() != 1710000000 {
		t.Fatalf("createAt = %v, want 1710000000", info.CreateAt)
	}
	if info.Metadata["team"] != "data" {
		t.Fatalf("metadata = %v, want team=data", info.Metadata)
	}

	raw.Metadata["team"] = "changed"
	if info.Metadata["team"] != "data" {
		t.Fatal("mutating decoded JSON changed the copied metadata")
	}
}

func TestBranchJSONRootAndEmptyMetadata(t *testing.T) {
	var raw branchJSON
	if err := json.Unmarshal([]byte(`{"parentBranch":null,"identifier":{"version_mapping":[]},"parentVersion":1,"createAt":1,"manifestSize":2,"metadata":{}}`), &raw); err != nil {
		t.Fatalf("unmarshal root branch JSON: %v", err)
	}
	info, err := raw.info("root")
	if err != nil {
		t.Fatalf("branchJSON.info: %v", err)
	}
	if info.ParentBranch != "" {
		t.Fatalf("root parent branch = %q, want empty", info.ParentBranch)
	}
	if info.Metadata == nil || len(info.Metadata) != 0 {
		t.Fatalf("root metadata = %v, want empty non-nil map", info.Metadata)
	}
	if info.Identifier.VersionMapping == nil || len(info.Identifier.VersionMapping) != 0 {
		t.Fatalf("root identifier = %+v, want empty non-nil mapping", info.Identifier)
	}
}

func TestBranchJSONRejectsMalformedIdentifier(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{"wrong arity", `{"version_mapping":[[1,"uuid","extra"]]}`, "want 2"},
		{"float version", `{"version_mapping":[[1.5,"uuid"]]}`, "version"},
		{"non-string uuid", `{"version_mapping":[[1,2]]}`, "uuid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var identifier BranchIdentifier
			err := json.Unmarshal([]byte(test.json), &identifier)
			if err == nil {
				t.Fatalf("Unmarshal accepted %s", test.json)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Unmarshal error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestBranchJSONRejectsCreateAtOverflow(t *testing.T) {
	raw := branchJSON{CreateAt: math.MaxInt64 + 1}
	if _, err := raw.info("overflow"); err == nil {
		t.Fatal("branchJSON.info accepted a createAt that overflows int64")
	}
}
