package org.lance.compat;

import java.io.IOException;
import java.math.BigDecimal;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;
import org.apache.arrow.memory.BufferAllocator;
import org.apache.arrow.memory.RootAllocator;
import org.apache.arrow.vector.BigIntVector;
import org.apache.arrow.vector.DecimalVector;
import org.apache.arrow.vector.Float4Vector;
import org.apache.arrow.vector.IntVector;
import org.apache.arrow.vector.TimeStampMicroTZVector;
import org.apache.arrow.vector.TimeStampNanoTZVector;
import org.apache.arrow.vector.TimeStampVector;
import org.apache.arrow.vector.VarBinaryVector;
import org.apache.arrow.vector.VarCharVector;
import org.apache.arrow.vector.VectorSchemaRoot;
import org.apache.arrow.vector.complex.FixedSizeListVector;
import org.apache.arrow.vector.complex.ListVector;
import org.apache.arrow.vector.complex.StructVector;
import org.apache.arrow.vector.ipc.ArrowReader;
import org.apache.arrow.vector.types.TimeUnit;
import org.apache.arrow.vector.types.pojo.ArrowType;
import org.apache.arrow.vector.types.pojo.Field;
import org.apache.arrow.vector.types.pojo.FieldType;
import org.apache.arrow.vector.types.pojo.Schema;
import org.lance.Dataset;
import org.lance.WriteParams;
import org.lance.ipc.LanceScanner;
import org.lance.ipc.ScanOptions;

/** Java producer/consumer for the Phase 5 Lance compatibility fixture. */
public final class Main {
  private static final int BASE_ROWS = 4;
  private static final int APPEND_ROWS = 2;
  private static final Map<String, String> SCHEMA_METADATA =
      Map.of("fixture", "phase5-v1", "owner", "interop");

  private Main() {}

  public static void main(String[] args) {
    if (args.length == 0) {
      System.err.println(
          "usage: lance-compat-java produce|read|append --dataset PATH [--version N] --storage-version FORMAT");
      System.exit(2);
    }
    String command = args[0];
    String dataset = null;
    long version = 0;
    String storageVersion = "2.1";
    for (int i = 1; i < args.length; i++) {
      switch (args[i]) {
        case "--dataset":
          dataset = requireValue(args, ++i, "--dataset");
          break;
        case "--version":
          version = Long.parseLong(requireValue(args, ++i, "--version"));
          break;
        case "--storage-version":
          storageVersion = requireValue(args, ++i, "--storage-version");
          break;
        default:
          throw new IllegalArgumentException("unknown argument: " + args[i]);
      }
    }
    if (dataset == null) {
      throw new IllegalArgumentException("--dataset is required");
    }
    try (BufferAllocator allocator = new RootAllocator()) {
      switch (command) {
        case "produce":
          produce(allocator, dataset, storageVersion);
          break;
        case "append":
          append(allocator, dataset, storageVersion);
          break;
        case "read":
          long selected = version == 0 ? 1 : version;
          int wantRows = selected >= 2 ? BASE_ROWS + APPEND_ROWS : BASE_ROWS;
          System.out.print(read(dataset, allocator, selected, wantRows).text());
          break;
        default:
          throw new IllegalArgumentException("unknown command: " + command);
      }
    } catch (Exception e) {
      System.err.println("lance-compat-java: " + e.getMessage());
      e.printStackTrace(System.err);
      System.exit(1);
    }
  }

  private static String requireValue(String[] args, int index, String flag) {
    if (index >= args.length) {
      throw new IllegalArgumentException(flag + " requires a value");
    }
    return args[index];
  }

  private static void produce(BufferAllocator allocator, String path, String storageVersion)
      throws IOException {
    write(allocator, path, baseRoot(allocator), WriteParams.WriteMode.CREATE, storageVersion);
    Report report = read(path, allocator, 1, BASE_ROWS);
    System.out.print(report.text());
  }

