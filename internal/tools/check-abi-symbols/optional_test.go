package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const optionalFixture = `package ffi

type symbol struct {
	name   string
	target any
}

type optionalGroup struct {
	optionalCall  func(handle uintptr, count uint32) int32
	additionalCall func(handle uintptr) int32
}

type bindings struct {
	required func(handle uintptr) int32
	opt      optionalGroup
}

func (b *bindings) symbols() []symbol {
	return []symbol{
		{"lance_required", &b.required},
	}
}

func bindOptional(lib symbolSource, b *bindings) {
	if addr, err := lib.Lookup("lance_optional"); err == nil {
		purego.RegisterFunc(&b.opt.optionalCall, addr)
	}
	if addr, err := lib.Lookup("lance_additional"); err == nil {
		purego.RegisterFunc(&b.opt.additionalCall, addr)
	}
}
`

const optionalHeader = `
int32_t lance_required(LanceDataset* handle);
int32_t lance_optional(LanceDataset* handle, uint32_t count);
int32_t lance_additional(LanceDataset* handle);
`

func writeFFIFixture(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "ffi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bindings.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return root
}

func parseFixture(t *testing.T) (*bindingsModel, map[string]cFunc) {
	t.Helper()
	_, model, err := parseGo(writeFFIFixture(t, optionalFixture))
	if err != nil {
		t.Fatalf("parseGo: %v", err)
	}
	header, err := parseHeader([]byte(optionalHeader))
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}
	return model, header
}

func TestVerifyChecksOptionalSignatures(t *testing.T) {
	model, header := parseFixture(t)
	if len(model.optional) != 2 {
		t.Fatalf("optional bindings = %d, want 2", len(model.optional))
	}
	if err := verify(&pin{}, model, header); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyRejectsMismatchedOptionalSignature(t *testing.T) {
	model, header := parseFixture(t)
	header["lance_optional"] = cFunc{name: "lance_optional", result: "int32_t", params: []string{"LanceDataset* handle", "uint64_t count"}}

	err := verify(&pin{}, model, header)
	if err == nil {
		t.Fatal("verify accepted a mismatched optional signature")
	}
	if !strings.Contains(err.Error(), "lance_optional") || !strings.Contains(err.Error(), "parameter 2") {
		t.Fatalf("verify error = %q, want a lance_optional parameter 2 mismatch", err)
	}
}

func TestVerifyAllowsOptionalAbsentFromHeader(t *testing.T) {
	model, header := parseFixture(t)
	delete(header, "lance_optional")
	delete(header, "lance_additional")

	if err := verify(&pin{}, model, header); err != nil {
		t.Fatalf("verify rejected optional symbols absent from the header: %v", err)
	}
}

func TestVerifyRejectsOptionalAlsoBoundAsRequired(t *testing.T) {
	source := strings.Replace(optionalFixture,
		`	required func(handle uintptr) int32`,
		`	required    func(handle uintptr) int32
	requiredAlt func(handle uintptr, count uint32) int32`,
		1)
	source = strings.Replace(source,
		`{"lance_required", &b.required},`,
		`{"lance_required", &b.required},
		{"lance_optional", &b.requiredAlt},`,
		1)
	_, model, err := parseGo(writeFFIFixture(t, source))
	if err != nil {
		t.Fatalf("parseGo: %v", err)
	}
	header, err := parseHeader([]byte(optionalHeader))
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}

	err = verify(&pin{}, model, header)
	if err == nil || !strings.Contains(err.Error(), "bound both as required and as optional") {
		t.Fatalf("verify error = %v, want a required/optional collision", err)
	}
}

func TestVerifyRejectsOptionalWithoutLookupGuard(t *testing.T) {
	source := strings.Replace(optionalFixture,
		`	if addr, err := lib.Lookup("lance_additional"); err == nil {
		purego.RegisterFunc(&b.opt.additionalCall, addr)
	}`,
		`	purego.RegisterFunc(&b.opt.additionalCall, 0)`,
		1)

	_, _, err := parseGo(writeFFIFixture(t, source))
	if err == nil || !strings.Contains(err.Error(), "not guarded by lib.Lookup") {
		t.Fatalf("parseGo error = %v, want an unguarded optional registration", err)
	}
}

func TestVerifyRejectsUncoveredOptionalField(t *testing.T) {
	source := strings.Replace(optionalFixture,
		`	if addr, err := lib.Lookup("lance_additional"); err == nil {
		purego.RegisterFunc(&b.opt.additionalCall, addr)
	}`,
		``,
		1)
	_, model, err := parseGo(writeFFIFixture(t, source))
	if err != nil {
		t.Fatalf("parseGo: %v", err)
	}
	header, err := parseHeader([]byte(optionalHeader))
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}

	err = verify(&pin{}, model, header)
	if err == nil || !strings.Contains(err.Error(), "without a lib.Lookup binding") {
		t.Fatalf("verify error = %v, want an uncovered optional field", err)
	}
}
