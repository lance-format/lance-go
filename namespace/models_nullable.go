// Companion to namespace/models.go; keep the wrapper list in sync when a model
// that carries a Nullable field is added or removed.
//
// encoding/json calls UnmarshalJSON only for keys that are present in the JSON
// document, so decoding into a reused destination would keep the previous value
// of a field that the new document omits. Each wrapper decodes into a fresh
// alias, which resets absent fields, and hands explicit null and concrete values
// to Nullable.UnmarshalJSON in a single pass.
//
// A wrapped model therefore replaces the whole destination: a field that the
// document omits resets even when it is not Nullable. Models without a wrapper
// keep the encoding/json behavior of leaving omitted fields untouched.

package namespace

import "encoding/json"

func (v *AddColumnsEntry) UnmarshalJSON(data []byte) error {
	type alias AddColumnsEntry
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*v = AddColumnsEntry(decoded)
	return nil
}

func (v *AddVirtualColumnEntry) UnmarshalJSON(data []byte) error {
	type alias AddVirtualColumnEntry
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*v = AddVirtualColumnEntry(decoded)
	return nil
}

func (v *AlterColumnsEntry) UnmarshalJSON(data []byte) error {
	type alias AlterColumnsEntry
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*v = AlterColumnsEntry(decoded)
	return nil
}

func (v *AlterTableBackfillColumnsRequest) UnmarshalJSON(data []byte) error {
	type alias AlterTableBackfillColumnsRequest
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*v = AlterTableBackfillColumnsRequest(decoded)
	return nil
}

func (v *AlterVirtualColumnEntry) UnmarshalJSON(data []byte) error {
	type alias AlterVirtualColumnEntry
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*v = AlterVirtualColumnEntry(decoded)
	return nil
}

func (v *CreateMaterializedViewRequest) UnmarshalJSON(data []byte) error {
	type alias CreateMaterializedViewRequest
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*v = CreateMaterializedViewRequest(decoded)
	return nil
}

func (v *CreateMaterializedViewResponse) UnmarshalJSON(data []byte) error {
	type alias CreateMaterializedViewResponse
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*v = CreateMaterializedViewResponse(decoded)
	return nil
}

func (v *MaterializedViewUdtfEntry) UnmarshalJSON(data []byte) error {
	type alias MaterializedViewUdtfEntry
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*v = MaterializedViewUdtfEntry(decoded)
	return nil
}

func (v *RefreshMaterializedViewRequest) UnmarshalJSON(data []byte) error {
	type alias RefreshMaterializedViewRequest
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*v = RefreshMaterializedViewRequest(decoded)
	return nil
}

func (v *UpdateFieldMetadataEntry) UnmarshalJSON(data []byte) error {
	type alias UpdateFieldMetadataEntry
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*v = UpdateFieldMetadataEntry(decoded)
	return nil
}
