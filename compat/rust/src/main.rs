//! Rust producer/consumer for the Phase 5 Lance compatibility fixture.
//!
//! Writes and reads datasets through lance's own Dataset API. The canonical
//! report is derived from literals in this file and the Arrow schema, never
//! from another language's dataset or IPC file.

use std::collections::{BTreeMap, HashMap};
use std::error::Error;
use std::sync::Arc;

use arrow_array::builder::{
    BinaryBuilder, Decimal128Builder, FixedSizeListBuilder, Float32Builder, Int32Builder,
    Int64Builder, ListBuilder, StringBuilder, StructBuilder, TimestampMicrosecondBuilder,
    TimestampNanosecondBuilder,
};
use arrow_array::{
    Array, ArrayRef, BinaryArray, Decimal128Array, FixedSizeListArray, Float32Array, Int32Array,
    Int64Array, ListArray, RecordBatch, RecordBatchIterator, StringArray, StructArray,
    TimestampMicrosecondArray, TimestampNanosecondArray,
};
use arrow_schema::{DataType, Field, Fields, Schema, TimeUnit};
use lance::dataset::{Dataset, WriteMode, WriteParams};
use lance_file::version::LanceFileVersion;

const BASE_ROWS: usize = 4;
const APPEND_ROWS: usize = 2;
const FORMAT_DEFAULT: &str = "2.1";

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 2 {
        eprintln!("usage: lance-compat-rust produce|read|append --dataset PATH [--version N] --storage-version FORMAT");
        std::process::exit(2);
    }
    let command = &args[1];
    let mut dataset: Option<String> = None;
    let mut version: u64 = 0;
    let mut storage_version = FORMAT_DEFAULT.to_string();
    let mut i = 2;
    while i < args.len() {
        match args[i].as_str() {
            "--dataset" => {
                i += 1;
                dataset = Some(args.get(i).ok_or("--dataset requires a value")?.clone());
            }
            "--version" => {
                i += 1;
                version = args.get(i).ok_or("--version requires a value")?.parse()?;
            }
            "--storage-version" => {
                i += 1;
                storage_version = args
                    .get(i)
                    .ok_or("--storage-version requires a value")?
                    .clone();
            }
            other => return Err(format!("unknown argument: {other}").into()),
        }
        i += 1;
    }
    let path = dataset.ok_or("--dataset is required")?;
    match command.as_str() {
        "produce" => produce(&path, &storage_version).await?,
        "append" => append(&path, &storage_version).await?,
        "read" => {
            let selected = if version == 0 { 1 } else { version };
            let want_rows = if selected >= 2 {
                BASE_ROWS + APPEND_ROWS
            } else {
                BASE_ROWS
            };
            let report = read_report(&path, selected, want_rows).await?;
            print!("{}", report.text());
        }
        other => return Err(format!("unknown command: {other}").into()),
    }
    Ok(())
}

fn metadata(pairs: &[(&str, &str)]) -> HashMap<String, String> {
    pairs
        .iter()
        .map(|(key, value)| ((*key).to_string(), (*value).to_string()))
        .collect()
}

fn field(
    name: &str,
    data_type: DataType,
    nullable: bool,
    metadata: HashMap<String, String>,
) -> Field {
    Field::new(name, data_type, nullable).with_metadata(metadata)
}

fn expected_schema() -> Schema {
    let profile_fields = Fields::from(vec![
        field(
            "name",
            DataType::Utf8,
            true,
            metadata(&[("role", "display")]),
        ),
        field("score", DataType::Int64, true, metadata(&[])),
        field(
            "attrs",
            DataType::List(Arc::new(Field::new("item", DataType::Int32, true))),
            true,
            metadata(&[]),
        ),
    ]);
    let fields = Fields::from(vec![
        field(
            "row_key",
            DataType::Utf8,
            false,
            metadata(&[("role", "primary"), ("unit", "key")]),
        ),
        field("id", DataType::Int64, true, metadata(&[])),
        field("label", DataType::Utf8, true, metadata(&[])),
        field("payload", DataType::Binary, true, metadata(&[])),
        field(
            "tags",
            DataType::List(Arc::new(Field::new("item", DataType::Utf8, true))),
            true,
            metadata(&[]),
        ),
        field(
            "profile",
            DataType::Struct(profile_fields),
            true,
            metadata(&[("encoding", "struct")]),
        ),
        field(
            "embedding",
            DataType::FixedSizeList(Arc::new(Field::new("item", DataType::Float32, true)), 4),
            true,
            metadata(&[("dim", "4"), ("metric", "l2")]),
        ),
        field(
            "amount",
            DataType::Decimal128(38, 9),
            true,
            metadata(&[("precision", "38"), ("unit", "currency")]),
        ),
        field(
            "ts_us",
            DataType::Timestamp(TimeUnit::Microsecond, Some("UTC".into())),
            true,
            metadata(&[]),
        ),
        field(
            "ts_ns",
            DataType::Timestamp(TimeUnit::Nanosecond, Some("Asia/Shanghai".into())),
            true,
            metadata(&[]),
        ),
    ]);
    Schema::new_with_metadata(
        fields,
        metadata(&[("fixture", "phase5-v1"), ("owner", "interop")]),
    )
}

