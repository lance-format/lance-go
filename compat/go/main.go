// Command lance-compat-go is the Go producer/consumer for the Phase 5
// cross-language Lance compatibility fixture. It is deliberately small: it
// writes the committed fixture through lance-go and prints a canonical report
// read back through lance-go.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	lance "github.com/hyhtvyj/lance-go"
)

const (
	baseRows   = 4
	appendRows = 2
)

type fieldSpec struct {
	path     string
	name     string
	typeName string
	nullable bool
	metadata map[string]string
}

var topLevelSpecs = []fieldSpec{
	{path: "0", name: "row_key", typeName: "utf8", nullable: false, metadata: map[string]string{"role": "primary", "unit": "key"}},
	{path: "1", name: "id", typeName: "int64", nullable: true},
	{path: "2", name: "label", typeName: "utf8", nullable: true},
	{path: "3", name: "payload", typeName: "binary", nullable: true},
	{path: "4", name: "tags", typeName: "list<utf8>", nullable: true},
	{path: "5", name: "profile", typeName: "struct", nullable: true, metadata: map[string]string{"encoding": "struct"}},
	{path: "6", name: "embedding", typeName: "fixed_size_list<float32,4>", nullable: true, metadata: map[string]string{"dim": "4", "metric": "l2"}},
	{path: "7", name: "amount", typeName: "decimal128(38,9)", nullable: true, metadata: map[string]string{"precision": "38", "unit": "currency"}},
	{path: "8", name: "ts_us", typeName: "timestamp<us,UTC>", nullable: true},
	{path: "9", name: "ts_ns", typeName: "timestamp<ns,Asia/Shanghai>", nullable: true},
}

var nestedSpecs = []fieldSpec{
	{path: "5.0", name: "name", typeName: "utf8", nullable: true, metadata: map[string]string{"role": "display"}},
	{path: "5.1", name: "score", typeName: "int64", nullable: true},
	{path: "5.2", name: "attrs", typeName: "list<int32>", nullable: true},
}

func metadata(pairs ...string) arrow.Metadata {
	keys := make([]string, 0, len(pairs)/2)
	values := make([]string, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		keys = append(keys, pairs[i])
		values = append(values, pairs[i+1])
	}
	return arrow.NewMetadata(keys, values)
}

func emptyMetadata() arrow.Metadata {
	return arrow.Metadata{}
}

func listField(name string, elem arrow.DataType, md arrow.Metadata) arrow.Field {
	return arrow.Field{
		Name:     name,
		Type:     arrow.ListOfField(arrow.Field{Name: "item", Type: elem, Nullable: true}),
		Nullable: true,
		Metadata: md,
	}
}

func expectedSchema() *arrow.Schema {
	profileFields := []arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true, Metadata: metadata("role", "display")},
		{Name: "score", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "attrs", Type: arrow.ListOfField(arrow.Field{Name: "item", Type: arrow.PrimitiveTypes.Int32, Nullable: true}), Nullable: true},
	}
	fields := []arrow.Field{
		{Name: "row_key", Type: arrow.BinaryTypes.String, Nullable: false, Metadata: metadata("role", "primary", "unit", "key")},
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "label", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "payload", Type: arrow.BinaryTypes.Binary, Nullable: true},
		listField("tags", arrow.BinaryTypes.String, emptyMetadata()),
		{Name: "profile", Type: arrow.StructOf(profileFields...), Nullable: true, Metadata: metadata("encoding", "struct")},
		{Name: "embedding", Type: arrow.FixedSizeListOfField(4, arrow.Field{Name: "item", Type: arrow.PrimitiveTypes.Float32, Nullable: true}), Nullable: true, Metadata: metadata("dim", "4", "metric", "l2")},
		{Name: "amount", Type: &arrow.Decimal128Type{Precision: 38, Scale: 9}, Nullable: true, Metadata: metadata("precision", "38", "unit", "currency")},
		{Name: "ts_us", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, Nullable: true},
		{Name: "ts_ns", Type: &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "Asia/Shanghai"}, Nullable: true},
	}
	md := metadata("fixture", "phase5-v1", "owner", "interop")
	return arrow.NewSchema(fields, &md)
}