  private static void append(BufferAllocator allocator, String path, String storageVersion)
      throws IOException {
    Report before = read(path, allocator, 1, BASE_ROWS);
    if (before.version != 1) {
      throw new IllegalStateException("before append version " + before.version + ", want 1");
    }
    try (Dataset dataset = Dataset.open(path)) {
      if (dataset.version() != 1) {
        throw new IllegalStateException(
            "before append latest version " + dataset.version() + ", want 1");
      }
    }
    write(allocator, path, appendRoot(allocator), WriteParams.WriteMode.APPEND, storageVersion);
    Report after = read(path, allocator, 2, BASE_ROWS + APPEND_ROWS);
    if (after.version != 2) {
      throw new IllegalStateException("after append version " + after.version + ", want 2");
    }
    List<String> lines = new ArrayList<>();
    lines.add("append|before_version|" + before.version);
    lines.add("append|after_version|" + after.version);
    lines.add("append|v1_rows|" + before.rows);
    lines.add("append|v2_rows|" + after.rows);
    lines.add("append|v1_keys|" + String.join(",", before.values.keySet()));
    lines.add("append|v2_keys|" + String.join(",", after.values.keySet()));
    Collections.sort(lines);
    System.out.println(String.join("\n", lines));
  }

  private static void write(
      BufferAllocator allocator,
      String path,
      VectorSchemaRoot root,
      WriteParams.WriteMode mode,
      String storageVersion)
      throws IOException {
    Schema schema = root.getSchema();
    try (ArrowReader reader = new OneBatchReader(allocator, schema, root);
        Dataset ignored =
            Dataset.write()
                .allocator(allocator)
                .reader(reader)
                .uri(path)
                .mode(mode)
                .dataStorageVersion(storageVersion)
                .execute()) {
      // execute() performs the write through the Java Lance writer.
    } finally {
      root.close();
    }
  }

  private static Report read(String path, BufferAllocator allocator, long version, int wantRows)
      throws IOException {
    try (Dataset latest = Dataset.open(path);
        Dataset dataset = latest.checkoutVersion(version)) {
      if (dataset.version() != version) {
        throw new IllegalStateException(
            "opened version " + dataset.version() + ", want " + version);
      }
      validateSchema(dataset.getSchema());
      if (dataset.countRows() != wantRows) {
        throw new IllegalStateException(
            "dataset has " + dataset.countRows() + " rows, want " + wantRows);
      }
      TreeMap<String, Map<String, String>> values = new TreeMap<>();
      ScanOptions options = new ScanOptions.Builder().batchSize(1024L).build();
      try (LanceScanner scanner = LanceScanner.create(dataset, options, allocator);
          ArrowReader reader = scanner.scanBatches()) {
        while (reader.loadNextBatch()) {
          VectorSchemaRoot root = reader.getVectorSchemaRoot();
          validateSchema(root.getSchema());
          int rows = root.getRowCount();
          for (int row = 0; row < rows; row++) {
            canonicalRow(root, row, values);
          }
        }
      }
      if (values.size() != wantRows) {
        throw new IllegalStateException("read " + values.size() + " rows, want " + wantRows);
      }
      return new Report(version, values.size(), values);
    }
  }

