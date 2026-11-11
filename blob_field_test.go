package lance_test

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"

	lance "github.com/lance-format/lance-go"
)

const (
	blobExtensionKey          = "ARROW:extension:name"
	blobInlineThresholdKey    = "lance-encoding:blob-inline-size-threshold"
	blobDedicatedThresholdKey = "lance-encoding:blob-dedicated-size-threshold"
	blobExtensionName         = "lance.blob.v2"
)

func TestBlobFieldNativeShape(t *testing.T) {
	requireNative(t)

	field, err := lance.BlobField("payload", true)
	if err != nil {
		t.Fatalf("BlobField: %v", err)
	}
	if field.Name != "payload" || !field.Nullable {
		t.Fatalf("field = %q nullable=%v, want payload nullable", field.Name, field.Nullable)
	}
	if got, ok := field.Metadata.GetValue(blobExtensionKey); !ok || got != blobExtensionName {
		t.Fatalf("extension metadata = %q ok=%v, want %q", got, ok, blobExtensionName)
	}
	if _, ok := field.Metadata.GetValue(blobInlineThresholdKey); ok {
		t.Fatal("default field unexpectedly has an inline threshold")
	}
	if _, ok := field.Metadata.GetValue(blobDedicatedThresholdKey); ok {
		t.Fatal("default field unexpectedly has a dedicated threshold")
	}
	structType, ok := field.Type.(*arrow.StructType)
	if !ok {
		t.Fatalf("field type = %T, want struct", field.Type)
	}
	if got := structType.NumFields(); got != 2 {
		t.Fatalf("field has %d children, want 2", got)
	}
	if name := structType.Field(0).Name; name != "data" {
		t.Fatalf("child 0 = %q, want data", name)
	}
	if id := structType.Field(0).Type.ID(); id != arrow.LARGE_BINARY {
		t.Fatalf("data type = %v, want LargeBinary", structType.Field(0).Type)
	}
	if name := structType.Field(1).Name; name != "uri" {
		t.Fatalf("child 1 = %q, want uri", name)
	}
	if id := structType.Field(1).Type.ID(); id != arrow.STRING {
		t.Fatalf("uri type = %v, want Utf8", structType.Field(1).Type)
	}
}

func TestBlobFieldThresholdOptions(t *testing.T) {
	requireNative(t)

	field, err := lance.BlobFieldWithOptions("payload", false, lance.BlobFieldOptions{
		InlineSizeThreshold:    ptr(uint64(0)),
		DedicatedSizeThreshold: ptr(uint64(1024)),
	})
	if err != nil {
		t.Fatalf("BlobFieldWithOptions: %v", err)
	}
	if field.Nullable {
		t.Fatal("field is nullable, want non-nullable")
	}
	if got, ok := field.Metadata.GetValue(blobInlineThresholdKey); !ok || got != "0" {
		t.Fatalf("inline threshold = %q ok=%v, want 0", got, ok)
	}
	if got, ok := field.Metadata.GetValue(blobDedicatedThresholdKey); !ok || got != "1024" {
		t.Fatalf("dedicated threshold = %q ok=%v, want 1024", got, ok)
	}

	if _, err := lance.BlobFieldWithOptions("payload", true, lance.BlobFieldOptions{
		DedicatedSizeThreshold: ptr(uint64(0)),
	}); err == nil {
		t.Fatal("present zero dedicated threshold was accepted")
	}
}

func TestBlobFieldRejectsEmbeddedNUL(t *testing.T) {
	requireNative(t)

	_, err := lance.BlobField("bad\x00name", true)
	if err == nil {
		t.Fatal("embedded NUL was accepted")
	}
	if !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("error = %v, want NUL rejection", err)
	}
}
