// Command check-abi-symbols verifies the purego bindings in internal/ffi
// against the lance-c C header from the commit pinned in that package.
//
// The header is not vendored. Export it from the pinned upstream checkout:
//
//	git -C /path/to/lance-c show ab5c2d39c8b912fff19a05486732dde4b49c3f33:include/lance/lance.h > lance.h
//	LANCE_C_HEADER=./lance.h go run ./scripts/check-abi-symbols.go
//
// It checks that every bound symbol exists, that Go and C agree on arity and
// on value/pointer classification, that every function field in the bindings
// struct has a symbol entry, and that the header bytes match the pinned
// SHA-256. It cannot detect a same-size integer type confusion beyond the
// signed and unsigned integer classes it models.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func main() {
	headerPath := flag.String("header", os.Getenv("LANCE_C_HEADER"), "path to include/lance/lance.h from the pinned lance-c commit (default: $LANCE_C_HEADER)")
	root := flag.String("root", ".", "lance-go repository root")
	flag.Parse()

	if err := run(*root, *headerPath); err != nil {
		fmt.Fprintf(os.Stderr, "check-abi-symbols: %v\n", err)
		os.Exit(1)
	}
}

type pin struct {
	repo      string
	release   string
	commit    string
	headerSHA string
}

type boundSymbol struct {
	name  string
	field string
}

type bindingsModel struct {
	fields  map[string]*ast.FuncType
	symbols []boundSymbol
}

type cFunc struct {
	name   string
	result string
	params []string
}

func run(root, headerPath string) error {
	upstream, model, err := parseGo(root)
	if err != nil {
		return err
	}
	for name, value := range map[string]string{
		"upstreamCommit":       upstream.commit,
		"upstreamHeaderSHA256": upstream.headerSHA,
	} {
		if value == "" {
			return fmt.Errorf("internal/ffi/upstream.go is missing the %s constant", name)
		}
	}

	headerHint := fmt.Sprintf("git -C /path/to/lance-c show %s:include/lance/lance.h > lance.h", upstream.commit)
	if headerPath == "" {
		return fmt.Errorf("no header supplied; export the pinned header and pass it with -header or LANCE_C_HEADER:\n  %s", headerHint)
	}

	data, err := os.ReadFile(headerPath)
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	sum := sha256.Sum256(data)
	actualSHA := hex.EncodeToString(sum[:])
	if actualSHA != upstream.headerSHA {
		return fmt.Errorf("header sha256 is %s, want %s for lance-c %s (%s); export the pinned revision:\n  %s\nUpdate upstreamHeaderSHA256 only when re-pinning intentionally",
			actualSHA, upstream.headerSHA, upstream.release, upstream.commit, headerHint)
	}

	header, err := parseHeader(data)
	if err != nil {
		return fmt.Errorf("parse %s: %w", headerPath, err)
	}
	if err := verify(upstream, model, header); err != nil {
		return err
	}

	fmt.Printf("check-abi-symbols: verified %d bindings against %s (lance-c %s %s, header sha256 %s)\n",
		len(model.symbols), headerPath, upstream.release, upstream.commit, upstream.headerSHA)
	return nil
}