  private static String canonicalRow(
      VectorSchemaRoot root, int row, Map<String, Map<String, String>> values) {
    String key =
        utf8(((VarCharVector) root.getVector("row_key")), row, "row_key", "row " + row);
    if (values.containsKey(key)) {
      throw new IllegalStateException("duplicate row_key " + key);
    }
    Map<String, String> rowValues = new LinkedHashMap<>();
    rowValues.put("row_key", "s:" + hex(key.getBytes(StandardCharsets.UTF_8)));
    BigIntVector ids = (BigIntVector) root.getVector("id");
    rowValues.put("id", ids.isNull(row) ? "null" : "i:" + ids.get(row));
    VarCharVector labels = (VarCharVector) root.getVector("label");
    rowValues.put(
        "label",
        labels.isNull(row)
            ? "null"
            : "s:" + hex(labels.get(row)));
    VarBinaryVector payloads = (VarBinaryVector) root.getVector("payload");
    rowValues.put(
        "payload", payloads.isNull(row) ? "null" : "b:" + hex(payloads.get(row)));
    rowValues.put("tags", canonicalStrings((ListVector) root.getVector("tags"), row));
    rowValues.put("profile", canonicalStruct((StructVector) root.getVector("profile"), row));
    rowValues.put(
        "embedding", canonicalFloats((FixedSizeListVector) root.getVector("embedding"), row));
    DecimalVector amounts = (DecimalVector) root.getVector("amount");
    rowValues.put(
        "amount",
        amounts.isNull(row) ? "null" : "d:" + amounts.getObject(row).toPlainString());
    rowValues.put("ts_us", timestamp((TimeStampVector) root.getVector("ts_us"), row));
    rowValues.put("ts_ns", timestamp((TimeStampVector) root.getVector("ts_ns"), row));
    values.put(key, rowValues);
    return key;
  }

  private static String utf8(VarCharVector vector, int row, String column, String where) {
    if (vector.isNull(row)) {
      throw new IllegalStateException(where + " has null " + column);
    }
    return new String(vector.get(row), StandardCharsets.UTF_8);
  }

  private static String canonicalStrings(ListVector vector, int row) {
    if (vector.isNull(row)) {
      return "null";
    }
    VarCharVector child = (VarCharVector) vector.getDataVector();
    int start = vector.getElementStartIndex(row);
    int end = vector.getElementEndIndex(row);
    List<String> items = new ArrayList<>();
    for (int i = start; i < end; i++) {
      items.add(child.isNull(i) ? "null" : "s:" + hex(child.get(i)));
    }
    return "[" + String.join(",", items) + "]";
  }

  private static String canonicalInts(ListVector vector, int row) {
    if (vector.isNull(row)) {
      return "null";
    }
    IntVector child = (IntVector) vector.getDataVector();
    int start = vector.getElementStartIndex(row);
    int end = vector.getElementEndIndex(row);
    List<String> items = new ArrayList<>();
    for (int i = start; i < end; i++) {
      items.add(child.isNull(i) ? "null" : "i:" + child.get(i));
    }
    return "[" + String.join(",", items) + "]";
  }

  private static String canonicalStruct(StructVector vector, int row) {
    if (vector.isNull(row)) {
      return "null";
    }
    VarCharVector names = vector.getChild("name", VarCharVector.class);
    BigIntVector scores = vector.getChild("score", BigIntVector.class);
    ListVector attrs = vector.getChild("attrs", ListVector.class);
    String name = names.isNull(row) ? "null" : "s:" + hex(names.get(row));
    String score = scores.isNull(row) ? "null" : "i:" + scores.get(row);
    return "{name=" + name + ",score=" + score + ",attrs=" + canonicalInts(attrs, row) + "}";
  }

  private static String canonicalFloats(FixedSizeListVector vector, int row) {
    if (vector.isNull(row)) {
      return "null";
    }
    Float4Vector child = (Float4Vector) vector.getDataVector();
    int start = vector.getElementStartIndex(row);
    int end = vector.getElementEndIndex(row);
    List<String> items = new ArrayList<>();
    for (int i = start; i < end; i++) {
      items.add("f:" + String.format("%08x", Float.floatToRawIntBits(child.get(i))));
    }
    return "[" + String.join(",", items) + "]";
  }

  private static String timestamp(TimeStampVector vector, int row) {
    return vector.isNull(row) ? "null" : "t:" + vector.get(row);
  }

  private static String hex(byte[] bytes) {
    StringBuilder out = new StringBuilder(bytes.length * 2);
    for (byte value : bytes) {
      out.append(String.format("%02x", value & 0xff));
    }
    return out.toString();
  }

  private static VectorSchemaRoot baseRoot(BufferAllocator allocator) {
    VectorSchemaRoot root = VectorSchemaRoot.create(schema(), allocator);
    fillBase(root);
    return root;
  }