func typeName(dt arrow.DataType) string {
	switch t := dt.(type) {
	case *arrow.StringType:
		return "utf8"
	case *arrow.Int64Type:
		return "int64"
	case *arrow.BinaryType:
		return "binary"
	case *arrow.ListType:
		return "list<" + typeName(t.Elem().Type) + ">"
	case *arrow.StructType:
		return "struct"
	case *arrow.FixedSizeListType:
		return fmt.Sprintf("fixed_size_list<%s,%d>", typeName(t.Elem().Type), t.Len())
	case *arrow.Decimal128Type:
		return fmt.Sprintf("decimal128(%d,%d)", t.Precision, t.Scale)
	case *arrow.TimestampType:
		return fmt.Sprintf("timestamp<%s,%s>", t.Unit.String(), t.TimeZone)
	default:
		return dt.String()
	}
}

func metadataEqual(actual arrow.Metadata, expected map[string]string) bool {
	if actual.Len() != len(expected) {
		return false
	}
	got := actual.ToMap()
	for k, v := range expected {
		if got[k] != v {
			return false
		}
	}
	return true
}

func validateField(actual arrow.Field, spec fieldSpec) error {
	if actual.Name != spec.name {
		return fmt.Errorf("field %s: name %q, want %q", spec.path, actual.Name, spec.name)
	}
	if got := typeName(actual.Type); got != spec.typeName {
		return fmt.Errorf("field %s: type %q, want %q", spec.path, got, spec.typeName)
	}
	if actual.Nullable != spec.nullable {
		return fmt.Errorf("field %s: nullable %v, want %v", spec.path, actual.Nullable, spec.nullable)
	}
	if !metadataEqual(actual.Metadata, spec.metadata) {
		return fmt.Errorf("field %s: metadata %v, want %v", spec.path, actual.Metadata.ToMap(), spec.metadata)
	}
	return nil
}

func validateSchema(schema *arrow.Schema) error {
	if schema == nil {
		return errors.New("nil schema")
	}
	if !metadataEqual(schema.Metadata, map[string]string{"fixture": "phase5-v1", "owner": "interop"}) {
		return fmt.Errorf("schema metadata %v", schema.Metadata.ToMap())
	}
	fields := schema.Fields()
	if len(fields) != len(topLevelSpecs) {
		return fmt.Errorf("top-level field count %d, want %d", len(fields), len(topLevelSpecs))
	}
	for i, spec := range topLevelSpecs {
		if err := validateField(fields[i], spec); err != nil {
			return err
		}
	}
	structType, ok := fields[5].Type.(*arrow.StructType)
	if !ok {
		return errors.New("profile is not a struct")
	}
	children := structType.Fields()
	if len(children) != len(nestedSpecs) {
		return fmt.Errorf("profile child count %d, want %d", len(children), len(nestedSpecs))
	}
	for i, spec := range nestedSpecs {
		if err := validateField(children[i], spec); err != nil {
			return err
		}
	}
	tags := fields[4].Type.(*arrow.ListType)
	if tags.Elem().Name != "item" || !tags.Elem().Nullable {
		return errors.New("tags element field is not nullable item")
	}
	embedding := fields[6].Type.(*arrow.FixedSizeListType)
	if embedding.Len() != 4 || embedding.Elem().Name != "item" || !embedding.Elem().Nullable {
		return errors.New("embedding element field is not nullable item with length 4")
	}
	return nil
}

func f32(bits uint32) float32 { return math.Float32frombits(bits) }

func decimalValue(s string) decimal128.Num {
	i, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("bad decimal literal " + s)
	}
	return decimal128.FromBigInt(i)
}

func appendString(b *array.StringBuilder, value *string) {
	if value == nil {
		b.AppendNull()
		return
	}
	b.Append(*value)
}

func appendBinary(b *array.BinaryBuilder, value []byte, valid bool) {
	if !valid {
		b.AppendNull()
		return
	}
	b.Append(value)
}