fn f32_from_bits(bits: u32) -> f32 {
    f32::from_bits(bits)
}

fn hex(bytes: &[u8]) -> String {
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        out.push_str(&format!("{byte:02x}"));
    }
    out
}

fn string_value(value: Option<&str>) -> String {
    match value {
        Some(value) => format!("s:{}", hex(value.as_bytes())),
        None => "null".to_string(),
    }
}

fn optional_i64(value: Option<i64>) -> String {
    match value {
        Some(value) => format!("i:{value}"),
        None => "null".to_string(),
    }
}

fn optional_timestamp(value: Option<i64>) -> String {
    match value {
        Some(value) => format!("t:{value}"),
        None => "null".to_string(),
    }
}

fn format_decimal(value: i128, scale: u32) -> String {
    let negative = value < 0;
    let digits = value.unsigned_abs().to_string();
    let scale = scale as usize;
    let padded = if digits.len() <= scale {
        format!("{}{}", "0".repeat(scale - digits.len() + 1), digits)
    } else {
        digits
    };
    let split = padded.len() - scale;
    let body = format!("{}.{}", &padded[..split], &padded[split..]);
    if negative {
        format!("-{body}")
    } else {
        body
    }
}

fn canonical_strings(array: &ListArray, row: usize) -> String {
    if array.is_null(row) {
        return "null".to_string();
    }
    let offsets = array.value_offsets();
    let start = offsets[row] as usize;
    let end = offsets[row + 1] as usize;
    let values = array
        .values()
        .as_any()
        .downcast_ref::<StringArray>()
        .expect("tags child is utf8");
    let items: Vec<String> = (start..end)
        .map(|index| {
            if values.is_null(index) {
                "null".to_string()
            } else {
                format!("s:{}", hex(values.value(index).as_bytes()))
            }
        })
        .collect();
    format!("[{}]", items.join(","))
}

fn canonical_ints(array: &ListArray, row: usize) -> String {
    if array.is_null(row) {
        return "null".to_string();
    }
    let offsets = array.value_offsets();
    let start = offsets[row] as usize;
    let end = offsets[row + 1] as usize;
    let values = array
        .values()
        .as_any()
        .downcast_ref::<Int32Array>()
        .expect("attrs child is int32");
    let items: Vec<String> = (start..end)
        .map(|index| {
            if values.is_null(index) {
                "null".to_string()
            } else {
                format!("i:{}", values.value(index))
            }
        })
        .collect();
    format!("[{}]", items.join(","))
}

fn canonical_struct(array: &StructArray, row: usize) -> String {
    if array.is_null(row) {
        return "null".to_string();
    }
    let names = array
        .column(0)
        .as_any()
        .downcast_ref::<StringArray>()
        .expect("profile.name is utf8");
    let scores = array
        .column(1)
        .as_any()
        .downcast_ref::<Int64Array>()
        .expect("profile.score is int64");
    let attrs = array
        .column(2)
        .as_any()
        .downcast_ref::<ListArray>()
        .expect("profile.attrs is list");
    let name = if names.is_null(row) {
        "null".to_string()
    } else {
        format!("s:{}", hex(names.value(row).as_bytes()))
    };
    let score = optional_i64(if scores.is_null(row) {
        None
    } else {
        Some(scores.value(row))
    });
    format!(
        "{{name={name},score={score},attrs={}}}",
        canonical_ints(attrs, row)
    )
}

