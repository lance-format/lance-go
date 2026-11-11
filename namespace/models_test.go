package namespace_test

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/lance-format/lance-go/namespace"
)

func TestOptionalFieldsPreserveAbsentAndEmpty(t *testing.T) {
	request := namespace.CreateTableRequest{}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal empty request: %v", err)
	}
	if string(encoded) != "{}" {
		t.Fatalf("empty request = %s, want {}", encoded)
	}

	emptyID := []string{}
	request.ID = &emptyID
	encoded, err = json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal empty id: %v", err)
	}
	if string(encoded) != `{"id":[]}` {
		t.Fatalf("empty id request = %s", encoded)
	}

	response := namespace.ListTablesResponse{Tables: []string{}}
	encoded, err = json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal required empty list: %v", err)
	}
	if string(encoded) != `{"tables":[]}` {
		t.Fatalf("required empty list = %s", encoded)
	}
}

func TestNullableDistinguishesAbsentNullAndValue(t *testing.T) {
	absent := namespace.RefreshMaterializedViewRequest{}
	encoded, err := json.Marshal(absent)
	if err != nil {
		t.Fatalf("marshal absent: %v", err)
	}
	if string(encoded) != "{}" {
		t.Fatalf("absent double option = %s", encoded)
	}

	null := namespace.RefreshMaterializedViewRequest{
		SrcVersion: &namespace.Nullable[int32]{},
	}
	encoded, err = json.Marshal(null)
	if err != nil {
		t.Fatalf("marshal null: %v", err)
	}
	if string(encoded) != `{"src_version":null}` {
		t.Fatalf("null double option = %s", encoded)
	}

	value := int32(7)
	present := namespace.RefreshMaterializedViewRequest{
		SrcVersion: &namespace.Nullable[int32]{Value: &value},
	}
	encoded, err = json.Marshal(present)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	if string(encoded) != `{"src_version":7}` {
		t.Fatalf("present double option = %s", encoded)
	}
}

func TestNullableUnmarshalKeepsExplicitNull(t *testing.T) {
	var request namespace.RefreshMaterializedViewRequest
	if err := json.Unmarshal([]byte(`{"src_version":null}`), &request); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if request.SrcVersion == nil {
		t.Fatal("explicit null was treated as an absent field")
	}
	if request.SrcVersion.Value != nil {
		t.Fatalf("explicit null value = %v, want nil", *request.SrcVersion.Value)
	}

	if err := json.Unmarshal([]byte(`{"src_version":12}`), &request); err != nil {
		t.Fatalf("unmarshal value: %v", err)
	}
	if request.SrcVersion == nil || request.SrcVersion.Value == nil || *request.SrcVersion.Value != 12 {
		t.Fatalf("present value = %#v", request.SrcVersion)
	}
}

func TestNullableEmptyListMarshalsAsEmpty(t *testing.T) {
	empty := []string{}
	entry := namespace.AlterVirtualColumnEntry{
		InputColumns: &namespace.Nullable[[]string]{Value: &empty},
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal empty list: %v", err)
	}
	if string(encoded) != `{"input_columns":[]}` {
		t.Fatalf("empty list = %s", encoded)
	}

	var nilSlice []string
	entry.InputColumns.Value = &nilSlice
	encoded, err = json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal nil list: %v", err)
	}
	if string(encoded) != `{"input_columns":[]}` {
		t.Fatalf("nil list = %s", encoded)
	}
}

func TestLargeIntegersRoundTripExactly(t *testing.T) {
	version := namespace.VersionRange{StartVersion: math.MaxInt64, EndVersion: math.MinInt64}
	encoded, err := json.Marshal(version)
	if err != nil {
		t.Fatalf("marshal version range: %v", err)
	}
	want := `{"start_version":9223372036854775807,"end_version":-9223372036854775808}`
	if string(encoded) != want {
		t.Fatalf("version range = %s, want %s", encoded, want)
	}

	var decoded namespace.VersionRange
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal version range: %v", err)
	}
	if !reflect.DeepEqual(decoded, version) {
		t.Fatalf("round trip = %#v, want %#v", decoded, version)
	}
}

func TestModelTagsMatchNativeNames(t *testing.T) {
	parent := "main"
	contents := namespace.BranchContents{ParentBranch: &parent, ManifestSize: 3}
	encoded, err := json.Marshal(contents)
	if err != nil {
		t.Fatalf("marshal branch contents: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decode branch contents: %v", err)
	}
	if _, ok := fields["parentBranch"]; !ok {
		t.Fatalf("branch contents missing parentBranch: %s", encoded)
	}
	if _, ok := fields["manifestSize"]; !ok {
		t.Fatalf("branch contents missing manifestSize: %s", encoded)
	}
}

func TestMaterializedViewKinds(t *testing.T) {
	request := namespace.CreateMaterializedViewRequest{
		Kind: namespace.CreateMaterializedViewKindChunker,
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal materialized view: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decode materialized view: %v", err)
	}
	if fields["kind"] != "chunker" {
		t.Fatalf("materialized view kind = %v, want chunker", fields["kind"])
	}
}