func buildRows(mem memory.Allocator, keys []string, ids []int64, idValid []bool, labels []string, labelValid []bool, payloads [][]byte, payloadValid []bool, tags [][]string, tagValid []bool, profileNames []*string, profileScores []*int64, profileAttrs [][]int32, profileAttrsValid []bool, profileValid []bool, embeddings [][4]uint32, embeddingValid []bool, decimals []string, decimalValid []bool, tsUS []int64, tsUSValid []bool, tsNS []int64, tsNSValid []bool) (arrow.RecordBatch, error) {
	schema := expectedSchema()
	rowKey := array.NewStringBuilder(mem)
	defer rowKey.Release()
	for _, key := range keys {
		rowKey.Append(key)
	}

	id := array.NewInt64Builder(mem)
	defer id.Release()
	for i, value := range ids {
		if idValid[i] {
			id.Append(value)
		} else {
			id.AppendNull()
		}
	}

	label := array.NewStringBuilder(mem)
	defer label.Release()
	for i, value := range labels {
		if labelValid[i] {
			label.Append(value)
		} else {
			label.AppendNull()
		}
	}

	payload := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
	defer payload.Release()
	for i, value := range payloads {
		appendBinary(payload, value, payloadValid[i])
	}

	tagsBuilder := array.NewListBuilder(mem, arrow.BinaryTypes.String)
	defer tagsBuilder.Release()
	tagsValues := tagsBuilder.ValueBuilder().(*array.StringBuilder)
	for i := range keys {
		tagsBuilder.Append(tagValid[i])
		if !tagValid[i] {
			continue
		}
		for _, value := range tags[i] {
			tagsValues.Append(value)
		}
	}
	tagsArray := tagsBuilder.NewArray()

	profileType := schema.Field(5).Type.(*arrow.StructType)
	profile := array.NewStructBuilder(mem, profileType)
	defer profile.Release()
	profileName := profile.FieldBuilder(0).(*array.StringBuilder)
	profileScore := profile.FieldBuilder(1).(*array.Int64Builder)
	profileAttrs := profile.FieldBuilder(2).(*array.ListBuilder)
	profileAttrsValues := profileAttrs.ValueBuilder().(*array.Int32Builder)
	for i := range keys {
		// StructBuilder.Append(false) already appends nulls to every child.
		profile.Append(profileValid[i])
		if !profileValid[i] {
			continue
		}
		appendString(profileName, profileNames[i])
		if profileScores[i] == nil {
			profileScore.AppendNull()
		} else {
			profileScore.Append(*profileScores[i])
		}
		profileAttrs.Append(profileAttrsValid[i])
		if profileAttrsValid[i] {
			for _, value := range profileAttrs[i] {
				profileAttrsValues.Append(value)
			}
		}
	}
	profileArray := profile.NewArray()

	embedding := array.NewFixedSizeListBuilder(mem, 4, arrow.PrimitiveTypes.Float32)
	defer embedding.Release()
	embeddingValues := embedding.ValueBuilder().(*array.Float32Builder)
	for i := range keys {
		embedding.Append(embeddingValid[i])
		for _, bits := range embeddings[i] {
			embeddingValues.Append(f32(bits))
		}
	}
	embeddingArray := embedding.NewArray()

	amount := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: 38, Scale: 9})
	defer amount.Release()
	for i, value := range decimals {
		if decimalValid[i] {
			amount.Append(decimalValue(value))
		} else {
			amount.AppendNull()
		}
	}
	amountArray := amount.NewArray()

	tsUSB := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"})
	defer tsUSB.Release()
	for i, value := range tsUS {
		if tsUSValid[i] {
			tsUSB.Append(arrow.Timestamp(value))
		} else {
			tsUSB.AppendNull()
		}
	}
	tsUSArray := tsUSB.NewArray()

	tsNSB := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "Asia/Shanghai"})
	defer tsNSB.Release()
	for i, value := range tsNS {
		if tsNSValid[i] {
			tsNSB.Append(arrow.Timestamp(value))
		} else {
			tsNSB.AppendNull()
		}
	}
	tsNSArray := tsNSB.NewArray()

	arrays := []arrow.Array{
		rowKey.NewArray(), id.NewArray(), label.NewArray(), payload.NewArray(), tagsArray,
		profileArray, embeddingArray, amountArray, tsUSArray, tsNSArray,
	}
	// The builders above own the returned arrays until they are released; the
	// record batch retains each array before the builders are released below.
	record := array.NewRecordBatch(schema, arrays, int64(len(keys)))
	for _, a := range arrays {
		a.Release()
	}
	return record, nil
}

