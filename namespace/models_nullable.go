// Companion to namespace/models.go; keep this file in sync when a model with
// an Option<Option<T>> field changes.
//
// These UnmarshalJSON methods preserve the Rust Option<Option<T>>
// distinction: a present JSON null is retained as a non-nil Nullable with a
// nil Value, while an absent field stays nil.

package namespace

import "encoding/json"

func (v *AddColumnsEntry) UnmarshalJSON(data []byte) error {
	type alias AddColumnsEntry
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var present map[string]json.RawMessage
	if err := json.Unmarshal(data, &present); err != nil {
		return err
	}
	if raw, ok := present["expression"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Expression = &nullable
	}
	if raw, ok := present["computed"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Computed = &nullable
	}
	if raw, ok := present["virtual_column"]; ok {
		var nullable Nullable[AddVirtualColumnEntry]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.VirtualColumn = &nullable
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
	var present map[string]json.RawMessage
	if err := json.Unmarshal(data, &present); err != nil {
		return err
	}
	if raw, ok := present["udf_backend"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.UDFBackend = &nullable
	}
	if raw, ok := present["auto_backfill"]; ok {
		var nullable Nullable[bool]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.AutoBackfill = &nullable
	}
	if raw, ok := present["manifest"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Manifest = &nullable
	}
	if raw, ok := present["manifest_checksum"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.ManifestChecksum = &nullable
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
	var present map[string]json.RawMessage
	if err := json.Unmarshal(data, &present); err != nil {
		return err
	}
	if raw, ok := present["rename"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Rename = &nullable
	}
	if raw, ok := present["nullable"]; ok {
		var nullable Nullable[bool]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Nullable = &nullable
	}
	if raw, ok := present["virtual_column"]; ok {
		var nullable Nullable[AlterVirtualColumnEntry]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.VirtualColumn = &nullable
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
	var present map[string]json.RawMessage
	if err := json.Unmarshal(data, &present); err != nil {
		return err
	}
	if raw, ok := present["where"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Where = &nullable
	}
	if raw, ok := present["concurrency"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Concurrency = &nullable
	}
	if raw, ok := present["intra_applier_concurrency"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.IntraApplierConcurrency = &nullable
	}
	if raw, ok := present["min_checkpoint_size"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.MinCheckpointSize = &nullable
	}
	if raw, ok := present["max_checkpoint_size"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.MaxCheckpointSize = &nullable
	}
	if raw, ok := present["batch_checkpoint_flush_interval_seconds"]; ok {
		var nullable Nullable[float64]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.BatchCheckpointFlushIntervalSeconds = &nullable
	}
	if raw, ok := present["read_version"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.ReadVersion = &nullable
	}
	if raw, ok := present["task_size"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.TaskSize = &nullable
	}
	if raw, ok := present["num_frags"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.NumFrags = &nullable
	}
	if raw, ok := present["checkpoint_size"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.CheckpointSize = &nullable
	}
	if raw, ok := present["commit_granularity"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.CommitGranularity = &nullable
	}
	if raw, ok := present["cluster"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Cluster = &nullable
	}
	if raw, ok := present["manifest"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Manifest = &nullable
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
	var present map[string]json.RawMessage
	if err := json.Unmarshal(data, &present); err != nil {
		return err
	}
	if raw, ok := present["input_columns"]; ok {
		var nullable Nullable[[]string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.InputColumns = &nullable
	}
	if raw, ok := present["image"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Image = &nullable
	}
	if raw, ok := present["udf"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.UDF = &nullable
	}
	if raw, ok := present["udf_name"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.UDFName = &nullable
	}
	if raw, ok := present["udf_version"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.UDFVersion = &nullable
	}
	if raw, ok := present["udf_backend"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.UDFBackend = &nullable
	}
	if raw, ok := present["auto_backfill"]; ok {
		var nullable Nullable[bool]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.AutoBackfill = &nullable
	}
	if raw, ok := present["manifest"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Manifest = &nullable
	}
	if raw, ok := present["manifest_checksum"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.ManifestChecksum = &nullable
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
	var present map[string]json.RawMessage
	if err := json.Unmarshal(data, &present); err != nil {
		return err
	}
	if raw, ok := present["udtf_spec"]; ok {
		var nullable Nullable[MaterializedViewUdtfEntry]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.UDTFSpec = &nullable
	}
	if raw, ok := present["auto_refresh"]; ok {
		var nullable Nullable[bool]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.AutoRefresh = &nullable
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
	var present map[string]json.RawMessage
	if err := json.Unmarshal(data, &present); err != nil {
		return err
	}
	if raw, ok := present["job_id"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.JobID = &nullable
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
	var present map[string]json.RawMessage
	if err := json.Unmarshal(data, &present); err != nil {
		return err
	}
	if raw, ok := present["input_columns"]; ok {
		var nullable Nullable[[]string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.InputColumns = &nullable
	}
	if raw, ok := present["partition_by"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.PartitionBy = &nullable
	}
	if raw, ok := present["partition_by_indexed_column"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.PartitionByIndexedColumn = &nullable
	}
	if raw, ok := present["num_cpus"]; ok {
		var nullable Nullable[float64]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.NumCpus = &nullable
	}
	if raw, ok := present["num_gpus"]; ok {
		var nullable Nullable[float64]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.NumGpus = &nullable
	}
	if raw, ok := present["memory"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Memory = &nullable
	}
	if raw, ok := present["error_handling"]; ok {
		var nullable Nullable[any]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.ErrorHandling = &nullable
	}
	if raw, ok := present["batch"]; ok {
		var nullable Nullable[bool]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Batch = &nullable
	}
	if raw, ok := present["manifest"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Manifest = &nullable
	}
	if raw, ok := present["manifest_checksum"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.ManifestChecksum = &nullable
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
	var present map[string]json.RawMessage
	if err := json.Unmarshal(data, &present); err != nil {
		return err
	}
	if raw, ok := present["src_version"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.SrcVersion = &nullable
	}
	if raw, ok := present["max_rows_per_fragment"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.MaxRowsPerFragment = &nullable
	}
	if raw, ok := present["concurrency"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Concurrency = &nullable
	}
	if raw, ok := present["intra_applier_concurrency"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.IntraApplierConcurrency = &nullable
	}
	if raw, ok := present["source_task_size"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.SourceTaskSize = &nullable
	}
	if raw, ok := present["cluster"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Cluster = &nullable
	}
	if raw, ok := present["output_limit"]; ok {
		var nullable Nullable[int32]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.OutputLimit = &nullable
	}
	if raw, ok := present["manifest"]; ok {
		var nullable Nullable[string]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Manifest = &nullable
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
	var present map[string]json.RawMessage
	if err := json.Unmarshal(data, &present); err != nil {
		return err
	}
	if raw, ok := present["replace"]; ok {
		var nullable Nullable[bool]
		if err := json.Unmarshal(raw, &nullable); err != nil {
			return err
		}
		decoded.Replace = &nullable
	}
	*v = UpdateFieldMetadataEntry(decoded)
	return nil
}
