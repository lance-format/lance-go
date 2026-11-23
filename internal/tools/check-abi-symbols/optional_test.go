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

const optionalBindFunction = `func bindOptional(lib symbolSource, b *bindings) {
	if addr, err := lib.Lookup("lance_optional"); err == nil {
		purego.RegisterFunc(&b.opt.optionalCall, addr)
	}
	if addr, err := lib.Lookup("lance_additional"); err == nil {
		purego.RegisterFunc(&b.opt.additionalCall, addr)
	}
}`

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

func writeFFIFixtureFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "ffi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
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
	if err == nil || !strings.Contains(err.Error(), "not guarded by a successful lib.Lookup") {
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

// A group whose registrations were all deleted must still be checked. Coverage
// built only from observed registrations would silently drop it.
func TestVerifyRejectsOptionalGroupWithNoRegistrations(t *testing.T) {
	source := strings.Replace(optionalFixture,
		`func bindOptional(lib symbolSource, b *bindings) {
	if addr, err := lib.Lookup("lance_optional"); err == nil {
		purego.RegisterFunc(&b.opt.optionalCall, addr)
	}
	if addr, err := lib.Lookup("lance_additional"); err == nil {
		purego.RegisterFunc(&b.opt.additionalCall, addr)
	}
}`,
		`func bindOptional(lib symbolSource, b *bindings) {}`,
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
	if err == nil {
		t.Fatal("verify accepted an optional group with no registrations")
	}
	for _, want := range []string{"optionalCall", "additionalCall", "without a lib.Lookup binding"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("verify error = %v, want %q", err, want)
		}
	}
}

func TestParseOptionalRejectsFailedLookupCondition(t *testing.T) {
	source := strings.Replace(optionalFixture,
		`if addr, err := lib.Lookup("lance_optional"); err == nil {`,
		`if addr, err := lib.Lookup("lance_optional"); err != nil {`,
		1)

	_, _, err := parseGo(writeFFIFixture(t, source))
	if err == nil || !strings.Contains(err.Error(), "successful lib.Lookup") {
		t.Fatalf("parseGo error = %v, want a failed lookup condition to be rejected", err)
	}
}

func TestParseOptionalRejectsDifferentAddress(t *testing.T) {
	source := strings.Replace(optionalFixture,
		`purego.RegisterFunc(&b.opt.optionalCall, addr)`,
		`purego.RegisterFunc(&b.opt.optionalCall, 0)`,
		1)

	_, _, err := parseGo(writeFFIFixture(t, source))
	if err == nil || !strings.Contains(err.Error(), "address other than addr") {
		t.Fatalf("parseGo error = %v, want a different address argument to be rejected", err)
	}
}

func TestParseOptionalRejectsExtraUnguardedRegistration(t *testing.T) {
	source := optionalFixture + `
func extraRegistration(b *bindings) {
	purego.RegisterFunc(&b.opt.optionalCall, 0)
}
`
	_, _, err := parseGo(writeFFIFixture(t, source))
	if err == nil || !strings.Contains(err.Error(), "not guarded by a successful lib.Lookup") {
		t.Fatalf("parseGo error = %v, want an extra unguarded registration to be rejected", err)
	}
}

func TestParseOptionalRejectsDuplicateGuardedRegistration(t *testing.T) {
	guard := `	if addr, err := lib.Lookup("lance_optional"); err == nil {
		purego.RegisterFunc(&b.opt.optionalCall, addr)
	}`
	source := strings.Replace(optionalFixture, guard, guard+"\n"+guard, 1)

	_, _, err := parseGo(writeFFIFixture(t, source))
	if err == nil || !strings.Contains(err.Error(), "bound more than once") {
		t.Fatalf("parseGo error = %v, want a duplicate guarded registration to be rejected", err)
	}
}

func TestOptionalCountsExcludeAbsentSignatures(t *testing.T) {
	model := &bindingsModel{optional: []boundSymbol{{name: "lance_a"}, {name: "lance_b"}}}
	header := map[string]cFunc{"lance_a": {name: "lance_a"}}

	checked, absent := optionalCounts(model, header)
	if checked != 1 || absent != 1 {
		t.Fatalf("optionalCounts = %d checked, %d absent; want 1 and 1", checked, absent)
	}
	if note := optionalAbsentNote(absent); !strings.Contains(note, "1 optional binding absent") {
		t.Fatalf("optionalAbsentNote(1) = %q", note)
	}
	if note := optionalAbsentNote(0); note != "" {
		t.Fatalf("optionalAbsentNote(0) = %q, want empty", note)
	}
}

// A registration nested inside another block is not the supported directly
// paired form; a deeper walk would otherwise misread it as guarded.
func TestParseOptionalRejectsNestedRegistrationForm(t *testing.T) {
	source := strings.Replace(optionalFixture,
		`	if addr, err := lib.Lookup("lance_optional"); err == nil {
		purego.RegisterFunc(&b.opt.optionalCall, addr)
	}`,
		`	if addr, err := lib.Lookup("lance_optional"); err == nil {
		if true {
			purego.RegisterFunc(&b.opt.optionalCall, addr)
		}
	}`,
		1)

	_, _, err := parseGo(writeFFIFixture(t, source))
	if err == nil || !strings.Contains(err.Error(), "unsupported registration form") {
		t.Fatalf("parseGo error = %v, want a nested registration form to be rejected", err)
	}
}

// Shadowing the address inside the guard body is ambiguous and unsupported.
func TestParseOptionalRejectsShadowedAddress(t *testing.T) {
	source := strings.Replace(optionalFixture,
		`	if addr, err := lib.Lookup("lance_optional"); err == nil {
		purego.RegisterFunc(&b.opt.optionalCall, addr)
	}`,
		`	if addr, err := lib.Lookup("lance_optional"); err == nil {
		addr := addr
		purego.RegisterFunc(&b.opt.optionalCall, addr)
	}`,
		1)

	_, _, err := parseGo(writeFFIFixture(t, source))
	if err == nil || !strings.Contains(err.Error(), "unsupported registration form") {
		t.Fatalf("parseGo error = %v, want a shadowed address binding to be rejected", err)
	}
}

// Two bindings fields can share a group type; one registration must not cover
// both instance paths.
func TestVerifyRequiresCoveragePerBindingsPath(t *testing.T) {
	source := strings.Replace(optionalFixture,
		`type bindings struct {
	required func(handle uintptr) int32
	opt      optionalGroup
}`,
		`type bindings struct {
	required func(handle uintptr) int32
	optA     optionalGroup
	optB     optionalGroup
}`,
		1)
	source = strings.ReplaceAll(source, "b.opt.", "b.optA.")

	_, model, err := parseGo(writeFFIFixture(t, source))
	if err != nil {
		t.Fatalf("parseGo: %v", err)
	}
	header, err := parseHeader([]byte(optionalHeader))
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}

	err = verify(&pin{}, model, header)
	if err == nil {
		t.Fatal("verify accepted coverage for one of two bindings instance paths")
	}
	for _, want := range []string{"optB.optionalCall", "optB.additionalCall"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("verify error = %v, want %q", err, want)
		}
	}
}