func baseRecord(mem memory.Allocator) (arrow.RecordBatch, error) {
	keys := []string{"r1", "r2", "r3", "r4"}
	ids := []int64{9007199254740993, -9007199254740993, 0, 9007199254740992}
	idValid := []bool{true, true, false, true}
	labels := []string{"alpha", "", "", "café 世界"}
	labelValid := []bool{true, true, false, true}
	payloads := [][]byte{{0x00, 0xff, 0x10}, {}, nil, {0x80}}
	payloadValid := []bool{true, true, false, true}
	tags := [][]string{{"a", "b"}, {}, nil, {"", "x"}}
	tagValid := []bool{true, true, false, true}
	nameAda := "Ada"
	nameEmpty := ""
	score := int64(9007199254740993)
	scoreZero := int64(0)
	profileNames := []*string{&nameAda, nil, nil, &nameEmpty}
	profileScores := []*int64{&score, nil, nil, &scoreZero}
	profileAttrs := [][]int32{{1, 2}, {}, nil, nil}
	profileAttrsValid := []bool{true, true, true, false}
	profileValid := []bool{true, true, false, true}
	embeddings := [][4]uint32{
		{0x3dcccccd, 0xc0200000, 0x00000000, 0x3f800000},
		{0, 0, 0, 0},
		{0x80000000, 0x7f7fffff, 0x00000001, 0x3f800000},
		{0x3fc00000, 0x80000000, 0x00000001, 0x40400000},
	}
	embeddingValid := []bool{true, false, true, true}
	decimals := []string{
		"12345678901234567890123456789123456789",
		"-1",
		"0",
		"-12345678901234567890123456789123456789",
	}
	decimalValid := []bool{true, true, true, true}
	tsUS := []int64{1704164645123456, 0, 0, 1704164645123456}
	tsUSValid := []bool{true, true, false, true}
	tsNS := []int64{1704164645123456789, -1, 0, 1704164645000000000}
	tsNSValid := []bool{true, true, false, true}
	return buildRows(mem, keys, ids, idValid, labels, labelValid, payloads, payloadValid, tags, tagValid, profileNames, profileScores, profileAttrs, profileAttrsValid, profileValid, embeddings, embeddingValid, decimals, decimalValid, tsUS, tsUSValid, tsNS, tsNSValid)
}

func appendRecord(mem memory.Allocator) (arrow.RecordBatch, error) {
	keys := []string{"x1", "x2"}
	ids := []int64{9007199254740995, 0}
	idValid := []bool{true, false}
	labels := []string{"append-1", ""}
	labelValid := []bool{true, false}
	payloads := [][]byte{{0xde, 0xad}, nil}
	payloadValid := []bool{true, false}
	tags := [][]string{{"x1"}, nil}
	tagValid := []bool{true, false}
	nameApp := "App"
	score := int64(1)
	profileNames := []*string{&nameApp, nil}
	profileScores := []*int64{&score, nil}
	profileAttrs := [][]int32{{3}, nil}
	profileAttrsValid := []bool{true, false}
	profileValid := []bool{true, false}
	embeddings := [][4]uint32{
		{0x3f800000, 0x3f800000, 0x3f800000, 0x3f800000},
		{0, 0, 0, 0},
	}
	embeddingValid := []bool{true, false}
	decimals := []string{"1000000000", "0"}
	decimalValid := []bool{true, false}
	tsUS := []int64{1, 0}
	tsUSValid := []bool{true, false}
	tsNS := []int64{1, 0}
	tsNSValid := []bool{true, false}
	return buildRows(mem, keys, ids, idValid, labels, labelValid, payloads, payloadValid, tags, tagValid, profileNames, profileScores, profileAttrs, profileAttrsValid, profileValid, embeddings, embeddingValid, decimals, decimalValid, tsUS, tsUSValid, tsNS, tsNSValid)
}