func parseGo(root string) (*pin, *bindingsModel, error) {
	dir := filepath.Join(root, "internal", "ffi")
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		name := fi.Name()
		return !strings.HasPrefix(name, ".") && !strings.HasSuffix(name, "_test.go")
	}, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", dir, err)
	}
	pkg, ok := pkgs["ffi"]
	if !ok {
		return nil, nil, fmt.Errorf("package ffi not found in %s", dir)
	}

	upstream := &pin{}
	model := &bindingsModel{fields: map[string]*ast.FuncType{}}

	var filenames []string
	for name := range pkg.Files {
		filenames = append(filenames, name)
	}
	sort.Strings(filenames)

	constants := map[string]string{}
	structFound := false

	for _, filename := range filenames {
		file := pkg.Files[filename]

		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range spec.Names {
				if i >= len(spec.Values) {
					continue
				}
				lit, ok := spec.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if value, err := strconv.Unquote(lit.Value); err == nil {
					constants[name.Name] = value
				}
			}
			return true
		})

		ast.Inspect(file, func(n ast.Node) bool {
			typeSpec, ok := n.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "bindings" {
				return true
			}
			if structFound {
				return true
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			structFound = true
			for _, field := range structType.Fields.List {
				funcType, ok := field.Type.(*ast.FuncType)
				if !ok {
					continue
				}
				for _, name := range field.Names {
					model.fields[name.Name] = funcType
				}
			}
			return true
		})
	}

	if !structFound {
		return nil, nil, fmt.Errorf("type bindings not found in %s", dir)
	}

	for _, filename := range filenames {
		file := pkg.Files[filename]
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			arrayType, ok := lit.Type.(*ast.ArrayType)
			if !ok {
				return true
			}
			elt, ok := arrayType.Elt.(*ast.Ident)
			if !ok || elt.Name != "symbol" {
				return true
			}
			for _, raw := range lit.Elts {
				entry, ok := raw.(*ast.CompositeLit)
				if !ok || len(entry.Elts) != 2 {
					continue
				}
				nameLit, ok := entry.Elts[0].(*ast.BasicLit)
				if !ok || nameLit.Kind != token.STRING {
					continue
				}
				name, err := strconv.Unquote(nameLit.Value)
				if err != nil {
					continue
				}
				unary, ok := entry.Elts[1].(*ast.UnaryExpr)
				if !ok {
					continue
				}
				selector, ok := unary.X.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				model.symbols = append(model.symbols, boundSymbol{name: name, field: selector.Sel.Name})
			}
			return true
		})
	}

	upstream.repo = constants["upstreamRepo"]
	upstream.release = constants["upstreamRelease"]
	upstream.commit = constants["upstreamCommit"]
	upstream.headerSHA = constants["upstreamHeaderSHA256"]

	if len(model.symbols) == 0 {
		return nil, nil, fmt.Errorf("no []symbol{{...}} binding list found in %s", dir)
	}
	return upstream, model, nil
}

var commentBlock = regexp.MustCompile(`(?s)/\*.*?\*/`)
var commentLine = regexp.MustCompile(`//[^\n]*`)
var lanceFunc = regexp.MustCompile(`\b(lance_[A-Za-z0-9_]+)[ \t\r\n]*\(`)

func parseHeader(src []byte) (map[string]cFunc, error) {
	text := stripComments(string(src))
	funcs := map[string]cFunc{}

	for _, loc := range lanceFunc.FindAllStringSubmatchIndex(text, -1) {
		name := text[loc[2]:loc[3]]
		nameStart := loc[2]
		lineStart := strings.LastIndexByte(text[:nameStart], '\n') + 1
		prefix := strings.TrimSpace(text[lineStart:nameStart])

		// Only top-level declarations have a plain return type before the
		// name. Skip typedefs, preprocessor lines and text inside another
		// parameter list.
		if prefix == "" || strings.ContainsAny(prefix, "();#{}") {
			continue
		}
		if strings.HasPrefix(prefix, "typedef ") || strings.HasPrefix(prefix, "struct ") ||
			strings.HasPrefix(prefix, "enum ") || strings.HasPrefix(prefix, "union ") {
			continue
		}

		open := loc[1] - 1
		closeIdx, ok := matchParen(text, open)
		if !ok {
			return nil, fmt.Errorf("unbalanced parentheses in declaration of %s", name)
		}

		params := parseCParams(text[open+1 : closeIdx])
		if _, exists := funcs[name]; exists {
			return nil, fmt.Errorf("duplicate declaration of %s", name)
		}
		funcs[name] = cFunc{name: name, result: prefix, params: params}
	}

	if len(funcs) == 0 {
		return nil, fmt.Errorf("no lance_* function declarations found")
	}
	return funcs, nil
}

func stripComments(src string) string {
	src = commentBlock.ReplaceAllStringFunc(src, blankExceptNewlines)
	return commentLine.ReplaceAllStringFunc(src, blankExceptNewlines)
}

func blankExceptNewlines(block string) string {
	out := []byte(block)
	for i := range out {
		if out[i] != '\n' {
			out[i] = ' '
		}
	}
	return string(out)
}

func matchParen(s string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func parseCParams(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "void" {
		return nil
	}

	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.Join(strings.Fields(raw[start:i]), " "))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.Join(strings.Fields(raw[start:]), " "))
	return parts
}