fn canonical_floats(array: &FixedSizeListArray, row: usize) -> String {
    if array.is_null(row) {
        return "null".to_string();
    }
    let values = array
        .values()
        .as_any()
        .downcast_ref::<Float32Array>()
        .expect("embedding child is float32");
    let start = row * 4;
    let items: Vec<String> = (start..start + 4)
        .map(|index| format!("f:{:08x}", values.value(index).to_bits()))
        .collect();
    format!("[{}]", items.join(","))
}

fn canonical_row(
    batch: &RecordBatch,
    row: usize,
    values: &mut BTreeMap<String, BTreeMap<String, String>>,
) -> Result<(), Box<dyn Error>> {
    let keys = batch
        .column_by_name("row_key")
        .ok_or("missing row_key")?
        .as_any()
        .downcast_ref::<StringArray>()
        .ok_or("row_key is not utf8")?;
    if keys.is_null(row) {
        return Err(format!("row {row} has null row_key").into());
    }
    let key = keys.value(row).to_string();
    if values.contains_key(&key) {
        return Err(format!("duplicate row_key {key:?}").into());
    }
    let mut row_values = BTreeMap::new();
    row_values.insert("row_key".to_string(), format!("s:{}", hex(key.as_bytes())));
    let ids = batch
        .column_by_name("id")
        .ok_or("missing id")?
        .as_any()
        .downcast_ref::<Int64Array>()
        .ok_or("id is not int64")?;
    row_values.insert(
        "id".to_string(),
        optional_i64(if ids.is_null(row) {
            None
        } else {
            Some(ids.value(row))
        }),
    );
    let labels = batch
        .column_by_name("label")
        .ok_or("missing label")?
        .as_any()
        .downcast_ref::<StringArray>()
        .ok_or("label is not utf8")?;
    row_values.insert(
        "label".to_string(),
        string_value(if labels.is_null(row) {
            None
        } else {
            Some(labels.value(row))
        }),
    );
    let payloads = batch
        .column_by_name("payload")
        .ok_or("missing payload")?
        .as_any()
        .downcast_ref::<BinaryArray>()
        .ok_or("payload is not binary")?;
    row_values.insert(
        "payload".to_string(),
        if payloads.is_null(row) {
            "null".to_string()
        } else {
            format!("b:{}", hex(payloads.value(row)))
        },
    );
    let tags = batch
        .column_by_name("tags")
        .ok_or("missing tags")?
        .as_any()
        .downcast_ref::<ListArray>()
        .ok_or("tags is not list")?;
    row_values.insert("tags".to_string(), canonical_strings(tags, row));
    let profile = batch
        .column_by_name("profile")
        .ok_or("missing profile")?
        .as_any()
        .downcast_ref::<StructArray>()
        .ok_or("profile is not struct")?;
    row_values.insert("profile".to_string(), canonical_struct(profile, row));
    let embedding = batch
        .column_by_name("embedding")
        .ok_or("missing embedding")?
        .as_any()
        .downcast_ref::<FixedSizeListArray>()
        .ok_or("embedding is not fixed-size list")?;
    row_values.insert("embedding".to_string(), canonical_floats(embedding, row));
    let amounts = batch
        .column_by_name("amount")
        .ok_or("missing amount")?
        .as_any()
        .downcast_ref::<Decimal128Array>()
        .ok_or("amount is not decimal128")?;
    row_values.insert(
        "amount".to_string(),
        if amounts.is_null(row) {
            "null".to_string()
        } else {
            format!("d:{}", format_decimal(amounts.value(row), 9))
        },
    );
    for (name, timestamp_array) in [
        (
            "ts_us",
            batch
                .column_by_name("ts_us")
                .ok_or("missing ts_us")?
                .as_any()
                .downcast_ref::<TimestampMicrosecondArray>()
                .ok_or("ts_us is not timestamp[us]")?,
        ),
        (
            "ts_ns",
            batch
                .column_by_name("ts_ns")
                .ok_or("missing ts_ns")?
                .as_any()
                .downcast_ref::<TimestampNanosecondArray>()
                .ok_or("ts_ns is not timestamp[ns]")?,
        ),
    ] {
        let value = if timestamp_array.is_null(row) {
            None
        } else {
            Some(timestamp_array.value(row))
        };
        row_values.insert(name.to_string(), optional_timestamp(value));
    }
    values.insert(key, row_values);
    Ok(())
}

