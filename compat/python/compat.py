#!/usr/bin/env python3
"""Python producer/consumer for the Phase 5 Lance compatibility fixture.

The CLI writes the committed fixture with pylance and prints a canonical report
read back through pylance. It intentionally does not use another language's
Lance output as input to a write.
"""

from __future__ import annotations

import argparse
import decimal
import struct
import sys
from pathlib import Path
from typing import Any

import lance
import pyarrow as pa

BASE_ROWS = 4
APPEND_ROWS = 2
DECIMAL = decimal.Decimal


def md(values: dict[str, str]) -> dict[bytes, bytes]:
    return {key.encode(): value.encode() for key, value in values.items()}


def field(name: str, typ: pa.DataType, nullable: bool, metadata: dict[str, str] | None = None) -> pa.Field:
    return pa.field(name, typ, nullable=nullable, metadata=md(metadata or {}))


def schema_field(schema: pa.Schema, name: str) -> pa.Field:
    return schema.field(schema.get_field_index(name))


def expected_schema() -> pa.Schema:
    profile = pa.struct(
        [
            field("name", pa.string(), True, {"role": "display"}),
            field("score", pa.int64(), True),
            field("attrs", pa.list_(pa.int32()), True),
        ]
    )
    return pa.schema(
        [
            field("row_key", pa.string(), False, {"role": "primary", "unit": "key"}),
            field("id", pa.int64(), True),
            field("label", pa.string(), True),
            field("payload", pa.binary(), True),
            field("tags", pa.list_(pa.string()), True),
            field("profile", profile, True, {"encoding": "struct"}),
            field("embedding", pa.list_(pa.float32(), 4), True, {"dim": "4", "metric": "l2"}),
            field("amount", pa.decimal128(38, 9), True, {"precision": "38", "unit": "currency"}),
            field("ts_us", pa.timestamp("us", tz="UTC"), True),
            field("ts_ns", pa.timestamp("ns", tz="Asia/Shanghai"), True),
        ],
        metadata=md({"fixture": "phase5-v1", "owner": "interop"}),
    )


SCHEMA = expected_schema()


def canonical_type(typ: pa.DataType) -> str:
    if pa.types.is_string(typ):
        return "utf8"
    if pa.types.is_int64(typ):
        return "int64"
    if pa.types.is_binary(typ):
        return "binary"
    if pa.types.is_list(typ):
        return f"list<{canonical_type(typ.value_type)}>"
    if pa.types.is_struct(typ):
        return "struct"
    if pa.types.is_fixed_size_list(typ):
        return f"fixed_size_list<{canonical_type(typ.value_type)},{typ.list_size}>"
    if pa.types.is_decimal(typ):
        return f"decimal128({typ.precision},{typ.scale})"
    if pa.types.is_timestamp(typ):
        return f"timestamp<{typ.unit},{typ.tz}>"
    raise AssertionError(f"unexpected type {typ}")


def check_metadata(actual: dict[bytes, bytes] | None, expected: dict[str, str], where: str) -> None:
    want = md(expected)
    if actual is None:
        actual = {}
    if actual != want:
        raise AssertionError(f"{where}: metadata {actual!r}, want {want!r}")


def validate_field(actual: pa.Field, name: str, typ: pa.DataType, nullable: bool, metadata: dict[str, str] | None = None, where: str = "") -> None:
    path = where or name
    if actual.name != name:
        raise AssertionError(f"{path}: name {actual.name!r}, want {name!r}")
    if actual.type != typ:
        raise AssertionError(f"{path}: type {actual.type}, want {typ}")
    if actual.nullable != nullable:
        raise AssertionError(f"{path}: nullable {actual.nullable}, want {nullable}")
    if canonical_type(actual.type) != canonical_type(typ):
        raise AssertionError(f"{path}: canonical type mismatch")
    check_metadata(actual.metadata, metadata or {}, path)