// Target-field uniqueness is a package-wide property, not a per-file one.
func TestParseGoRejectsDuplicateOptionalFieldAcrossFiles(t *testing.T) {
	extra := `package ffi

func bindExtra(lib symbolSource, b *bindings) {
	if addr, err := lib.Lookup("lance_extra"); err == nil {
		purego.RegisterFunc(&b.opt.optionalCall, addr)
	}
}
`
	_, _, err := parseGo(writeFFIFixtureFiles(t, map[string]string{
		"bindings.go": optionalFixture,
		"extra.go":    extra,
	}))
	if err == nil || !strings.Contains(err.Error(), "registered more than once across package files") {
		t.Fatalf("parseGo error = %v, want a cross-file duplicate field to be rejected", err)
	}
}

// Grouped named fields such as "first, second optionalGroup" declare two
// bindings instance paths; both must be collected and covered independently.
func TestVerifyRequiresCoverageForGroupedBindingsFields(t *testing.T) {
	grouped := strings.Replace(optionalFixture,
		`type bindings struct {
	required func(handle uintptr) int32
	opt      optionalGroup
}`,
		`type bindings struct {
	required func(handle uintptr) int32
	first, second optionalGroup
}`,
		1)

	header, err := parseHeader([]byte(optionalHeader))
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}

	t.Run("no registrations collects both names", func(t *testing.T) {
		source := strings.Replace(grouped, optionalBindFunction,
			`func bindOptional(lib symbolSource, b *bindings) {}`, 1)
		_, model, err := parseGo(writeFFIFixture(t, source))
		if err != nil {
			t.Fatalf("parseGo: %v", err)
		}
		err = verify(&pin{}, model, header)
		if err == nil {
			t.Fatal("verify accepted grouped optional fields with no registrations")
		}
		for _, want := range []string{"first.optionalCall", "second.optionalCall", "second.additionalCall"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("verify error = %v, want %q", err, want)
			}
		}
	})

	t.Run("missing member rejected", func(t *testing.T) {
		source := strings.ReplaceAll(grouped, "b.opt.", "b.first.")
		_, model, err := parseGo(writeFFIFixture(t, source))
		if err != nil {
			t.Fatalf("parseGo: %v", err)
		}
		err = verify(&pin{}, model, header)
		if err == nil {
			t.Fatal("verify accepted a missing grouped optional member")
		}
		for _, want := range []string{"second.optionalCall", "second.additionalCall"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("verify error = %v, want %q", err, want)
			}
		}
		if strings.Contains(err.Error(), "first.optionalCall") {
			t.Fatalf("first should be covered, error = %v", err)
		}
	})
}