fn validate_metadata(
    actual: &HashMap<String, String>,
    expected: &HashMap<String, String>,
    where_: &str,
) -> Result<(), Box<dyn Error>> {
    if actual != expected {
        return Err(format!("{where_}: metadata {actual:?}, want {expected:?}").into());
    }
    Ok(())
}

fn validate_field(
    actual: &Field,
    name: &str,
    data_type: &DataType,
    nullable: bool,
    expected_metadata: &HashMap<String, String>,
) -> Result<(), Box<dyn Error>> {
    if actual.name() != name {
        return Err(format!("field name {}, want {name}", actual.name()).into());
    }
    if actual.data_type() != data_type {
        return Err(format!("{name}: type {}, want {data_type}", actual.data_type()).into());
    }
    if actual.is_nullable() != nullable {
        return Err(format!("{name}: nullable {}, want {nullable}", actual.is_nullable()).into());
    }
    validate_metadata(actual.metadata(), expected_metadata, name)
}

fn validate_schema(schema: &Schema) -> Result<(), Box<dyn Error>> {
    validate_metadata(
        schema.metadata(),
        &metadata(&[("fixture", "phase5-v1"), ("owner", "interop")]),
        "schema",
    )?;
    let fields = schema.fields();
    if fields.len() != 10 {
        return Err(format!("field count {}, want 10", fields.len()).into());
    }
    validate_field(
        &fields[0],
        "row_key",
        &DataType::Utf8,
        false,
        &metadata(&[("role", "primary"), ("unit", "key")]),
    )?;
    validate_field(&fields[1], "id", &DataType::Int64, true, &metadata(&[]))?;
    validate_field(&fields[2], "label", &DataType::Utf8, true, &metadata(&[]))?;
    validate_field(
        &fields[3],
        "payload",
        &DataType::Binary,
        true,
        &metadata(&[]),
    )?;
    validate_field(
        &fields[4],
        "tags",
        &DataType::List(Arc::new(Field::new("item", DataType::Utf8, true))),
        true,
        &metadata(&[]),
    )?;
    validate_field(
        &fields[5],
        "profile",
        &DataType::Struct(Fields::from(vec![
            field(
                "name",
                DataType::Utf8,
                true,
                metadata(&[("role", "display")]),
            ),
            field("score", DataType::Int64, true, metadata(&[])),
            field(
                "attrs",
                DataType::List(Arc::new(Field::new("item", DataType::Int32, true))),
                true,
                metadata(&[]),
            ),
        ])),
        true,
        &metadata(&[("encoding", "struct")]),
    )?;
    validate_field(
        &fields[6],
        "embedding",
        &DataType::FixedSizeList(Arc::new(Field::new("item", DataType::Float32, true)), 4),
        true,
        &metadata(&[("dim", "4"), ("metric", "l2")]),
    )?;
    validate_field(
        &fields[7],
        "amount",
        &DataType::Decimal128(38, 9),
        true,
        &metadata(&[("precision", "38"), ("unit", "currency")]),
    )?;
    validate_field(
        &fields[8],
        "ts_us",
        &DataType::Timestamp(TimeUnit::Microsecond, Some("UTC".into())),
        true,
        &metadata(&[]),
    )?;
    validate_field(
        &fields[9],
        "ts_ns",
        &DataType::Timestamp(TimeUnit::Nanosecond, Some("Asia/Shanghai".into())),
        true,
        &metadata(&[]),
    )?;
    let tags = fields[4].data_type();
    let DataType::List(tag_item) = tags else {
        return Err("tags is not a list".into());
    };
    if tag_item.name() != "item"
        || !tag_item.is_nullable()
        || *tag_item.data_type() != DataType::Utf8
    {
        return Err("tags item is not nullable utf8".into());
    }
    let DataType::Struct(profile_fields) = fields[5].data_type() else {
        return Err("profile is not a struct".into());
    };
    if profile_fields.len() != 3 {
        return Err(format!("profile child count {}, want 3", profile_fields.len()).into());
    }
    validate_field(
        &profile_fields[0],
        "name",
        &DataType::Utf8,
        true,
        &metadata(&[("role", "display")]),
    )?;
    validate_field(
        &profile_fields[1],
        "score",
        &DataType::Int64,
        true,
        &metadata(&[]),
    )?;
    validate_field(
        &profile_fields[2],
        "attrs",
        &DataType::List(Arc::new(Field::new("item", DataType::Int32, true))),
        true,
        &metadata(&[]),
    )?;
    let DataType::List(attrs_item) = profile_fields[2].data_type() else {
        return Err("profile.attrs is not a list".into());
    };
    if attrs_item.name() != "item"
        || !attrs_item.is_nullable()
        || *attrs_item.data_type() != DataType::Int32
    {
        return Err("profile.attrs item is not nullable int32".into());
    }
    let DataType::FixedSizeList(embedding_item, size) = fields[6].data_type() else {
        return Err("embedding is not a fixed-size list".into());
    };
    if *size != 4
        || embedding_item.name() != "item"
        || !embedding_item.is_nullable()
        || *embedding_item.data_type() != DataType::Float32
    {
        return Err("embedding item is not nullable float32 with length 4".into());
    }
    Ok(())
}