func verify(upstream *pin, model *bindingsModel, header map[string]cFunc) error {
	boundFields := map[string]bool{}
	seenSymbols := map[string]bool{}

	for _, sym := range model.symbols {
		if seenSymbols[sym.name] {
			return fmt.Errorf("symbol %s is bound more than once", sym.name)
		}
		seenSymbols[sym.name] = true
		boundFields[sym.field] = true

		goFuncType, ok := model.fields[sym.field]
		if !ok {
			return fmt.Errorf("symbol %s points at bindings field %s, which is not a function field", sym.name, sym.field)
		}
		cFuncType, ok := header[sym.name]
		if !ok {
			return fmt.Errorf("bound symbol %s is not declared in the pinned header for lance-c %s (%s)", sym.name, upstream.release, upstream.commit)
		}

		goParams, goResult, err := describeGoFunc(goFuncType)
		if err != nil {
			return fmt.Errorf("bindings field %s (%s): %w", sym.field, sym.name, err)
		}
		cResult := cClass(cFuncType.result)
		if cResult != goResult {
			return fmt.Errorf("%s: C returns %q (%s), Go binding returns %s", sym.name, cFuncType.result, cResult, goResult)
		}

		if len(goParams) != len(cFuncType.params) {
			return fmt.Errorf("%s: C takes %d parameter(s), Go binding takes %d", sym.name, len(cFuncType.params), len(goParams))
		}
		for i, cParam := range cFuncType.params {
			cCls := cClass(cParamType(cParam))
			if cCls != goParams[i] {
				return fmt.Errorf("%s: parameter %d is %q (%s) in C but %s in the Go binding", sym.name, i+1, cParam, cCls, goParams[i])
			}
		}
	}

	var unbound []string
	for field := range model.fields {
		if !boundFields[field] {
			unbound = append(unbound, field)
		}
	}
	if len(unbound) > 0 {
		sort.Strings(unbound)
		return fmt.Errorf("bindings function field(s) %s have no symbol entry", strings.Join(unbound, ", "))
	}
	return nil
}

func describeGoFunc(ft *ast.FuncType) ([]string, string, error) {
	var params []string
	if ft.Params != nil {
		for _, field := range ft.Params.List {
			cls, err := goClass(field.Type)
			if err != nil {
				return nil, "", err
			}
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				params = append(params, cls)
			}
		}
	}

	result := "void"
	if ft.Results != nil && len(ft.Results.List) > 0 {
		count := 0
		for _, field := range ft.Results.List {
			n := len(field.Names)
			if n == 0 {
				n = 1
			}
			count += n
		}
		if count != 1 {
			return nil, "", fmt.Errorf("function has %d return values; purego supports at most one", count)
		}
		cls, err := goClass(ft.Results.List[0].Type)
		if err != nil {
			return nil, "", err
		}
		result = cls
	}
	return params, result, nil
}

func goClass(expr ast.Expr) (string, error) {
	switch e := expr.(type) {
	case *ast.Ident:
		switch e.Name {
		case "string", "uintptr":
			return "pointer", nil
		case "uint64":
			return "uint64", nil
		case "int64":
			return "int64", nil
		case "int32":
			return "int32", nil
		case "uint32":
			return "uint32", nil
		case "uint8":
			return "uint8", nil
		case "bool":
			return "bool", nil
		}
	case *ast.StarExpr, *ast.ArrayType, *ast.FuncType:
		return "pointer", nil
	case *ast.SelectorExpr:
		if id, ok := e.X.(*ast.Ident); ok && id.Name == "unsafe" && e.Sel.Name == "Pointer" {
			return "pointer", nil
		}
	}
	return "", fmt.Errorf("unsupported Go type %s", types.ExprString(expr))
}

func cClass(typ string) string {
	typ = strings.ReplaceAll(typ, "const", " ")
	typ = strings.ReplaceAll(typ, "volatile", " ")
	typ = strings.Join(strings.Fields(typ), " ")

	switch {
	case strings.Contains(typ, "("):
		return "pointer" // function pointer
	case strings.Contains(typ, "*"):
		return "pointer"
	}

	switch typ {
	case "void":
		return "void"
	case "int", "int32_t", "LanceErrorCode", "LancePollStatus", "LanceMetadataUpdateMode":
		return "int32"
	case "LanceWaker", "LanceScanStatisticsCallback":
		return "pointer"
	case "uint64_t", "size_t":
		return "uint64"
	case "int64_t":
		return "int64"
	case "uint32_t":
		return "uint32"
	case "uint8_t":
		return "uint8"
	case "bool", "_Bool":
		return "bool"
	default:
		return "unknown(" + typ + ")"
	}
}

func cParamType(param string) string {
	fields := strings.Fields(param)
	if len(fields) > 1 {
		last := fields[len(fields)-1]
		if isIdent(last) {
			fields = fields[:len(fields)-1]
		}
	}
	return strings.Join(fields, " ")
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}