def validate_schema(schema: pa.Schema) -> None:
    if schema is None:
        raise AssertionError("nil schema")
    check_metadata(schema.metadata, {"fixture": "phase5-v1", "owner": "interop"}, "schema")
    if len(schema) != 10:
        raise AssertionError(f"field count {len(schema)}, want 10")
    validate_field(schema_field(schema, "row_key"), "row_key", pa.string(), False, {"role": "primary", "unit": "key"})
    validate_field(schema_field(schema, "id"), "id", pa.int64(), True)
    validate_field(schema_field(schema, "label"), "label", pa.string(), True)
    validate_field(schema_field(schema, "payload"), "payload", pa.binary(), True)
    validate_field(schema_field(schema, "tags"), "tags", pa.list_(pa.string()), True)
    validate_field(schema_field(schema, "profile"), "profile", schema_field(SCHEMA, "profile").type, True, {"encoding": "struct"})
    validate_field(schema_field(schema, "embedding"), "embedding", pa.list_(pa.float32(), 4), True, {"dim": "4", "metric": "l2"})
    validate_field(schema_field(schema, "amount"), "amount", pa.decimal128(38, 9), True, {"precision": "38", "unit": "currency"})
    validate_field(schema_field(schema, "ts_us"), "ts_us", pa.timestamp("us", tz="UTC"), True)
    validate_field(schema_field(schema, "ts_ns"), "ts_ns", pa.timestamp("ns", tz="Asia/Shanghai"), True)

    profile = schema_field(schema, "profile").type
    assert pa.types.is_struct(profile)
    children = [profile.field(i) for i in range(profile.num_fields)]
    if len(children) != 3:
        raise AssertionError(f"profile child count {len(children)}, want 3")
    validate_field(children[0], "name", pa.string(), True, {"role": "display"}, "profile.name")
    validate_field(children[1], "score", pa.int64(), True, None, "profile.score")
    validate_field(children[2], "attrs", pa.list_(pa.int32()), True, None, "profile.attrs")

    tags = schema_field(schema, "tags").type
    assert pa.types.is_list(tags)
    if tags.value_field.name != "item" or not tags.value_field.nullable:
        raise AssertionError("tags element field is not nullable item")
    embedding = schema_field(schema, "embedding").type
    assert pa.types.is_fixed_size_list(embedding)
    if embedding.list_size != 4 or embedding.value_field.name != "item" or not embedding.value_field.nullable:
        raise AssertionError("embedding element field is not nullable item with length 4")


def f32(bits: int) -> float:
    return struct.unpack(">f", struct.pack(">I", bits))[0]


def base_table() -> pa.Table:
    profile_type = schema_field(SCHEMA, "profile").type
    arrays = [
        pa.array(["r1", "r2", "r3", "r4"], type=pa.string()),
        pa.array([9007199254740993, -9007199254740993, None, 9007199254740992], type=pa.int64()),
        pa.array(["alpha", "", None, "café 世界"], type=pa.string()),
        pa.array([b"\x00\xff\x10", b"", None, b"\x80"], type=pa.binary()),
        pa.array([["a", "b"], [], None, ["", "x"]], type=pa.list_(pa.string())),
        pa.array(
            [
                {"name": "Ada", "score": 9007199254740993, "attrs": [1, 2]},
                {"name": None, "score": None, "attrs": []},
                None,
                {"name": "", "score": 0, "attrs": None},
            ],
            type=profile_type,
        ),
        pa.array(
            [
                [f32(0x3DCCCCCD), f32(0xC0200000), f32(0x00000000), f32(0x3F800000)],
                None,
                [f32(0x80000000), f32(0x7F7FFFFF), f32(0x00000001), f32(0x3F800000)],
                [f32(0x3FC00000), f32(0x80000000), f32(0x00000001), f32(0x40400000)],
            ],
            type=pa.list_(pa.float32(), 4),
        ),
        pa.array(
            [
                DECIMAL("12345678901234567890123456789.123456789"),
                DECIMAL("-0.000000001"),
                DECIMAL("0.000000000"),
                DECIMAL("-12345678901234567890123456789.123456789"),
            ],
            type=pa.decimal128(38, 9),
        ),
        pa.array([1704164645123456, 0, None, 1704164645123456], type=pa.timestamp("us", tz="UTC")),
        pa.array([1704164645123456789, -1, None, 1704164645000000000], type=pa.timestamp("ns", tz="Asia/Shanghai")),
    ]
    return pa.Table.from_arrays(arrays, schema=SCHEMA)


