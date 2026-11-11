// Command native88-fragment-fixtures produces ten disposable Lance datasets
// for the retained native88 fragment-mutation C and C++ consumers.
//
// This command is task-only fixture tooling. It is not part of the lance-go SDK
// and is excluded from the delivery PR. It creates fixtures using only the
// public Create, Append, DropColumns and Delete APIs; the mutation operations
// under acceptance are not exercised here.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/lance-format/lance-go"
)

const (
	sourceUpstreamCommit = "88c07cf8ccafebf579e769416251be370b45180e"
	sourceHeaderSHA256   = "c23406d5c683133eb647cfeb150fa05182ffc8ba7da283b3902dbb4bf0926c6c"

	rowsPerFragment = 4
)

var (
	fixtureFamilies  = []string{"c", "cpp"}
	fixtureMutations = []string{"add", "merge", "update", "replace", "delete"}
	expectedLiveIDs  = []int64{1, 3, 4, 5, 6, 7, 8, 9, 11, 12}
)

type fixtureManifest struct {
	Source   manifestSource `json:"source_fixture"`
	Schema   schemaFacts    `json:"schema"`
	Fixtures []fixtureFacts `json:"fixtures"`
}

type manifestSource struct {
	UpstreamCommit string `json:"upstream_commit"`
	HeaderSHA256   string `json:"header_sha256"`
}

type schemaFacts struct {
	Metadata map[string]string `json:"metadata"`
	Fields   []fieldFacts      `json:"fields"`
}

type fieldFacts struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Nullable bool              `json:"nullable"`
	Metadata map[string]string `json:"metadata"`
	FieldID  int32             `json:"field_id"`
	Children []fieldFacts      `json:"children,omitempty"`
}

type fixtureFacts struct {
	Name          string          `json:"name"`
	URI           string          `json:"uri"`
	Version       uint64          `json:"version"`
	RowCount      uint64          `json:"row_count"`
	FragmentCount int             `json:"fragment_count"`
	Fragments     []fragmentFacts `json:"fragments"`
	Rows          []rowFacts      `json:"rows"`
}

type fragmentFacts struct {
	ID           uint64 `json:"id"`
	PhysicalRows uint64 `json:"physical_rows"`
	DeletedRows  uint64 `json:"deleted_rows"`
	LogicalRows  uint64 `json:"logical_rows"`
}