fn report_lines(
    values: &BTreeMap<String, BTreeMap<String, String>>,
    version: u64,
    rows: usize,
) -> String {
    let mut lines = vec![
        format!("schema|fixture|{}", hex("phase5-v1".as_bytes())),
        format!("schema|owner|{}", hex("interop".as_bytes())),
    ];
    let specs = [
        (
            "0",
            "row_key",
            "utf8",
            false,
            vec![("role", "primary"), ("unit", "key")],
        ),
        ("1", "id", "int64", true, vec![]),
        ("2", "label", "utf8", true, vec![]),
        ("3", "payload", "binary", true, vec![]),
        ("4", "tags", "list<utf8>", true, vec![]),
        ("5", "profile", "struct", true, vec![("encoding", "struct")]),
        (
            "5.0",
            "profile.name",
            "utf8",
            true,
            vec![("role", "display")],
        ),
        ("5.1", "profile.score", "int64", true, vec![]),
        ("5.2", "profile.attrs", "list<int32>", true, vec![]),
        (
            "6",
            "embedding",
            "fixed_size_list<float32,4>",
            true,
            vec![("dim", "4"), ("metric", "l2")],
        ),
        (
            "7",
            "amount",
            "decimal128(38,9)",
            true,
            vec![("precision", "38"), ("unit", "currency")],
        ),
        ("8", "ts_us", "timestamp<us,UTC>", true, vec![]),
        ("9", "ts_ns", "timestamp<ns,Asia/Shanghai>", true, vec![]),
    ];
    for (index, name, data_type, nullable, metadata) in specs {
        let mut line = format!("field|{index}|{name}|{data_type}|{nullable}|");
        let mut metadata = metadata;
        metadata.sort();
        let parts: Vec<String> = metadata
            .into_iter()
            .map(|(key, value)| format!("{key}|{}", hex(value.as_bytes())))
            .collect();
        line.push_str(&parts.join("|"));
        lines.push(line);
    }
    let columns = [
        ("row_key", "utf8"),
        ("id", "int64"),
        ("label", "utf8"),
        ("payload", "binary"),
        ("tags", "list<utf8>"),
        ("profile", "struct"),
        ("embedding", "fixed_size_list<float32,4>"),
        ("amount", "decimal128(38,9)"),
        ("ts_us", "timestamp<us,UTC>"),
        ("ts_ns", "timestamp<ns,Asia/Shanghai>"),
    ];
    for (key, row_values) in values {
        for (name, data_type) in columns {
            lines.push(format!(
                "value|{key}|{name}|{data_type}|{}",
                row_values.get(name).expect("canonical column")
            ));
        }
    }
    lines.push(format!("rows|{rows}"));
    lines.push(format!("version|{version}"));
    lines.sort();
    format!("{}\n", lines.join("\n"))
}

struct Report {
    version: u64,
    rows: usize,
    values: BTreeMap<String, BTreeMap<String, String>>,
}

impl Report {
    fn text(&self) -> String {
        report_lines(&self.values, self.version, self.rows)
    }
}