  private static VectorSchemaRoot appendRoot(BufferAllocator allocator) {
    VectorSchemaRoot root = VectorSchemaRoot.create(schema(), allocator);
    fillAppend(root);
    return root;
  }

  private static void fillBase(VectorSchemaRoot root) {
    int n = BASE_ROWS;
    BigIntVector ids = (BigIntVector) root.getVector("id");
    VarCharVector labels = (VarCharVector) root.getVector("label");
    VarBinaryVector payloads = (VarBinaryVector) root.getVector("payload");
    ListVector tags = (ListVector) root.getVector("tags");
    StructVector profiles = (StructVector) root.getVector("profile");
    VarCharVector names = profiles.getChild("name", VarCharVector.class);
    BigIntVector scores = profiles.getChild("score", BigIntVector.class);
    ListVector attrs = profiles.getChild("attrs", ListVector.class);
    FixedSizeListVector embeddings = (FixedSizeListVector) root.getVector("embedding");
    Float4Vector embeddingValues = (Float4Vector) embeddings.getDataVector();
    DecimalVector amounts = (DecimalVector) root.getVector("amount");
    TimeStampMicroTZVector tsUs = (TimeStampMicroTZVector) root.getVector("ts_us");
    TimeStampNanoTZVector tsNs = (TimeStampNanoTZVector) root.getVector("ts_ns");
    VarCharVector keys = (VarCharVector) root.getVector("row_key");

    String[] rowKeys = {"r1", "r2", "r3", "r4"};
    long[] idValues = {9007199254740993L, -9007199254740993L, 0L, 9007199254740992L};
    boolean[] idValid = {true, true, false, true};
    String[] labelsValue = {"alpha", "", null, "café 世界"};
    byte[][] payloadValues = {{0, (byte) 0xff, 0x10}, {}, {}, {(byte) 0x80}};
    boolean[] payloadValid = {true, true, false, true};
    for (int i = 0; i < n; i++) {
      keys.setSafe(i, rowKeys[i].getBytes(StandardCharsets.UTF_8));
      if (idValid[i]) {
        ids.setSafe(i, idValues[i]);
      } else {
        ids.setNull(i);
      }
      if (labelsValue[i] == null) {
        labels.setNull(i);
      } else {
        labels.setSafe(i, labelsValue[i].getBytes(StandardCharsets.UTF_8));
      }
      if (payloadValid[i]) {
        payloads.setSafe(i, payloadValues[i]);
      } else {
        payloads.setNull(i);
      }
    }

    tags.startNewValue(0);
    ((VarCharVector) tags.getDataVector()).setSafe(0, "a".getBytes(StandardCharsets.UTF_8));
    ((VarCharVector) tags.getDataVector()).setSafe(1, "b".getBytes(StandardCharsets.UTF_8));
    tags.endValue(0, 2);
    tags.startNewValue(1);
    tags.endValue(1, 0);
    tags.setNull(2);
    tags.startNewValue(3);
    ((VarCharVector) tags.getDataVector()).setSafe(2, new byte[0]);
    ((VarCharVector) tags.getDataVector()).setSafe(3, "x".getBytes(StandardCharsets.UTF_8));
    tags.endValue(3, 2);

    // profile: present rows are defined explicitly; row 3 is a null struct.
    profiles.setIndexDefined(0);
    names.setSafe(0, "Ada".getBytes(StandardCharsets.UTF_8));
    scores.setSafe(0, 9007199254740993L);
    attrs.startNewValue(0);
    ((IntVector) attrs.getDataVector()).setSafe(0, 1);
    ((IntVector) attrs.getDataVector()).setSafe(1, 2);
    attrs.endValue(0, 2);
    profiles.setIndexDefined(1);
    names.setNull(1);
    scores.setNull(1);
    attrs.startNewValue(1);
    attrs.endValue(1, 0);
    profiles.setNull(2);
    names.setNull(2);
    scores.setNull(2);
    attrs.setNull(2);
    profiles.setIndexDefined(3);
    names.setSafe(3, new byte[0]);
    scores.setSafe(3, 0);
    attrs.setNull(3);

    int[][] embeddingBits = {
      {0x3DCCCCCD, 0xC0200000, 0x00000000, 0x3F800000},
      {0x80000000, 0x7F7FFFFF, 0x00000001, 0x3F800000},
      {0x3FC00000, 0x80000000, 0x00000001, 0x40400000},
    };
    for (int i = 0; i < n; i++) {
      if (i == 1) {
        embeddings.setNull(i);
        for (int j = 0; j < 4; j++) {
          embeddingValues.setSafe(i * 4 + j, 0.0f);
        }
      } else {
        int[] bits = embeddingBits[i == 0 ? 0 : i == 2 ? 1 : 2];
        embeddings.startNewValue(i);
        for (int j = 0; j < 4; j++) {
          embeddingValues.setSafe(i * 4 + j, Float.intBitsToFloat(bits[j]));
        }
      }
    }

    String[] amountsValue = {
      "12345678901234567890123456789.123456789",
      "-0.000000001",
      "0.000000000",
      "-12345678901234567890123456789.123456789"
    };
    boolean[] amountValid = {true, true, true, true};
    for (int i = 0; i < n; i++) {
      if (amountValid[i]) {
        amounts.setSafe(i, new BigDecimal(amountsValue[i]));
      } else {
        amounts.setNull(i);
      }
    }
    long[] usValues = {1704164645123456L, 0L, 0L, 1704164645123456L};
    long[] nsValues = {1704164645123456789L, -1L, 0L, 1704164645000000000L};
    for (int i = 0; i < n; i++) {
      if (i == 2) {
        tsUs.setNull(i);
        tsNs.setNull(i);
      } else {
        tsUs.setSafe(i, usValues[i]);
        tsNs.setSafe(i, nsValues[i]);
      }
    }
    root.setRowCount(n);
  }