func writeRecord(ctx context.Context, path string, record arrow.RecordBatch, mode, format string) error {
	reader, err := array.NewRecordReader(record.Schema(), []arrow.RecordBatch{record})
	if err != nil {
		return err
	}
	defer reader.Release()
	params := lance.WithWriteParams(lance.WriteParams{DataStorageVersion: format})
	var ds *lance.Dataset
	if mode == "create" {
		ds, err = lance.Create(ctx, path, reader, params)
	} else {
		ds, err = lance.Append(ctx, path, reader, params)
	}
	if err != nil {
		return err
	}
	return ds.Close()
}

func hexString(value string) string { return "s:" + hex.EncodeToString([]byte(value)) }

func formatDecimal(value decimal128.Num, scale int) string {
	n := value.BigInt()
	sign := ""
	if n.Sign() < 0 {
		sign = "-"
		n = new(big.Int).Abs(n)
	}
	digits := n.String()
	if scale > 0 {
		if len(digits) <= scale {
			digits = strings.Repeat("0", scale-len(digits)+1) + digits
		}
		digits = digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
	}
	return sign + digits
}

func canonicalListStrings(values *array.List, row int) (string, error) {
	if values.IsNull(row) {
		return "null", nil
	}
	child := values.ListValues().(*array.String)
	start, end := values.ValueOffsets(row)
	parts := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		if child.IsNull(int(i)) {
			parts = append(parts, "null")
		} else {
			parts = append(parts, hexString(child.Value(int(i))))
		}
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}

func canonicalListInts(values *array.List, row int) (string, error) {
	if values.IsNull(row) {
		return "null", nil
	}
	child := values.ListValues().(*array.Int32)
	start, end := values.ValueOffsets(row)
	parts := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		if child.IsNull(int(i)) {
			parts = append(parts, "null")
		} else {
			parts = append(parts, fmt.Sprintf("i:%d", child.Value(int(i))))
		}
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}