async fn read_report(path: &str, version: u64, want_rows: usize) -> Result<Report, Box<dyn Error>> {
    let mut dataset = Dataset::open(path).await?;
    if version > 0 {
        dataset = dataset.checkout_version(version).await?;
    }
    if dataset.version_id() != version {
        return Err(format!("opened version {}, want {version}", dataset.version_id()).into());
    }
    validate_schema(dataset.schema())?;
    let count = dataset.count_rows(None).await?;
    if count != want_rows {
        return Err(format!("dataset has {count} rows, want {want_rows}").into());
    }
    let batch = dataset.scan().try_into_batch().await?;
    validate_schema(batch.schema().as_ref())?;
    let mut values = BTreeMap::new();
    for row in 0..batch.num_rows() {
        canonical_row(&batch, row, &mut values)?;
    }
    if values.len() != want_rows {
        return Err(format!("read {} rows, want {want_rows}", values.len()).into());
    }
    Ok(Report {
        version,
        rows: values.len(),
        values,
    })
}

fn base_batch() -> Result<RecordBatch, Box<dyn Error>> {
    let schema = Arc::new(expected_schema());
    let mut keys = StringBuilder::new();
    for value in ["r1", "r2", "r3", "r4"] {
        keys.append_value(value);
    }
    let keys = Arc::new(keys.finish()) as ArrayRef;

    let mut ids = Int64Builder::new();
    ids.append_value(9_007_199_254_740_993);
    ids.append_value(-9_007_199_254_740_993);
    ids.append_null();
    ids.append_value(9_007_199_254_740_992);
    let ids = Arc::new(ids.finish()) as ArrayRef;

    let mut labels = StringBuilder::new();
    labels.append_value("alpha");
    labels.append_value("");
    labels.append_null();
    labels.append_value("café 世界");
    let labels = Arc::new(labels.finish()) as ArrayRef;

    let mut payloads = BinaryBuilder::new();
    payloads.append_value([0x00, 0xff, 0x10]);
    payloads.append_value([]);
    payloads.append_null();
    payloads.append_value([0x80]);
    let payloads = Arc::new(payloads.finish()) as ArrayRef;

    let mut tags = ListBuilder::new(StringBuilder::new());
    tags.append(true);
    tags.values().append_value("a");
    tags.values().append_value("b");
    tags.append(true);
    tags.append_null();
    tags.append(true);
    tags.values().append_value("");
    tags.values().append_value("x");
    let tags = Arc::new(tags.finish()) as ArrayRef;

    let profile_fields = Fields::from(vec![
        field(
            "name",
            DataType::Utf8,
            true,
            metadata(&[("role", "display")]),
        ),
        field("score", DataType::Int64, true, metadata(&[])),
        field(
            "attrs",
            DataType::List(Arc::new(Field::new("item", DataType::Int32, true))),
            true,
            metadata(&[]),
        ),
    ]);
    let mut profiles = StructBuilder::new(
        profile_fields.clone(),
        vec![
            Box::new(StringBuilder::new()),
            Box::new(Int64Builder::new()),
            Box::new(ListBuilder::new(Int32Builder::new())),
        ],
    );
    profiles.append(true);
    profiles
        .field_builder::<StringBuilder>(0)
        .unwrap()
        .append_value("Ada");
    profiles
        .field_builder::<Int64Builder>(1)
        .unwrap()
        .append_value(9_007_199_254_740_993);
    let attrs = profiles
        .field_builder::<ListBuilder<Int32Builder>>(2)
        .unwrap();
    attrs.append(true);
    attrs.values().append_value(1);
    attrs.values().append_value(2);
    profiles.append(true);
    profiles
        .field_builder::<StringBuilder>(0)
        .unwrap()
        .append_null();
    profiles
        .field_builder::<Int64Builder>(1)
        .unwrap()
        .append_null();
    profiles
        .field_builder::<ListBuilder<Int32Builder>>(2)
        .unwrap()
        .append(true);
    profiles.append_null();
    profiles
        .field_builder::<StringBuilder>(0)
        .unwrap()
        .append_null();
    profiles
        .field_builder::<Int64Builder>(1)
        .unwrap()
        .append_null();
    profiles
        .field_builder::<ListBuilder<Int32Builder>>(2)
        .unwrap()
        .append_null();
    profiles.append(true);
    profiles
        .field_builder::<StringBuilder>(0)
        .unwrap()
        .append_value("");
    profiles
        .field_builder::<Int64Builder>(1)
        .unwrap()
        .append_value(0);
    profiles
        .field_builder::<ListBuilder<Int32Builder>>(2)
        .unwrap()
        .append_null();
    let profiles = Arc::new(profiles.finish()) as ArrayRef;

    let mut embeddings = FixedSizeListBuilder::new(Float32Builder::new(), 4);
    for (row, bits) in [
        [0x3DCCCCCD, 0xC0200000, 0x00000000, 0x3F800000],
        [0x00000000; 4],
        [0x80000000, 0x7F7FFFFF, 0x00000001, 0x3F800000],
        [0x3FC00000, 0x80000000, 0x00000001, 0x40400000],
    ]
    .into_iter()
    .enumerate()
    {
        embeddings.append(row != 1);
        for value in bits {
            embeddings.values().append_value(f32_from_bits(value));
        }
    }
    let embeddings = Arc::new(embeddings.finish()) as ArrayRef;

    let mut amounts = Decimal128Builder::new();
    for value in [
        Some(123_456_789_012_345_678_901_234_567_891_234_567_89i128),
        Some(-1),
        Some(0),
        Some(-123_456_789_012_345_678_901_234_567_891_234_567_89i128),
    ] {
        match value {
            Some(value) => amounts.append_value(value),
            None => amounts.append_null(),
        }
    }
    let amounts = Arc::new(amounts.finish().with_precision_and_scale(38, 9)?) as ArrayRef;

    let mut ts_us = TimestampMicrosecondBuilder::new().with_timezone("UTC");
    for value in [
        Some(1_704_164_645_123_456),
        Some(0),
        None,
        Some(1_704_164_645_123_456),
    ] {
        match value {
            Some(value) => ts_us.append_value(value),
            None => ts_us.append_null(),
        }
    }
    let ts_us = Arc::new(ts_us.finish()) as ArrayRef;

    let mut ts_ns = TimestampNanosecondBuilder::new().with_timezone("Asia/Shanghai");
    for value in [
        Some(1_704_164_645_123_456_789),
        Some(-1),
        None,
        Some(1_704_164_645_000_000_000),
    ] {
        match value {
            Some(value) => ts_ns.append_value(value),
            None => ts_ns.append_null(),
        }
    }
    let ts_ns = Arc::new(ts_ns.finish()) as ArrayRef;

    Ok(RecordBatch::try_new(
        schema,
        vec![
            keys, ids, labels, payloads, tags, profiles, embeddings, amounts, ts_us, ts_ns,
        ],
    )?)
}