  private static void fillAppend(VectorSchemaRoot root) {
    BigIntVector ids = (BigIntVector) root.getVector("id");
    VarCharVector labels = (VarCharVector) root.getVector("label");
    VarBinaryVector payloads = (VarBinaryVector) root.getVector("payload");
    ListVector tags = (ListVector) root.getVector("tags");
    StructVector profiles = (StructVector) root.getVector("profile");
    VarCharVector names = profiles.getChild("name", VarCharVector.class);
    BigIntVector scores = profiles.getChild("score", BigIntVector.class);
    ListVector attrs = profiles.getChild("attrs", ListVector.class);
    FixedSizeListVector embeddings = (FixedSizeListVector) root.getVector("embedding");
    Float4Vector embeddingValues = (Float4Vector) embeddings.getDataVector();
    DecimalVector amounts = (DecimalVector) root.getVector("amount");
    TimeStampMicroTZVector tsUs = (TimeStampMicroTZVector) root.getVector("ts_us");
    TimeStampNanoTZVector tsNs = (TimeStampNanoTZVector) root.getVector("ts_ns");
    VarCharVector keys = (VarCharVector) root.getVector("row_key");

    keys.setSafe(0, "x1".getBytes(StandardCharsets.UTF_8));
    ids.setSafe(0, 9007199254740995L);
    labels.setSafe(0, "append-1".getBytes(StandardCharsets.UTF_8));
    payloads.setSafe(0, new byte[] {(byte) 0xde, (byte) 0xad});
    tags.startNewValue(0);
    ((VarCharVector) tags.getDataVector()).setSafe(0, "x1".getBytes(StandardCharsets.UTF_8));
    tags.endValue(0, 1);
    profiles.setIndexDefined(0);
    names.setSafe(0, "App".getBytes(StandardCharsets.UTF_8));
    scores.setSafe(0, 1L);
    attrs.startNewValue(0);
    ((IntVector) attrs.getDataVector()).setSafe(0, 3);
    attrs.endValue(0, 1);
    embeddings.startNewValue(0);
    for (int j = 0; j < 4; j++) {
      embeddingValues.setSafe(j, 1.0f);
    }
    amounts.setSafe(0, new BigDecimal("1.000000000"));
    tsUs.setSafe(0, 1L);
    tsNs.setSafe(0, 1L);

    keys.setSafe(1, "x2".getBytes(StandardCharsets.UTF_8));
    ids.setNull(1);
    labels.setNull(1);
    payloads.setNull(1);
    tags.setNull(1);
    profiles.setNull(1);
    names.setNull(1);
    scores.setNull(1);
    attrs.setNull(1);
    embeddings.setNull(1);
    for (int j = 0; j < 4; j++) {
      embeddingValues.setSafe(4 + j, 0.0f);
    }
    amounts.setNull(1);
    tsUs.setNull(1);
    tsNs.setNull(1);
    root.setRowCount(APPEND_ROWS);
  }