type rowFacts struct {
	ID           int64  `json:"id"`
	Value        int32  `json:"value"`
	ProfileScore int32  `json:"profile_score"`
	ProfileTag   string `json:"profile_tag"`
	RowID        uint64 `json:"row_id"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "native88-fragment-fixtures: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s -output <fresh-directory>\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "Create ten independent native88 fragment-mutation fixtures plus manifest.json.")
		flag.PrintDefaults()
	}
	outputFlag := flag.String("output", "", "fresh local directory to receive the ten fixtures (must not exist)")
	flag.Parse()
	if *outputFlag == "" {
		flag.Usage()
		return errors.New("-output is required")
	}
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flag.Args())
	}

	output, err := filepath.Abs(*outputFlag)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	if err := prepareOutputDir(output); err != nil {
		return err
	}

	ctx := context.Background()
	schema := newFixtureSchema()
	manifest := fixtureManifest{
		Source: manifestSource{
			UpstreamCommit: sourceUpstreamCommit,
			HeaderSHA256:   sourceHeaderSHA256,
		},
		Fixtures: make([]fixtureFacts, 0, len(fixtureFamilies)*len(fixtureMutations)),
	}

	for _, family := range fixtureFamilies {
		for _, mutation := range fixtureMutations {
			name := family + "-" + mutation
			uri := filepath.Join(output, name)

			if err := buildFixture(ctx, uri, schema); err != nil {
				return fmt.Errorf("build %s: %w", name, err)
			}
			storedSchema, facts, err := validateFixture(ctx, uri)
			if err != nil {
				return fmt.Errorf("validate %s: %w", name, err)
			}
			facts.Name = name
			if len(manifest.Fixtures) == 0 {
				manifest.Schema = storedSchema
			}
			manifest.Fixtures = append(manifest.Fixtures, facts)
		}
	}

	manifestPath, err := writeManifest(output, manifest)
	if err != nil {
		return err
	}

	fmt.Printf("created and validated %d fixtures under %s\n", len(manifest.Fixtures), output)
	for _, fixture := range manifest.Fixtures {
		fmt.Printf("  %-10s %s\n", fixture.Name, fixture.URI)
	}
	fmt.Printf("manifest: %s\n", manifestPath)
	fmt.Println("consumer acceptance: NOT RUN (native88 C11/C++17 consumer execution remains parent-owned on NieR)")
	return nil
}

// prepareOutputDir creates the output directory and refuses to reuse an
// existing destination. It never removes or rewrites user data.
func prepareOutputDir(output string) error {
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create parent of output directory %q: %w", parent, err)
	}
	if err := os.Mkdir(output, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("output directory %q already exists; refusing to reuse or delete it", output)
		}
		return fmt.Errorf("create output directory %q: %w", output, err)
	}
	return nil
}

// newFixtureSchema builds the exact Arrow schema from the upstream native88
// fixture. Field order, nullability, metadata and the nested struct shape are
// part of the fixture contract.
func newFixtureSchema() *arrow.Schema {
	profileFields := []arrow.Field{
		{
			Name:     "score",
			Type:     arrow.PrimitiveTypes.Int32,
			Nullable: true,
			Metadata: arrow.NewMetadata([]string{"nested"}, []string{"score"}),
		},
		{
			Name:     "tag",
			Type:     arrow.BinaryTypes.String,
			Nullable: true,
		},
	}
	schemaMetadata := arrow.NewMetadata([]string{"fixture"}, []string{"fragment-mutation"})
	fields := []arrow.Field{
		{
			Name:     "id",
			Type:     arrow.PrimitiveTypes.Int64,
			Nullable: false,
			Metadata: arrow.NewMetadata([]string{"role"}, []string{"id"}),
		},
		{
			Name:     "value",
			Type:     arrow.PrimitiveTypes.Int32,
			Nullable: true,
			Metadata: arrow.NewMetadata([]string{"role"}, []string{"value"}),
		},
		{
			Name:     "stale",
			Type:     arrow.BinaryTypes.String,
			Nullable: true,
		},
		{
			Name:     "profile",
			Type:     arrow.StructOf(profileFields...),
			Nullable: true,
			Metadata: arrow.NewMetadata([]string{"role"}, []string{"nested"}),
		},
	}
	return arrow.NewSchema(fields, &schemaMetadata)
}

// buildFixture performs only the writes used by the upstream fixture recipe:
// create [1,2,3,4], append [5,6,7,8], append [9,10,11,12], drop stale, and
// delete ids 2 and 10.
func buildFixture(ctx context.Context, uri string, schema *arrow.Schema) error {
	if err := writeBatch(ctx, uri, schema, []int64{1, 2, 3, 4}, true); err != nil {
		return err
	}
	if err := writeBatch(ctx, uri, schema, []int64{5, 6, 7, 8}, false); err != nil {
		return err
	}
	if err := writeBatch(ctx, uri, schema, []int64{9, 10, 11, 12}, false); err != nil {
		return err
	}

	base, err := lance.Open(ctx, uri)
	if err != nil {
		return fmt.Errorf("open after appends: %w", err)
	}
	version, err := base.Version()
	if err != nil {
		_ = base.Close()
		return fmt.Errorf("read version after appends: %w", err)
	}
	if version != 3 {
		_ = base.Close()
		return fmt.Errorf("version after appends = %d, want 3", version)
	}
	dropped, err := base.DropColumns(ctx, "stale")
	closeErr := base.Close()
	if err != nil {
		return fmt.Errorf("drop column \"stale\": %w", err)
	}
	if closeErr != nil {
		_ = dropped.Close()
		return fmt.Errorf("close version 3 handle: %w", closeErr)
	}

	version, err = dropped.Version()
	if err != nil {
		_ = dropped.Close()
		return fmt.Errorf("read version after drop: %w", err)
	}
	if version != 4 {
		_ = dropped.Close()
		return fmt.Errorf("version after drop = %d, want 4", version)
	}
	deleted, count, err := dropped.Delete(ctx, "id IN (2, 10)")
	closeErr = dropped.Close()
	if err != nil {
		return fmt.Errorf("delete ids 2 and 10: %w", err)
	}
	if closeErr != nil {
		_ = deleted.Close()
		return fmt.Errorf("close version 4 handle: %w", closeErr)
	}
	if count != 2 {
		_ = deleted.Close()
		return fmt.Errorf("deleted row count = %d, want 2", count)
	}

	version, err = deleted.Version()
	if err != nil {
		_ = deleted.Close()
		return fmt.Errorf("read version after delete: %w", err)
	}
	if version != 5 {
		_ = deleted.Close()
		return fmt.Errorf("version after delete = %d, want 5", version)
	}
	if err := deleted.Close(); err != nil {
		return fmt.Errorf("close final fixture handle: %w", err)
	}
	return nil
}

// writeBatch builds one all-valid Arrow batch and writes it through either
// Create or Append. The record reader retains its input batch; both the reader
// and the batch are released on every path.
func writeBatch(ctx context.Context, uri string, schema *arrow.Schema, ids []int64, create bool) error {
	batch, err := newFixtureBatch(schema, ids)
	if err != nil {
		return fmt.Errorf("build batch: %w", err)
	}
	defer batch.Release()

	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{batch})
	if err != nil {
		return fmt.Errorf("open record reader: %w", err)
	}
	defer reader.Release()

	var dataset *lance.Dataset
	if create {
		dataset, err = lance.Create(ctx, uri, reader, lance.WithWriteParams(lance.WriteParams{
			MaxRowsPerFile:     rowsPerFragment,
			EnableStableRowIDs: true,
		}))
	} else {
		dataset, err = lance.Append(ctx, uri, reader)
	}
	if err != nil {
		return fmt.Errorf("write dataset: %w", err)
	}
	if err := dataset.Close(); err != nil {
		return fmt.Errorf("close dataset after write: %w", err)
	}
	return nil
}

func newFixtureBatch(schema *arrow.Schema, ids []int64) (arrow.RecordBatch, error) {
	idValues := make([]int64, len(ids))
	valueValues := make([]int32, len(ids))
	staleValues := make([]string, len(ids))
	scoreValues := make([]int32, len(ids))
	tagValues := make([]string, len(ids))
	for i, id := range ids {
		idValues[i] = id
		valueValues[i] = int32(id) * 10
		staleValues[i] = "stale"
		scoreValues[i] = int32(id) * 100
		tagValues[i] = fmt.Sprintf("tag-%d", id)
	}

	idArray := buildInt64Array(idValues)
	defer idArray.Release()
	valueArray := buildInt32Array(valueValues)
	defer valueArray.Release()
	staleArray := buildStringArray(staleValues)
	defer staleArray.Release()
	scoreArray := buildInt32Array(scoreValues)
	defer scoreArray.Release()
	tagArray := buildStringArray(tagValues)
	defer tagArray.Release()

	profileFields := schema.Field(3).Type.(*arrow.StructType).Fields()
	profileArray, err := array.NewStructArrayWithFields([]arrow.Array{scoreArray, tagArray}, profileFields)
	if err != nil {
		return nil, fmt.Errorf("build profile struct: %w", err)
	}
	defer profileArray.Release()

	return array.NewRecordBatch(schema, []arrow.Array{
		idArray,
		valueArray,
		staleArray,
		profileArray,
	}, int64(len(ids))), nil
}

func buildInt64Array(values []int64) arrow.Array {
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	defer builder.Release()
	builder.AppendValues(values, nil)
	return builder.NewArray()
}

func buildInt32Array(values []int32) arrow.Array {
	builder := array.NewInt32Builder(memory.DefaultAllocator)
	defer builder.Release()
	builder.AppendValues(values, nil)
	return builder.NewArray()
}

func buildStringArray(values []string) arrow.Array {
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	defer builder.Release()
	builder.AppendValues(values, nil)
	return builder.NewArray()
}

// validateFixture reopens one persisted dataset and checks every literal
// prerequisite used by the native88 consumers.
func validateFixture(ctx context.Context, uri string) (schemaFacts, fixtureFacts, error) {
	dataset, err := lance.Open(ctx, uri)
	if err != nil {
		return schemaFacts{}, fixtureFacts{}, fmt.Errorf("reopen dataset: %w", err)
	}
	defer func() { _ = dataset.Close() }()

	version, err := dataset.Version()
	if err != nil {
		return schemaFacts{}, fixtureFacts{}, fmt.Errorf("read version: %w", err)
	}
	if version != 5 {
		return schemaFacts{}, fixtureFacts{}, fmt.Errorf("version = %d, want 5", version)
	}

	rowCount, err := dataset.CountRows(ctx)
	if err != nil {
		return schemaFacts{}, fixtureFacts{}, fmt.Errorf("count rows: %w", err)
	}
	if rowCount != uint64(len(expectedLiveIDs)) {
		return schemaFacts{}, fixtureFacts{}, fmt.Errorf("row count = %d, want %d", rowCount, len(expectedLiveIDs))
	}

	storedSchema, err := validateSchema(dataset)
	if err != nil {
		return schemaFacts{}, fixtureFacts{}, err
	}

	fragmentCount, err := dataset.FragmentCount()
	if err != nil {
		return schemaFacts{}, fixtureFacts{}, fmt.Errorf("fragment count: %w", err)
	}
	if fragmentCount != 3 {
		return schemaFacts{}, fixtureFacts{}, fmt.Errorf("fragment count = %d, want 3", fragmentCount)
	}
	fragments, err := collectFragmentFacts(dataset)
	if err != nil {
		return schemaFacts{}, fixtureFacts{}, err
	}
	if err := validateFragmentFacts(fragments); err != nil {
		return schemaFacts{}, fixtureFacts{}, err
	}

	rows, err := scanFixtureRows(ctx, dataset)
	if err != nil {
		return schemaFacts{}, fixtureFacts{}, err
	}
	if uint64(len(rows)) != rowCount {
		return schemaFacts{}, fixtureFacts{}, fmt.Errorf("scan returned %d rows but CountRows reported %d", len(rows), rowCount)
	}
	if err := validateRowFacts(rows); err != nil {
		return schemaFacts{}, fixtureFacts{}, err
	}

	return storedSchema, fixtureFacts{
		URI:           uri,
		Version:       version,
		RowCount:      rowCount,
		FragmentCount: len(fragments),
		Fragments:     fragments,
		Rows:          rows,
	}, nil
}

func validateSchema(dataset *lance.Dataset) (schemaFacts, error) {
	native, err := dataset.NativeSchema()
	if err != nil {
		return schemaFacts{}, fmt.Errorf("native schema: %w", err)
	}
	defer func() { _ = native.Close() }()

	arrowSchema, err := native.Arrow()
	if err != nil {
		return schemaFacts{}, fmt.Errorf("arrow view of native schema: %w", err)
	}
	if got := arrowSchema.NumFields(); got != 3 {
		return schemaFacts{}, fmt.Errorf("schema has %d fields, want 3 (id, value, profile after dropping stale)", got)
	}
	if err := checkMetadata("schema", arrowSchema.Metadata(), map[string]string{"fixture": "fragment-mutation"}); err != nil {
		return schemaFacts{}, err
	}
	if _, err := native.FieldID("stale"); err == nil {
		return schemaFacts{}, errors.New("dropped column \"stale\" still has a field ID")
	}

	facts, err := collectSchemaFacts(native, arrowSchema)
	if err != nil {
		return schemaFacts{}, err
	}
	if err := compareSchemaFacts(facts, expectedSchemaFacts()); err != nil {
		return schemaFacts{}, err
	}
	return facts, nil
}

func collectSchemaFacts(native *lance.NativeSchema, schema *arrow.Schema) (schemaFacts, error) {
	facts := schemaFacts{
		Metadata: schema.Metadata().ToMap(),
		Fields:   make([]fieldFacts, 0, schema.NumFields()),
	}
	for i := 0; i < schema.NumFields(); i++ {
		field, err := collectFieldFacts(native, "", schema.Field(i))
		if err != nil {
			return schemaFacts{}, err
		}
		facts.Fields = append(facts.Fields, field)
	}
	return facts, nil
}

func collectFieldFacts(native *lance.NativeSchema, parent string, field arrow.Field) (fieldFacts, error) {
	path := field.Name
	if parent != "" {
		path = parent + "." + field.Name
	}
	fieldID, err := native.FieldID(path)
	if err != nil {
		return fieldFacts{}, fmt.Errorf("field ID for %q: %w", path, err)
	}
	facts := fieldFacts{
		Name:     field.Name,
		Type:     describeType(field.Type),
		Nullable: field.Nullable,
		Metadata: field.Metadata.ToMap(),
		FieldID:  fieldID,
	}
	if structType, ok := field.Type.(*arrow.StructType); ok {
		facts.Children = make([]fieldFacts, 0, structType.NumFields())
		for i := 0; i < structType.NumFields(); i++ {
			child, err := collectFieldFacts(native, path, structType.Field(i))
			if err != nil {
				return fieldFacts{}, err
			}
			facts.Children = append(facts.Children, child)
		}
	}
	return facts, nil
}

func describeType(dataType arrow.DataType) string {
	switch dataType.ID() {
	case arrow.INT64:
		return "int64"
	case arrow.INT32:
		return "int32"
	case arrow.STRING:
		return "utf8"
	case arrow.STRUCT:
		structType := dataType.(*arrow.StructType)
		parts := make([]string, 0, structType.NumFields())
		for i := 0; i < structType.NumFields(); i++ {
			field := structType.Field(i)
			parts = append(parts, field.Name+":"+describeType(field.Type))
		}
		return "struct<" + strings.Join(parts, ",") + ">"
	default:
		return dataType.String()
	}
}

func expectedSchemaFacts() schemaFacts {
	return schemaFacts{
		Metadata: map[string]string{"fixture": "fragment-mutation"},
		Fields: []fieldFacts{
			{
				Name:     "id",
				Type:     "int64",
				Nullable: false,
				Metadata: map[string]string{"role": "id"},
				FieldID:  0,
			},
			{
				Name:     "value",
				Type:     "int32",
				Nullable: true,
				Metadata: map[string]string{"role": "value"},
				FieldID:  1,
			},
			{
				Name:     "profile",
				Type:     "struct<score:int32,tag:utf8>",
				Nullable: true,
				Metadata: map[string]string{"role": "nested"},
				FieldID:  3,
				Children: []fieldFacts{
					{
						Name:     "score",
						Type:     "int32",
						Nullable: true,
						Metadata: map[string]string{"nested": "score"},
						FieldID:  4,
					},
					{
						Name:     "tag",
						Type:     "utf8",
						Nullable: true,
						Metadata: map[string]string{},
						FieldID:  5,
					},
				},
			},
		},
	}
}

func compareSchemaFacts(got, want schemaFacts) error {
	if reflect.DeepEqual(got, want) {
		return nil
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	return fmt.Errorf("stored schema does not match the native88 fragment-mutation fixture:\n got: %s\nwant: %s", gotJSON, wantJSON)
}

func checkMetadata(where string, got arrow.Metadata, want map[string]string) error {
	actual := got.ToMap()
	if len(actual) != len(want) {
		return fmt.Errorf("%s metadata = %v, want %v", where, actual, want)
	}
	for key, wantValue := range want {
		if gotValue, ok := actual[key]; !ok || gotValue != wantValue {
			return fmt.Errorf("%s metadata = %v, want %v", where, actual, want)
		}
	}
	return nil
}

func collectFragmentFacts(dataset *lance.Dataset) ([]fragmentFacts, error) {
	fragments, err := dataset.Fragments()
	if err != nil {
		return nil, fmt.Errorf("open fragments: %w", err)
	}
	defer closeFragments(fragments)

	facts := make([]fragmentFacts, 0, len(fragments))
	for i, fragment := range fragments {
		physicalRows, err := fragment.PhysicalRows()
		if err != nil {
			return nil, fmt.Errorf("fragment %d physical rows: %w", i, err)
		}
		deletedRows, err := fragment.DeletedRows()
		if err != nil {
			return nil, fmt.Errorf("fragment %d deleted rows: %w", i, err)
		}
		logicalRows, err := fragment.LogicalRows()
		if err != nil {
			return nil, fmt.Errorf("fragment %d logical rows: %w", i, err)
		}
		facts = append(facts, fragmentFacts{
			ID:           fragment.ID(),
			PhysicalRows: physicalRows,
			DeletedRows:  deletedRows,
			LogicalRows:  logicalRows,
		})
	}
	return facts, nil
}

func closeFragments(fragments []*lance.Fragment) {
	for _, fragment := range fragments {
		_ = fragment.Close()
	}
}

func validateFragmentFacts(fragments []fragmentFacts) error {
	if len(fragments) != 3 {
		return fmt.Errorf("opened %d fragments, want 3", len(fragments))
	}
	want := []struct {
		physical uint64
		deleted  uint64
		logical  uint64
	}{
		{physical: 4, deleted: 1, logical: 3},
		{physical: 4, deleted: 0, logical: 4},
		{physical: 4, deleted: 1, logical: 3},
	}
	seen := make(map[uint64]struct{}, len(fragments))
	for i, fragment := range fragments {
		if _, duplicate := seen[fragment.ID]; duplicate {
			return fmt.Errorf("fragment ID %d appears more than once", fragment.ID)
		}
		seen[fragment.ID] = struct{}{}
		if fragment.PhysicalRows != want[i].physical ||
			fragment.DeletedRows != want[i].deleted ||
			fragment.LogicalRows != want[i].logical {
			return fmt.Errorf(
				"fragment %d (ID %d) physical/deleted/logical rows = %d/%d/%d, want %d/%d/%d",
				i, fragment.ID,
				fragment.PhysicalRows, fragment.DeletedRows, fragment.LogicalRows,
				want[i].physical, want[i].deleted, want[i].logical,
			)
		}
	}
	return nil
}

func scanFixtureRows(ctx context.Context, dataset *lance.Dataset) ([]rowFacts, error) {
	scanner, err := dataset.Scan(lance.WithRowID(true))
	if err != nil {
		return nil, fmt.Errorf("scan with stable row IDs: %w", err)
	}
	defer func() { _ = scanner.Close() }()

	rows := make(map[int64]rowFacts, len(expectedLiveIDs))
	rowIDs := make(map[uint64]int64, len(expectedLiveIDs))
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			return nil, fmt.Errorf("scan records: %w", err)
		}
		if err := collectBatchRows(batch, rows, rowIDs); err != nil {
			return nil, err
		}
	}

	ids := make([]int64, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	sortedRows := make([]rowFacts, 0, len(ids))
	for _, id := range ids {
		sortedRows = append(sortedRows, rows[id])
	}
	return sortedRows, nil
}

func collectBatchRows(batch arrow.RecordBatch, rows map[int64]rowFacts, rowIDs map[uint64]int64) error {
	batchSchema := batch.Schema()
	if _, present := columnIndex(batchSchema, "stale"); present {
		return errors.New("scan batch still contains the dropped \"stale\" column")
	}
	idColumn, err := int64Column(batch, "id")
	if err != nil {
		return err
	}
	valueColumn, err := int32Column(batch, "value")
	if err != nil {
		return err
	}
	rowIDColumn, err := uint64Column(batch, "_rowid")
	if err != nil {
		return err
	}
	profileColumn, err := structColumn(batch, "profile")
	if err != nil {
		return err
	}

	profileType := profileColumn.DataType().(*arrow.StructType)
	scoreIndex, ok := profileType.FieldIdx("score")
	if !ok {
		return errors.New("profile struct has no \"score\" child")
	}
	tagIndex, ok := profileType.FieldIdx("tag")
	if !ok {
		return errors.New("profile struct has no \"tag\" child")
	}
	scoreColumn, ok := profileColumn.Field(scoreIndex).(*array.Int32)
	if !ok {
		return fmt.Errorf("profile.score has type %T, want *array.Int32", profileColumn.Field(scoreIndex))
	}
	tagColumn, ok := profileColumn.Field(tagIndex).(*array.String)
	if !ok {
		return fmt.Errorf("profile.tag has type %T, want *array.String", profileColumn.Field(tagIndex))
	}

	profileOffset := int(profileColumn.Offset())
	if profileOffset+profileColumn.Len() > scoreColumn.Len() || profileOffset+profileColumn.Len() > tagColumn.Len() {
		return fmt.Errorf("profile children are shorter than the struct: profile limit %d, score length %d, tag length %d",
			profileOffset+profileColumn.Len(), scoreColumn.Len(), tagColumn.Len())
	}

	for i := 0; i < int(batch.NumRows()); i++ {
		if !idColumn.IsValid(i) {
			return fmt.Errorf("row %d has a null id", i)
		}
		id := idColumn.Value(i)
		if !valueColumn.IsValid(i) {
			return fmt.Errorf("row id %d has a null value", id)
		}
		if !profileColumn.IsValid(i) {
			return fmt.Errorf("row id %d has a null profile", id)
		}
		profileIndex := i + profileOffset
		if !scoreColumn.IsValid(profileIndex) {
			return fmt.Errorf("row id %d has a null profile.score", id)
		}
		if !tagColumn.IsValid(profileIndex) {
			return fmt.Errorf("row id %d has a null profile.tag", id)
		}
		if !rowIDColumn.IsValid(i) {
			return fmt.Errorf("row id %d has a null stable row ID", id)
		}

		rowID := rowIDColumn.Value(i)
		if _, duplicate := rows[id]; duplicate {
			return fmt.Errorf("duplicate id %d in scan", id)
		}
		if otherID, duplicate := rowIDs[rowID]; duplicate {
			return fmt.Errorf("stable row ID %d is shared by ids %d and %d", rowID, otherID, id)
		}
		rowIDs[rowID] = id

		value := valueColumn.Value(i)
		if want := int32(id) * 10; value != want {
			return fmt.Errorf("row id %d: value = %d, want %d", id, value, want)
		}
		score := scoreColumn.Value(profileIndex)
		if want := int32(id) * 100; score != want {
			return fmt.Errorf("row id %d: profile.score = %d, want %d", id, score, want)
		}
		tag := tagColumn.Value(profileIndex)
		if want := fmt.Sprintf("tag-%d", id); tag != want {
			return fmt.Errorf("row id %d: profile.tag = %q, want %q", id, tag, want)
		}

		rows[id] = rowFacts{
			ID:           id,
			Value:        value,
			ProfileScore: score,
			ProfileTag:   strings.Clone(tag),
			RowID:        rowID,
		}
	}
	return nil
}

func columnIndex(schema *arrow.Schema, name string) (int, bool) {
	for i := 0; i < schema.NumFields(); i++ {
		if schema.Field(i).Name == name {
			return i, true
		}
	}
	return 0, false
}

func int64Column(batch arrow.RecordBatch, name string) (*array.Int64, error) {
	index, ok := columnIndex(batch.Schema(), name)
	if !ok {
		return nil, fmt.Errorf("scan batch has no %q column", name)
	}
	column, ok := batch.Column(index).(*array.Int64)
	if !ok {
		return nil, fmt.Errorf("%q column has type %T, want *array.Int64", name, batch.Column(index))
	}
	return column, nil
}

func int32Column(batch arrow.RecordBatch, name string) (*array.Int32, error) {
	index, ok := columnIndex(batch.Schema(), name)
	if !ok {
		return nil, fmt.Errorf("scan batch has no %q column", name)
	}
	column, ok := batch.Column(index).(*array.Int32)
	if !ok {
		return nil, fmt.Errorf("%q column has type %T, want *array.Int32", name, batch.Column(index))
	}
	return column, nil
}

func uint64Column(batch arrow.RecordBatch, name string) (*array.Uint64, error) {
	index, ok := columnIndex(batch.Schema(), name)
	if !ok {
		return nil, fmt.Errorf("scan batch has no %q column", name)
	}
	column, ok := batch.Column(index).(*array.Uint64)
	if !ok {
		return nil, fmt.Errorf("%q column has type %T, want *array.Uint64", name, batch.Column(index))
	}
	return column, nil
}

func structColumn(batch arrow.RecordBatch, name string) (*array.Struct, error) {
	index, ok := columnIndex(batch.Schema(), name)
	if !ok {
		return nil, fmt.Errorf("scan batch has no %q column", name)
	}
	column, ok := batch.Column(index).(*array.Struct)
	if !ok {
		return nil, fmt.Errorf("%q column has type %T, want *array.Struct", name, batch.Column(index))
	}
	return column, nil
}

func validateRowFacts(rows []rowFacts) error {
	if len(rows) != len(expectedLiveIDs) {
		return fmt.Errorf("live row count = %d, want %d", len(rows), len(expectedLiveIDs))
	}
	for i, wantID := range expectedLiveIDs {
		if rows[i].ID != wantID {
			ids := make([]int64, len(rows))
			for j, row := range rows {
				ids[j] = row.ID
			}
			return fmt.Errorf("live ids = %v, want %v (first mismatch at sorted position %d: got %d, want %d)", ids, expectedLiveIDs, i, rows[i].ID, wantID)
		}
	}

	foundID3 := false
	for _, row := range rows {
		if row.ID == 3 {
			foundID3 = true
			if row.RowID != 2 {
				return fmt.Errorf("stable row ID for id 3 = %d, want 2", row.RowID)
			}
		}
	}
	if !foundID3 {
		return errors.New("id 3 is missing from the live rows")
	}
	return nil
}

func writeManifest(output string, manifest fixtureManifest) (string, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(output, "manifest.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write manifest %q: %w", path, err)
	}
	return path, nil
}