fn append_batch() -> Result<RecordBatch, Box<dyn Error>> {
    let schema = Arc::new(expected_schema());
    let keys = Arc::new({
        let mut builder = StringBuilder::new();
        builder.append_value("x1");
        builder.append_value("x2");
        builder.finish()
    }) as ArrayRef;
    let ids = Arc::new({
        let mut builder = Int64Builder::new();
        builder.append_value(9_007_199_254_740_995);
        builder.append_null();
        builder.finish()
    }) as ArrayRef;
    let labels = Arc::new({
        let mut builder = StringBuilder::new();
        builder.append_value("append-1");
        builder.append_null();
        builder.finish()
    }) as ArrayRef;
    let payloads = Arc::new({
        let mut builder = BinaryBuilder::new();
        builder.append_value([0xde, 0xad]);
        builder.append_null();
        builder.finish()
    }) as ArrayRef;
    let tags = Arc::new({
        let mut builder = ListBuilder::new(StringBuilder::new());
        builder.append(true);
        builder.values().append_value("x1");
        builder.append_null();
        builder.finish()
    }) as ArrayRef;
    let profile_fields = Fields::from(vec![
        field(
            "name",
            DataType::Utf8,
            true,
            metadata(&[("role", "display")]),
        ),
        field("score", DataType::Int64, true, metadata(&[])),
        field(
            "attrs",
            DataType::List(Arc::new(Field::new("item", DataType::Int32, true))),
            true,
            metadata(&[]),
        ),
    ]);
    let profiles = Arc::new({
        let mut builder = StructBuilder::new(
            profile_fields,
            vec![
                Box::new(StringBuilder::new()),
                Box::new(Int64Builder::new()),
                Box::new(ListBuilder::new(Int32Builder::new())),
            ],
        );
        builder.append(true);
        builder
            .field_builder::<StringBuilder>(0)
            .unwrap()
            .append_value("App");
        builder
            .field_builder::<Int64Builder>(1)
            .unwrap()
            .append_value(1);
        let attrs = builder
            .field_builder::<ListBuilder<Int32Builder>>(2)
            .unwrap();
        attrs.append(true);
        attrs.values().append_value(3);
        builder.append_null();
        builder
            .field_builder::<StringBuilder>(0)
            .unwrap()
            .append_null();
        builder
            .field_builder::<Int64Builder>(1)
            .unwrap()
            .append_null();
        builder
            .field_builder::<ListBuilder<Int32Builder>>(2)
            .unwrap()
            .append_null();
        builder.finish()
    }) as ArrayRef;
    let embeddings = Arc::new({
        let mut builder = FixedSizeListBuilder::new(Float32Builder::new(), 4);
        builder.append(true);
        for _ in 0..4 {
            builder.values().append_value(1.0);
        }
        builder.append(false);
        for _ in 0..4 {
            builder.values().append_value(0.0);
        }
        builder.finish()
    }) as ArrayRef;
    let amounts = Arc::new({
        let mut builder = Decimal128Builder::new();
        builder.append_value(1_000_000_000i128);
        builder.append_null();
        builder.finish().with_precision_and_scale(38, 9)?
    }) as ArrayRef;
    let ts_us = Arc::new({
        let mut builder = TimestampMicrosecondBuilder::new().with_timezone("UTC");
        builder.append_value(1);
        builder.append_null();
        builder.finish()
    }) as ArrayRef;
    let ts_ns = Arc::new({
        let mut builder = TimestampNanosecondBuilder::new().with_timezone("Asia/Shanghai");
        builder.append_value(1);
        builder.append_null();
        builder.finish()
    }) as ArrayRef;
    Ok(RecordBatch::try_new(
        schema,
        vec![
            keys, ids, labels, payloads, tags, profiles, embeddings, amounts, ts_us, ts_ns,
        ],
    )?)
}