  private static Schema schema() {
    List<Field> profileChildren =
        List.of(
            new Field(
                "name",
                new FieldType(true, new ArrowType.Utf8(), null, Map.of("role", "display")),
                Collections.emptyList()),
            new Field(
                "score", new FieldType(true, new ArrowType.Int(64, true), null, Map.of()), Collections.emptyList()),
            new Field(
                "attrs",
                new FieldType(true, new ArrowType.List(), null, Map.of()),
                List.of(
                    new Field(
                        "item",
                        new FieldType(true, new ArrowType.Int(32, true), null, Map.of()),
                        Collections.emptyList()))));
    List<Field> tagsChildren =
        List.of(
            new Field(
                "item",
                new FieldType(true, new ArrowType.Utf8(), null, Map.of()),
                Collections.emptyList()));
    List<Field> embeddingChildren =
        List.of(
            new Field(
                "item",
                new FieldType(true, new ArrowType.FloatingPoint(org.apache.arrow.vector.types.FloatingPointPrecision.SINGLE), null, Map.of()),
                Collections.emptyList()));
    List<Field> fields =
        List.of(
            field("row_key", false, new ArrowType.Utf8(), Map.of("role", "primary", "unit", "key"), Collections.emptyList()),
            field("id", true, new ArrowType.Int(64, true), Map.of(), Collections.emptyList()),
            field("label", true, new ArrowType.Utf8(), Map.of(), Collections.emptyList()),
            field("payload", true, new ArrowType.Binary(), Map.of(), Collections.emptyList()),
            field("tags", true, new ArrowType.List(), Map.of(), tagsChildren),
            field("profile", true, new ArrowType.Struct(), Map.of("encoding", "struct"), profileChildren),
            field(
                "embedding",
                true,
                new ArrowType.FixedSizeList(4),
                Map.of("dim", "4", "metric", "l2"),
                embeddingChildren),
            field(
                "amount",
                true,
                new ArrowType.Decimal(38, 9),
                Map.of("precision", "38", "unit", "currency"),
                Collections.emptyList()),
            field(
                "ts_us",
                true,
                new ArrowType.Timestamp(TimeUnit.MICROSECOND, "UTC"),
                Map.of(),
                Collections.emptyList()),
            field(
                "ts_ns",
                true,
                new ArrowType.Timestamp(TimeUnit.NANOSECOND, "Asia/Shanghai"),
                Map.of(),
                Collections.emptyList()));
    return new Schema(fields, SCHEMA_METADATA);
  }

  private static Field field(
      String name, boolean nullable, ArrowType type, Map<String, String> metadata, List<Field> children) {
    return new Field(name, new FieldType(nullable, type, null, metadata), children);
  }