def append_table() -> pa.Table:
    profile_type = schema_field(SCHEMA, "profile").type
    arrays = [
        pa.array(["x1", "x2"], type=pa.string()),
        pa.array([9007199254740995, None], type=pa.int64()),
        pa.array(["append-1", None], type=pa.string()),
        pa.array([b"\xde\xad", None], type=pa.binary()),
        pa.array([["x1"], None], type=pa.list_(pa.string())),
        pa.array(
            [
                {"name": "App", "score": 1, "attrs": [3]},
                None,
            ],
            type=profile_type,
        ),
        pa.array(
            [
                [f32(0x3F800000), f32(0x3F800000), f32(0x3F800000), f32(0x3F800000)],
                None,
            ],
            type=pa.list_(pa.float32(), 4),
        ),
        pa.array([DECIMAL("1.000000000"), None], type=pa.decimal128(38, 9)),
        pa.array([1, None], type=pa.timestamp("us", tz="UTC")),
        pa.array([1, None], type=pa.timestamp("ns", tz="Asia/Shanghai")),
    ]
    return pa.Table.from_arrays(arrays, schema=SCHEMA)


def hex_string(value: str) -> str:
    return "s:" + value.encode("utf-8").hex()


def format_decimal(value: decimal.Decimal) -> str:
    return format(value, "f")


def canonical_list_strings(value: Any) -> str:
    if value is None:
        return "null"
    return "[" + ",".join(hex_string(item) if item is not None else "null" for item in value) + "]"


def canonical_list_ints(value: Any) -> str:
    if value is None:
        return "null"
    return "[" + ",".join(f"i:{item}" if item is not None else "null" for item in value) + "]"


def canonical_row(batch: pa.RecordBatch, row: int) -> tuple[str, dict[str, str]]:
    values: dict[str, str] = {}
    row_key = batch["row_key"][row]
    if not row_key.is_valid:
        raise AssertionError(f"row {row} has null row_key")
    key = row_key.as_py()
    values["row_key"] = hex_string(key)

    for name in ["id", "label", "payload", "tags", "profile", "embedding", "amount", "ts_us", "ts_ns"]:
        scalar = batch[name][row]
        if not scalar.is_valid:
            values[name] = "null"
            continue
        if name in ("ts_us", "ts_ns"):
            values[name] = f"t:{scalar.value}"
            continue
        value = scalar.as_py()
        if name == "id":
            values[name] = f"i:{value}"
        elif name in ("label",):
            values[name] = hex_string(value)
        elif name == "payload":
            values[name] = "b:" + value.hex()
        elif name == "tags":
            values[name] = canonical_list_strings(value)
        elif name == "profile":
            name_value = "null" if value["name"] is None else hex_string(value["name"])
            score_value = "null" if value["score"] is None else f"i:{value['score']}"
            values[name] = f"{{name={name_value},score={score_value},attrs={canonical_list_ints(value['attrs'])}}}"
        elif name == "embedding":
            values[name] = "[" + ",".join("f:" + struct.pack(">f", item).hex() for item in value) + "]"
        elif name == "amount":
            values[name] = "d:" + format_decimal(value)
        else:
            raise AssertionError(f"unexpected column {name}")
    return key, values