func canonicalRow(record arrow.RecordBatch, row int) (string, map[string]string, error) {
	values := make(map[string]string, len(topLevelSpecs))
	rowKey := record.Column(0).(*array.String)
	if rowKey.IsNull(row) {
		return "", nil, fmt.Errorf("row %d has null row_key", row)
	}
	key := rowKey.Value(row)
	values["row_key"] = hexString(key)

	id := record.Column(1).(*array.Int64)
	if id.IsNull(row) {
		values["id"] = "null"
	} else {
		values["id"] = fmt.Sprintf("i:%d", id.Value(row))
	}

	label := record.Column(2).(*array.String)
	if label.IsNull(row) {
		values["label"] = "null"
	} else {
		values["label"] = hexString(label.Value(row))
	}

	payload := record.Column(3).(*array.Binary)
	if payload.IsNull(row) {
		values["payload"] = "null"
	} else {
		values["payload"] = "b:" + hex.EncodeToString(payload.Value(row))
	}

	tagsValue, err := canonicalListStrings(record.Column(4).(*array.List), row)
	if err != nil {
		return "", nil, err
	}
	values["tags"] = tagsValue

	profile := record.Column(5).(*array.Struct)
	if profile.IsNull(row) {
		values["profile"] = "null"
	} else {
		name := profile.Field(0).(*array.String)
		nameValue := "null"
		if !name.IsNull(row) {
			nameValue = hexString(name.Value(row))
		}
		score := profile.Field(1).(*array.Int64)
		scoreValue := "null"
		if !score.IsNull(row) {
			scoreValue = fmt.Sprintf("i:%d", score.Value(row))
		}
		attrs, err := canonicalListInts(profile.Field(2).(*array.List), row)
		if err != nil {
			return "", nil, err
		}
		values["profile"] = fmt.Sprintf("{name=%s,score=%s,attrs=%s}", nameValue, scoreValue, attrs)
	}

	embedding := record.Column(6).(*array.FixedSizeList)
	if embedding.IsNull(row) {
		values["embedding"] = "null"
	} else {
		child := embedding.ListValues().(*array.Float32)
		start, end := embedding.ValueOffsets(row)
		parts := make([]string, 0, end-start)
		for i := start; i < end; i++ {
			parts = append(parts, fmt.Sprintf("f:%08x", math.Float32bits(child.Value(int(i)))))
		}
		values["embedding"] = "[" + strings.Join(parts, ",") + "]"
	}

	amount := record.Column(7).(*array.Decimal128)
	if amount.IsNull(row) {
		values["amount"] = "null"
	} else {
		values["amount"] = "d:" + formatDecimal(amount.Value(row), 9)
	}

	tsUS := record.Column(8).(*array.Timestamp)
	if tsUS.IsNull(row) {
		values["ts_us"] = "null"
	} else {
		values["ts_us"] = fmt.Sprintf("t:%d", int64(tsUS.Value(row)))
	}
	tsNS := record.Column(9).(*array.Timestamp)
	if tsNS.IsNull(row) {
		values["ts_ns"] = "null"
	} else {
		values["ts_ns"] = fmt.Sprintf("t:%d", int64(tsNS.Value(row)))
	}
	return key, values, nil
}

type report struct {
	version uint64
	rows    int
	values  map[string]map[string]string
}

func schemaReportLines() []string {
	lines := []string{
		"schema|fixture|" + hex.EncodeToString([]byte("phase5-v1")),
		"schema|owner|" + hex.EncodeToString([]byte("interop")),
	}
	for _, spec := range topLevelSpecs {
		lines = append(lines, fieldReportLine(spec))
	}
	for _, spec := range nestedSpecs {
		lines = append(lines, fieldReportLine(spec))
	}
	return lines
}

func fieldReportLine(spec fieldSpec) string {
	line := fmt.Sprintf("field|%s|%s|%s|%t|", spec.path, spec.name, spec.typeName, spec.nullable)
	keys := make([]string, 0, len(spec.metadata))
	for key := range spec.metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		parts = append(parts, key, hex.EncodeToString([]byte(spec.metadata[key])))
	}
	line += strings.Join(parts, "|")
	return line
}

func canonicalReport(data report) string {
	keys := make([]string, 0, len(data.values))
	for key := range data.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := schemaReportLines()
	for _, key := range keys {
		for _, spec := range append([]fieldSpec{{path: "0", name: "row_key", typeName: "utf8", nullable: false}}, topLevelSpecs[1:]...) {
			lines = append(lines, fmt.Sprintf("value|%s|%s|%s|%s", key, spec.name, spec.typeName, data.values[key][spec.name]))
		}
	}
	lines = append(lines, fmt.Sprintf("rows|%d", data.rows), fmt.Sprintf("version|%d", data.version))
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func readCanonical(ctx context.Context, path string, version uint64, wantRows int) (report, error) {
	opts := make([]lance.OpenOption, 0, 1)
	if version != 0 {
		opts = append(opts, lance.WithVersion(version))
	}
	ds, err := lance.Open(ctx, path, opts...)
	if err != nil {
		return report{}, err
	}
	defer ds.Close()

	schema, err := ds.Schema()
	if err != nil {
		return report{}, err
	}
	if err := validateSchema(schema); err != nil {
		return report{}, err
	}
	actualVersion, err := ds.Version()
	if err != nil {
		return report{}, err
	}
	if version != 0 && actualVersion != version {
		return report{}, fmt.Errorf("opened version %d, want %d", actualVersion, version)
	}
	rows, err := ds.CountRows(ctx)
	if err != nil {
		return report{}, err
	}
	if rows != uint64(wantRows) {
		return report{}, fmt.Errorf("dataset has %d rows, want %d", rows, wantRows)
	}

	scanner, err := ds.Scan()
	if err != nil {
		return report{}, err
	}
	// Scanner.Records closes the scanner when iteration finishes.
	values := make(map[string]map[string]string, wantRows)
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			return report{}, err
		}
		if err := validateSchema(batch.Schema()); err != nil {
			return report{}, fmt.Errorf("batch schema: %w", err)
		}
		for row := 0; row < int(batch.NumRows()); row++ {
			key, rowValues, err := canonicalRow(batch, row)
			if err != nil {
				return report{}, err
			}
			if _, exists := values[key]; exists {
				return report{}, fmt.Errorf("duplicate row_key %q", key)
			}
			values[key] = rowValues
		}
	}
	if len(values) != wantRows {
		return report{}, fmt.Errorf("read %d rows, want %d", len(values), wantRows)
	}
	return report{version: actualVersion, rows: len(values), values: values}, nil
}