  private static void validateSchema(Schema schema) {
    if (!SCHEMA_METADATA.equals(schema.getCustomMetadata())) {
      throw new IllegalStateException(
          "schema metadata " + schema.getCustomMetadata() + ", want " + SCHEMA_METADATA);
    }
    List<Field> fields = schema.getFields();
    if (fields.size() != 10) {
      throw new IllegalStateException("field count " + fields.size() + ", want 10");
    }
    validateField(fields.get(0), "row_key", false, new ArrowType.Utf8(), Map.of("role", "primary", "unit", "key"));
    validateField(fields.get(1), "id", true, new ArrowType.Int(64, true), Map.of());
    validateField(fields.get(2), "label", true, new ArrowType.Utf8(), Map.of());
    validateField(fields.get(3), "payload", true, new ArrowType.Binary(), Map.of());
    validateField(fields.get(4), "tags", true, new ArrowType.List(), Map.of());
    validateField(fields.get(5), "profile", true, new ArrowType.Struct(), Map.of("encoding", "struct"));
    validateField(
        fields.get(6),
        "embedding",
        true,
        new ArrowType.FixedSizeList(4),
        Map.of("dim", "4", "metric", "l2"));
    validateField(
        fields.get(7),
        "amount",
        true,
        new ArrowType.Decimal(38, 9),
        Map.of("precision", "38", "unit", "currency"));
    validateField(
        fields.get(8),
        "ts_us",
        true,
        new ArrowType.Timestamp(TimeUnit.MICROSECOND, "UTC"),
        Map.of());
    validateField(
        fields.get(9),
        "ts_ns",
        true,
        new ArrowType.Timestamp(TimeUnit.NANOSECOND, "Asia/Shanghai"),
        Map.of());
    validateChild(fields.get(4), "item", true, new ArrowType.Utf8(), Map.of());
    List<Field> profile = fields.get(5).getChildren();
    if (profile.size() != 3) {
      throw new IllegalStateException("profile child count " + profile.size() + ", want 3");
    }
    validateField(profile.get(0), "name", true, new ArrowType.Utf8(), Map.of("role", "display"));
    validateField(profile.get(1), "score", true, new ArrowType.Int(64, true), Map.of());
    validateField(profile.get(2), "attrs", true, new ArrowType.List(), Map.of());
    validateChild(profile.get(2), "item", true, new ArrowType.Int(32, true), Map.of());
    validateChild(
        fields.get(6),
        "item",
        true,
        new ArrowType.FloatingPoint(org.apache.arrow.vector.types.FloatingPointPrecision.SINGLE),
        Map.of());
  }

  private static void validateChild(
      Field parent, String name, boolean nullable, ArrowType type, Map<String, String> metadata) {
    List<Field> children = parent.getChildren();
    if (children.size() != 1) {
      throw new IllegalStateException(parent.getName() + " child count " + children.size() + ", want 1");
    }
    validateField(children.get(0), name, nullable, type, metadata);
  }

  private static void validateField(
      Field field, String name, boolean nullable, ArrowType type, Map<String, String> metadata) {
    if (!field.getName().equals(name)) {
      throw new IllegalStateException("field name " + field.getName() + ", want " + name);
    }
    if (!field.getType().equals(type)) {
      throw new IllegalStateException(name + " type " + field.getType() + ", want " + type);
    }
    if (field.isNullable() != nullable) {
      throw new IllegalStateException(name + " nullable " + field.isNullable() + ", want " + nullable);
    }
    Map<String, String> actualMetadata = field.getMetadata();
    if (actualMetadata == null) {
      actualMetadata = Map.of();
    }
    if (!actualMetadata.equals(metadata)) {
      throw new IllegalStateException(name + " metadata " + actualMetadata + ", want " + metadata);
    }
  }

  private static final class OneBatchReader extends ArrowReader {
    private final Schema schema;
    private final VectorSchemaRoot root;
    private boolean read;

    OneBatchReader(BufferAllocator allocator, Schema schema, VectorSchemaRoot root) {
      super(allocator);
      this.schema = schema;
      this.root = root;
    }

    @Override
    public boolean loadNextBatch() {
      if (read) {
        return false;
      }
      read = true;
      return true;
    }

    @Override
    public long bytesRead() {
      return 0;
    }