def report_lines(data: dict[str, dict[str, str]], version: int, rows: int) -> str:
    lines = [
        "schema|fixture|" + "phase5-v1".encode().hex(),
        "schema|owner|" + "interop".encode().hex(),
    ]
    specs = [
        ("0", "row_key", "utf8", False, {"role": "primary", "unit": "key"}),
        ("1", "id", "int64", True, {}),
        ("2", "label", "utf8", True, {}),
        ("3", "payload", "binary", True, {}),
        ("4", "tags", "list<utf8>", True, {}),
        ("5", "profile", "struct", True, {"encoding": "struct"}),
        ("5.0", "profile.name", "utf8", True, {"role": "display"}),
        ("5.1", "profile.score", "int64", True, {}),
        ("5.2", "profile.attrs", "list<int32>", True, {}),
        ("6", "embedding", "fixed_size_list<float32,4>", True, {"dim": "4", "metric": "l2"}),
        ("7", "amount", "decimal128(38,9)", True, {"precision": "38", "unit": "currency"}),
        ("8", "ts_us", "timestamp<us,UTC>", True, {}),
        ("9", "ts_ns", "timestamp<ns,Asia/Shanghai>", True, {}),
    ]
    for index, name, typ, nullable, metadata in specs:
        line = f"field|{index}|{name}|{typ}|{str(nullable).lower()}|"
        line += "|".join(f"{key}|{value.encode().hex()}" for key, value in sorted(metadata.items()))
        lines.append(line)
    for key in sorted(data):
        for name, typ in [
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
        ]:
            lines.append(f"value|{key}|{name}|{typ}|{data[key][name]}")
    lines.extend([f"rows|{rows}", f"version|{version}"])
    return "\n".join(sorted(lines)) + "\n"


def read_report(path: str, version: int, want_rows: int) -> tuple[str, dict[str, dict[str, str]], int, int]:
    ds = lance.dataset(path, version=version)
    validate_schema(ds.schema)
    actual_version = int(ds.version)
    if actual_version != version:
        raise AssertionError(f"opened version {actual_version}, want {version}")
    actual_rows = int(ds.count_rows())
    if actual_rows != want_rows:
        raise AssertionError(f"dataset has {actual_rows} rows, want {want_rows}")
    values: dict[str, dict[str, str]] = {}
    for batch in ds.to_batches():
        validate_schema(batch.schema)
        for row in range(batch.num_rows):
            key, row_values = canonical_row(batch, row)
            if key in values:
                raise AssertionError(f"duplicate row_key {key!r}")
            values[key] = row_values
    if len(values) != want_rows:
        raise AssertionError(f"read {len(values)} rows, want {want_rows}")
    return report_lines(values, actual_version, len(values)), values, actual_version, len(values)


def produce(path: str, storage_version: str) -> None:
    Path(path).parent.mkdir(parents=True, exist_ok=True)
    lance.write_dataset(base_table(), path, mode="create", data_storage_version=storage_version)
    report, _, _, _ = read_report(path, 1, BASE_ROWS)
    print(report, end="")


def append_fixture(path: str, storage_version: str) -> None:
    before_report, before_values, before_version, before_rows = read_report(path, 1, BASE_ROWS)
    if before_version != 1:
        raise AssertionError(f"before append version {before_version}, want 1")
    ds = lance.dataset(path)
    if int(ds.version) != 1:
        raise AssertionError(f"before append latest version {ds.version}, want 1")
    lance.write_dataset(append_table(), path, mode="append", data_storage_version=storage_version)
    after_report, after_values, after_version, after_rows = read_report(path, 2, BASE_ROWS + APPEND_ROWS)
    if after_version != 2:
        raise AssertionError(f"after append version {after_version}, want 2")
    lines = [
        f"append|before_version|{before_version}",
        f"append|after_version|{after_version}",
        f"append|v1_rows|{before_rows}",
        f"append|v2_rows|{after_rows}",
        "append|v1_keys|" + ",".join(sorted(before_values)),
        "append|v2_keys|" + ",".join(sorted(after_values)),
    ]
    print("\n".join(sorted(lines)))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    for name in ("produce", "read", "append"):
        cmd = sub.add_parser(name)
        cmd.add_argument("--dataset", required=True)
        cmd.add_argument("--storage-version", default="2.1")
        cmd.add_argument("--version", type=int, default=0)
    args = parser.parse_args()
    try:
        if args.command == "produce":
            produce(args.dataset, args.storage_version)
        elif args.command == "append":
            append_fixture(args.dataset, args.storage_version)
        else:
            version = args.version if args.version else 1
            want_rows = BASE_ROWS if version == 1 else BASE_ROWS + APPEND_ROWS
            report, _, _, _ = read_report(args.dataset, version, want_rows)
            print(report, end="")
    except Exception as exc:  # noqa: BLE001 - CLI must fail loudly with a useful message.
        print(f"lance-compat-python: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