func ensureParent(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

func produce(ctx context.Context, path, format string) error {
	if err := ensureParent(path); err != nil {
		return err
	}
	mem := memory.NewGoAllocator()
	record, err := baseRecord(mem)
	if err != nil {
		return err
	}
	defer record.Release()
	if err := writeRecord(ctx, path, record, "create", format); err != nil {
		return err
	}
	data, err := readCanonical(ctx, path, 1, baseRows)
	if err != nil {
		return err
	}
	fmt.Print(canonicalReport(data))
	return nil
}

func appendFixture(ctx context.Context, path, format string) error {
	before, err := readCanonical(ctx, path, 1, baseRows)
	if err != nil {
		return fmt.Errorf("before append: %w", err)
	}
	if before.version != 1 {
		return fmt.Errorf("before append latest version %d, want 1", before.version)
	}
	mem := memory.NewGoAllocator()
	record, err := appendRecord(mem)
	if err != nil {
		return err
	}
	defer record.Release()
	if err := writeRecord(ctx, path, record, "append", format); err != nil {
		return err
	}
	after, err := readCanonical(ctx, path, 2, baseRows+appendRows)
	if err != nil {
		return err
	}
	if after.version != 2 {
		return fmt.Errorf("after append version %d, want 2", after.version)
	}
	keysBefore := sortedKeys(before.values)
	keysAfter := sortedKeys(after.values)
	lines := []string{
		fmt.Sprintf("append|before_version|%d", before.version),
		fmt.Sprintf("append|after_version|%d", after.version),
		fmt.Sprintf("append|v1_rows|%d", before.rows),
		fmt.Sprintf("append|v2_rows|%d", after.rows),
		"append|v1_keys|" + strings.Join(keysBefore, ","),
		"append|v2_keys|" + strings.Join(keysAfter, ","),
	}
	sort.Strings(lines)
	fmt.Print(strings.Join(lines, "\n") + "\n")
	return nil
}

func sortedKeys(values map[string]map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: lance-compat-go produce|read|append --dataset PATH [--version N] --storage-version FORMAT")
		os.Exit(2)
	}
	command := os.Args[1]
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	dataset := flags.String("dataset", "", "dataset path")
	version := flags.Uint64("version", 0, "dataset version (read)")
	format := flags.String("storage-version", "2.1", "Lance data storage version")
	if err := flags.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	if *dataset == "" {
		fmt.Fprintln(os.Stderr, "--dataset is required")
		os.Exit(2)
	}
	ctx := context.Background()
	var err error
	switch command {
	case "produce":
		err = produce(ctx, *dataset, *format)
	case "append":
		err = appendFixture(ctx, *dataset, *format)
	case "read":
		wantRows := baseRows
		if *version >= 2 {
			wantRows = baseRows + appendRows
		}
		var data report
		data, err = readCanonical(ctx, *dataset, *version, wantRows)
		if err == nil {
			fmt.Print(canonicalReport(data))
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", command)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