async fn write(
    path: &str,
    batch: RecordBatch,
    mode: WriteMode,
    storage_version: &str,
) -> Result<(), Box<dyn Error>> {
    let schema = batch.schema();
    let version: LanceFileVersion = storage_version.parse()?;
    let params = WriteParams {
        mode,
        data_storage_version: Some(version),
        ..Default::default()
    };
    let batches: Vec<Result<RecordBatch, arrow_schema::ArrowError>> = vec![Ok(batch)];
    let reader = RecordBatchIterator::new(batches, schema);
    let dataset = Dataset::write(reader, path, Some(params)).await?;
    drop(dataset);
    Ok(())
}

async fn produce(path: &str, storage_version: &str) -> Result<(), Box<dyn Error>> {
    if let Some(parent) = std::path::Path::new(path).parent() {
        if !parent.as_os_str().is_empty() {
            std::fs::create_dir_all(parent)?;
        }
    }
    write(path, base_batch()?, WriteMode::Create, storage_version).await?;
    let report = read_report(path, 1, BASE_ROWS).await?;
    print!("{}", report.text());
    Ok(())
}

async fn append(path: &str, storage_version: &str) -> Result<(), Box<dyn Error>> {
    let before = read_report(path, 1, BASE_ROWS).await?;
    if before.version != 1 {
        return Err(format!("before append version {}, want 1", before.version).into());
    }
    let latest = Dataset::open(path).await?;
    if latest.version_id() != 1 {
        return Err(format!(
            "before append latest version {}, want 1",
            latest.version_id()
        )
        .into());
    }
    write(path, append_batch()?, WriteMode::Append, storage_version).await?;
    let after = read_report(path, 2, BASE_ROWS + APPEND_ROWS).await?;
    if after.version != 2 {
        return Err(format!("after append version {}, want 2", after.version).into());
    }
    let mut lines = vec![
        format!("append|before_version|{}", before.version),
        format!("append|after_version|{}", after.version),
        format!("append|v1_rows|{}", before.rows),
        format!("append|v2_rows|{}", after.rows),
        format!(
            "append|v1_keys|{}",
            before.values.keys().cloned().collect::<Vec<_>>().join(",")
        ),
        format!(
            "append|v2_keys|{}",
            after.values.keys().cloned().collect::<Vec<_>>().join(",")
        ),
    ];
    lines.sort();
    println!("{}", lines.join("\n"));
    Ok(())
}