    @Override
    protected void closeReadSource() {}

    @Override
    protected Schema readSchema() {
      return schema;
    }

    @Override
    public VectorSchemaRoot getVectorSchemaRoot() {
      return root;
    }
  }

  private static final class Report {
    final long version;
    final int rows;
    final TreeMap<String, Map<String, String>> values;

    Report(long version, int rows, TreeMap<String, Map<String, String>> values) {
      this.version = version;
      this.rows = rows;
      this.values = values;
    }

    String text() {
      List<String> lines = new ArrayList<>();
      lines.add("schema|fixture|" + hex("phase5-v1".getBytes(StandardCharsets.UTF_8)));
      lines.add("schema|owner|" + hex("interop".getBytes(StandardCharsets.UTF_8)));
      lines.add(fieldLine("0", "row_key", "utf8", false, Map.of("role", "primary", "unit", "key")));
      lines.add(fieldLine("1", "id", "int64", true, Map.of()));
      lines.add(fieldLine("2", "label", "utf8", true, Map.of()));
      lines.add(fieldLine("3", "payload", "binary", true, Map.of()));
      lines.add(fieldLine("4", "tags", "list<utf8>", true, Map.of()));
      lines.add(fieldLine("5", "profile", "struct", true, Map.of("encoding", "struct")));
      lines.add(fieldLine("5.0", "profile.name", "utf8", true, Map.of("role", "display")));
      lines.add(fieldLine("5.1", "profile.score", "int64", true, Map.of()));
      lines.add(fieldLine("5.2", "profile.attrs", "list<int32>", true, Map.of()));
      lines.add(
          fieldLine(
              "6",
              "embedding",
              "fixed_size_list<float32,4>",
              true,
              Map.of("dim", "4", "metric", "l2")));
      lines.add(
          fieldLine(
              "7",
              "amount",
              "decimal128(38,9)",
              true,
              Map.of("precision", "38", "unit", "currency")));
      lines.add(fieldLine("8", "ts_us", "timestamp<us,UTC>", true, Map.of()));
      lines.add(fieldLine("9", "ts_ns", "timestamp<ns,Asia/Shanghai>", true, Map.of()));
      List<String> columns =
          List.of(
              "row_key",
              "id",
              "label",
              "payload",
              "tags",
              "profile",
              "embedding",
              "amount",
              "ts_us",
              "ts_ns");
      List<String> types =
          List.of(
              "utf8",
              "int64",
              "utf8",
              "binary",
              "list<utf8>",
              "struct",
              "fixed_size_list<float32,4>",
              "decimal128(38,9)",
              "timestamp<us,UTC>",
              "timestamp<ns,Asia/Shanghai>");
      for (Map.Entry<String, Map<String, String>> entry : values.entrySet()) {
        for (int i = 0; i < columns.size(); i++) {
          lines.add(
              "value|"
                  + entry.getKey()
                  + "|"
                  + columns.get(i)
                  + "|"
                  + types.get(i)
                  + "|"
                  + entry.getValue().get(columns.get(i)));
        }
      }
      lines.add("rows|" + rows);
      lines.add("version|" + version);
      Collections.sort(lines);
      return String.join("\n", lines) + "\n";
    }

    private static String fieldLine(
        String index, String name, String type, boolean nullable, Map<String, String> metadata) {
      if (metadata == null) {
        metadata = Map.of();
      }
      StringBuilder line =
          new StringBuilder("field|")
              .append(index)
              .append("|")
              .append(name)
              .append("|")
              .append(type)
              .append("|")
              .append(nullable)
              .append("|");
      List<String> keys = new ArrayList<>(metadata.keySet());
      Collections.sort(keys);
      List<String> parts = new ArrayList<>();
      for (String key : keys) {
        parts.add(key);
        parts.add(hex(metadata.get(key).getBytes(StandardCharsets.UTF_8)));
      }
      return line.append(String.join("|", parts)).toString();
    }
  }
}
