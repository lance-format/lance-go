// Command check-abi-symbols verifies the purego bindings in internal/ffi
// against the lance-c C header from the commit pinned in that package.
//
// The header is not vendored. Export it from the pinned upstream checkout:
//
//	PINNED_COMMIT=$(sed -n 's/^[[:space:]]*upstreamCommit[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' internal/ffi/upstream.go)
//	test -n "$PINNED_COMMIT"
//	git -C /path/to/lance-c show "$PINNED_COMMIT:include/lance/lance.h" > lance.h
//	LANCE_C_HEADER="$PWD/lance.h" go -C internal/tools run ./check-abi-symbols
//
// The command runs from the internal/tools module directory. Pass absolute
// paths for -header and -root; relative values resolve under internal/tools
// rather than the caller's directory.
//
// It checks that every bound symbol exists, that Go and C agree on arity and
// on value/pointer classification, that every function field in the bindings
// struct has a symbol entry, and that the header bytes match the pinned
// SHA-256. For a split release it accepts the include/lance directory, verifies
// every public *.h file against an expected HEADER-MANIFEST or composite
// identity, and parses the union of all headers. Type-only headers remain part
// of the identity even when they contain no lance_* function declarations.
// Declaration attributes such as LANCE_API, LANCE_NODISCARD and
// LANCE_DEPRECATED(message) are removed before the return type is classified.
// Symbols the loader probes optionally are checked the same way when the
// pinned header declares them; their absence from the header or the loaded
// library stays allowed. It cannot detect a same-size integer type confusion
// beyond the signed and unsigned integer classes it models.
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

	"github.com/lance-format/lance-go/internal/tools/headeridentity"
	"github.com/lance-format/lance-go/internal/tools/reporoot"
)

func main() {
	headerPath := flag.String("header", os.Getenv("LANCE_C_HEADER"), "path to include/lance/lance.h or the include/lance directory from the pinned lance-c commit (absolute path; relative values resolve from the internal/tools module directory; default: $LANCE_C_HEADER)")
	headerManifest := flag.String("header-manifest", "", "expected HEADER-MANIFEST for a directory header set (absolute path; relative values resolve from the internal/tools module directory)")
	headerIdentity := flag.String("header-identity", "", "expected composite SHA-256 of a directory header set; defaults to upstreamHeaderIdentitySHA256 when the pin records one")
	root := flag.String("root", "", "lance-go repository root (absolute path; relative values resolve from the internal/tools module directory; default: discovered from the current directory)")
	flag.Parse()

	if *root == "" {
		discovered, err := reporoot.Find()
		if err != nil {
			fmt.Fprintf(os.Stderr, "check-abi-symbols: %v\n", err)
			os.Exit(1)
		}
		*root = discovered
	}

	if err := run(*root, headerInput{path: *headerPath, manifestPath: *headerManifest, identity: *headerIdentity}); err != nil {
		fmt.Fprintf(os.Stderr, "check-abi-symbols: %v\n", err)
		os.Exit(1)
	}
}

type pin struct {
	repo              string
	release           string
	commit            string
	headerSHA         string
	headerIdentitySHA string
}

type headerInput struct {
	path         string
	manifestPath string
	identity     string
}

type boundSymbol struct {
	name  string
	field string
}

type bindingsModel struct {
	fields   map[string]*ast.FuncType
	groups   map[string]map[string]*ast.FuncType
	nested   map[string]string
	symbols  []boundSymbol
	optional []boundSymbol
}

type cFunc struct {
	name   string
	result string
	params []string
}

func run(root string, input headerInput) error {
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
	if input.path == "" {
		return fmt.Errorf("no header supplied; export the pinned header and pass it with -header or LANCE_C_HEADER:\n  %s", headerHint)
	}
	info, err := os.Stat(input.path)
	if err != nil {
		return fmt.Errorf("inspect header input %s: %w", input.path, err)
	}

	if info.IsDir() {
		header, identity, count, err := checkHeaderDirectory(input, upstream)
		if err != nil {
			return err
		}
		if err := verify(upstream, model, header); err != nil {
			return err
		}
		optionalChecked, optionalAbsent := optionalCounts(model, header)
		fmt.Printf("check-abi-symbols: verified %d bindings and %d optional bindings%s against %d headers in %s (lance-c %s %s, header identity sha256 %s)\n",
			len(model.symbols), optionalChecked, optionalAbsentNote(optionalAbsent), count, input.path, upstream.release, upstream.commit, identity)
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("header input %s is not a regular file or directory", input.path)
	}
	if input.manifestPath != "" || input.identity != "" {
		return fmt.Errorf("-header-manifest and -header-identity require -header to name an include/lance directory")
	}

	data, err := os.ReadFile(input.path)
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	actualSHA := sha256Hex(data)
	if actualSHA != upstream.headerSHA {
		return fmt.Errorf("header sha256 is %s, want %s for lance-c %s (%s); export the pinned revision:\n  %s\nUpdate upstreamHeaderSHA256 only when re-pinning intentionally",
			actualSHA, upstream.headerSHA, upstream.release, upstream.commit, headerHint)
	}

	header, err := parseHeader(data)
	if err != nil {
		return fmt.Errorf("parse %s: %w", input.path, err)
	}
	if err := verify(upstream, model, header); err != nil {
		return err
	}

	optionalChecked, optionalAbsent := optionalCounts(model, header)
	fmt.Printf("check-abi-symbols: verified %d bindings and %d optional bindings%s against %s (lance-c %s %s, header sha256 %s)\n",
		len(model.symbols), optionalChecked, optionalAbsentNote(optionalAbsent), input.path, upstream.release, upstream.commit, upstream.headerSHA)
	return nil
}

// checkHeaderDirectory verifies a split include/lance directory against an
// independently supplied manifest or composite identity and parses the union
// of its headers. Type-only headers may contribute zero declarations, but a
// malformed declaration or a duplicate across files is still an error.
func checkHeaderDirectory(input headerInput, upstream *pin) (map[string]cFunc, string, int, error) {
	expectedIdentity := input.identity
	if expectedIdentity == "" {
		expectedIdentity = upstream.headerIdentitySHA
	}
	var expectedManifest []byte
	if input.manifestPath != "" {
		data, err := os.ReadFile(input.manifestPath)
		if err != nil {
			return nil, "", 0, fmt.Errorf("read expected header manifest %s: %w", input.manifestPath, err)
		}
		if _, err := headeridentity.Parse(data); err != nil {
			return nil, "", 0, fmt.Errorf("expected header manifest %s: %w", input.manifestPath, err)
		}
		manifestIdentity := sha256Hex(data)
		if expectedIdentity != "" && expectedIdentity != manifestIdentity {
			return nil, "", 0, fmt.Errorf("expected header manifest %s has sha256 %s, but the supplied identity is %s", input.manifestPath, manifestIdentity, expectedIdentity)
		}
		expectedIdentity = manifestIdentity
		expectedManifest = data
	}
	if expectedIdentity == "" {
		return nil, "", 0, fmt.Errorf("directory header set requires an expected identity; pass -header-identity or -header-manifest, or record upstreamHeaderIdentitySHA256 in the pin")
	}

	files, identity, err := headeridentity.Verify(input.path, expectedManifest, expectedIdentity)
	if err != nil {
		return nil, "", 0, err
	}
	union := make(map[string]cFunc)
	sources := make(map[string]string)
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(input.path, filepath.Base(file.Path)))
		if err != nil {
			return nil, "", 0, fmt.Errorf("read %s: %w", file.Path, err)
		}
		declarations, err := parseHeaderAllowEmpty(data)
		if err != nil {
			return nil, "", 0, fmt.Errorf("parse %s: %w", file.Path, err)
		}
		for name, declaration := range declarations {
			if previous, ok := sources[name]; ok {
				return nil, "", 0, fmt.Errorf("duplicate declaration of %s in %s and %s", name, previous, file.Path)
			}
			sources[name] = file.Path
			union[name] = declaration
		}
	}
	if len(union) == 0 {
		return nil, "", 0, fmt.Errorf("no lance_* function declarations found in the header set")
	}
	return union, identity, len(files), nil
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
	model := &bindingsModel{
		fields: make(map[string]*ast.FuncType),
		groups: make(map[string]map[string]*ast.FuncType),
		nested: make(map[string]string),
	}

	var filenames []string
	for name := range pkg.Files {
		filenames = append(filenames, name)
	}
	sort.Strings(filenames)

	constants := map[string]string{}

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
			if !ok {
				return true
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			name := typeSpec.Name.Name
			funcFields := make(map[string]*ast.FuncType)
			for _, field := range structType.Fields.List {
				if funcType, ok := field.Type.(*ast.FuncType); ok {
					for _, fieldName := range field.Names {
						funcFields[fieldName.Name] = funcType
					}
					continue
				}
				if name != "bindings" {
					continue
				}
				nestedType, ok := field.Type.(*ast.Ident)
				if !ok {
					continue
				}
				// A grouped declaration such as "first, second optionalGroup"
				// declares every name, and each is a separate instance path
				// that must be covered.
				for _, fieldName := range field.Names {
					model.nested[fieldName.Name] = nestedType.Name
				}
			}
			if len(funcFields) > 0 {
				model.groups[name] = funcFields
			}
			return true
		})
	}

	bindingsFields, ok := model.groups["bindings"]
	if !ok {
		return nil, nil, fmt.Errorf("type bindings not found in %s", dir)
	}
	model.fields = bindingsFields

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

	for _, filename := range filenames {
		optional, err := parseOptionalBindings(pkg.Files[filename])
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", filename, err)
		}
		model.optional = append(model.optional, optional...)
	}
	seenOptionalSymbols := make(map[string]bool, len(model.optional))
	seenOptionalFields := make(map[string]bool, len(model.optional))
	for _, sym := range model.optional {
		if seenOptionalSymbols[sym.name] {
			return nil, nil, fmt.Errorf("optional symbol %s is bound more than once across package files", sym.name)
		}
		seenOptionalSymbols[sym.name] = true
		if seenOptionalFields[sym.field] {
			return nil, nil, fmt.Errorf("optional field %s is registered more than once across package files", sym.field)
		}
		seenOptionalFields[sym.field] = true
	}

	upstream.repo = constants["upstreamRepo"]
	upstream.release = constants["upstreamRelease"]
	upstream.commit = constants["upstreamCommit"]
	upstream.headerSHA = constants["upstreamHeaderSHA256"]
	upstream.headerIdentitySHA = constants["upstreamHeaderIdentitySHA256"]
	if upstream.headerIdentitySHA != "" && !headeridentity.ValidDigest(upstream.headerIdentitySHA) {
		return nil, nil, fmt.Errorf("upstreamHeaderIdentitySHA256 %q is not a lowercase SHA-256", upstream.headerIdentitySHA)
	}

	if len(model.symbols) == 0 {
		return nil, nil, fmt.Errorf("no []symbol{{...}} binding list found in %s", dir)
	}
	return upstream, model, nil
}

// lookupGuard is one accepted guarded optional registration.
type lookupGuard struct {
	symbol string
	field  string
	call   *ast.CallExpr
}

// parseOptionalBindings finds the opportunistically resolved symbols in one
// package ffi file. They have the shape
//
//	if addr, err := lib.Lookup("lance_x"); err == nil {
//		purego.RegisterFunc(&b.group.field, addr)
//	}
//
// Every registration under the bindings value must be paired with its own
// successful lookup and address variable; an unguarded or duplicate
// registration, a failing lookup condition, or a different address argument is
// reported instead of being ignored.
func parseOptionalBindings(file *ast.File) ([]boundSymbol, error) {
	registrations := map[*ast.CallExpr]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if field, ok := registerFuncField(call); ok {
			registrations[call] = field
		}
		return true
	})

	var optional []boundSymbol
	seenSymbols := map[string]bool{}
	seenFields := map[string]bool{}
	matched := map[*ast.CallExpr]bool{}
	var parseErr error

	ast.Inspect(file, func(n ast.Node) bool {
		if parseErr != nil {
			return false
		}
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		guard, guardErr := parseLookupGuard(ifStmt, registrations)
		if guardErr != nil {
			parseErr = guardErr
			return false
		}
		if guard == nil {
			return true
		}
		if seenSymbols[guard.symbol] {
			parseErr = fmt.Errorf("optional symbol %s is bound more than once", guard.symbol)
			return false
		}
		seenSymbols[guard.symbol] = true
		if seenFields[guard.field] {
			parseErr = fmt.Errorf("optional field %s is registered more than once", guard.field)
			return false
		}
		seenFields[guard.field] = true
		matched[guard.call] = true
		optional = append(optional, boundSymbol{name: guard.symbol, field: guard.field})
		return true
	})
	if parseErr != nil {
		return nil, parseErr
	}

	for call, field := range registrations {
		if !matched[call] {
			return nil, fmt.Errorf("optional registration of %s is not guarded by a successful lib.Lookup", field)
		}
	}
	return optional, nil
}

// parseLookupGuard recognizes one successful optional registration and returns
// nil when ifStmt is not a lib.Lookup statement at all. A lib.Lookup statement
// that does not guard exactly one registration of the looked-up address is an
// error.
func parseLookupGuard(ifStmt *ast.IfStmt, registrations map[*ast.CallExpr]string) (*lookupGuard, error) {
	assign, ok := ifStmt.Init.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 2 || len(assign.Rhs) != 1 {
		return nil, nil
	}
	addrIdent, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return nil, nil
	}
	errIdent, ok := assign.Lhs[1].(*ast.Ident)
	if !ok {
		return nil, nil
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok {
		return nil, nil
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Lookup" || len(call.Args) != 1 {
		return nil, nil
	}
	literal, ok := call.Args[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return nil, nil
	}
	symbol, err := strconv.Unquote(literal.Value)
	if err != nil || !strings.HasPrefix(symbol, "lance_") {
		return nil, nil
	}
	if !isNilErrCondition(ifStmt.Cond, errIdent.Name) {
		return nil, fmt.Errorf("optional symbol %s is not guarded by a successful lib.Lookup (want %s == nil)", symbol, errIdent.Name)
	}

	// Only a directly paired registration is supported: the body must be one
	// purego.RegisterFunc statement using the address from the if-init. This
	// rejects nested blocks, extra statements and shadowed address bindings
	// that a deeper syntax walk could misread.
	if len(ifStmt.Body.List) != 1 {
		return nil, fmt.Errorf("optional symbol %s uses an unsupported registration form; want one purego.RegisterFunc statement", symbol)
	}
	exprStmt, ok := ifStmt.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return nil, fmt.Errorf("optional symbol %s uses an unsupported registration form; want one purego.RegisterFunc statement", symbol)
	}
	registration, ok := exprStmt.X.(*ast.CallExpr)
	if !ok {
		return nil, fmt.Errorf("optional symbol %s uses an unsupported registration form; want one purego.RegisterFunc statement", symbol)
	}
	field, ok := registrations[registration]
	if !ok {
		return nil, fmt.Errorf("optional symbol %s does not register a bindings field", symbol)
	}
	if !callArgIsIdent(registration, 1, addrIdent.Name) {
		return nil, fmt.Errorf("optional symbol %s registers %s from an address other than %s", symbol, field, addrIdent.Name)
	}
	return &lookupGuard{symbol: symbol, field: field, call: registration}, nil
}

// isNilErrCondition reports whether cond is "err == nil" in either order.
func isNilErrCondition(cond ast.Expr, errName string) bool {
	binary, ok := cond.(*ast.BinaryExpr)
	if !ok || binary.Op != token.EQL {
		return false
	}
	if isIdentNamed(binary.X, errName) && isNilExpr(binary.Y) {
		return true
	}
	return isIdentNamed(binary.Y, errName) && isNilExpr(binary.X)
}

func isIdentNamed(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

func isNilExpr(expr ast.Expr) bool {
	return isIdentNamed(expr, "nil")
}

// callArgIsIdent reports whether call.Args[index] is the named identifier.
func callArgIsIdent(call *ast.CallExpr, index int, name string) bool {
	if len(call.Args) <= index {
		return false
	}
	ident, ok := call.Args[index].(*ast.Ident)
	return ok && ident.Name == name
}

// registerFuncField returns "group.field" when call is
// purego.RegisterFunc(&b.<group>.<field>, ...).
func registerFuncField(call *ast.CallExpr) (string, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "purego" || selector.Sel.Name != "RegisterFunc" {
		return "", false
	}
	if len(call.Args) == 0 {
		return "", false
	}
	address, ok := call.Args[0].(*ast.UnaryExpr)
	if !ok || address.Op != token.AND {
		return "", false
	}
	fieldSelector, ok := address.X.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	groupSelector, ok := fieldSelector.X.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	binding, ok := groupSelector.X.(*ast.Ident)
	if !ok || binding.Name != "b" {
		return "", false
	}
	return groupSelector.Sel.Name + "." + fieldSelector.Sel.Name, true
}

var commentBlock = regexp.MustCompile(`(?s)/\*.*?\*/`)
var commentLine = regexp.MustCompile(`//[^\n]*`)
var lanceFunc = regexp.MustCompile(`\b(lance_[A-Za-z0-9_]+)[ \t\r\n]*\(`)

func parseHeader(src []byte) (map[string]cFunc, error) {
	funcs, err := parseHeaderAllowEmpty(src)
	if err != nil {
		return nil, err
	}
	if len(funcs) == 0 {
		return nil, fmt.Errorf("no lance_* function declarations found")
	}
	return funcs, nil
}

// parseHeaderAllowEmpty is parseHeader without the requirement that a file
// contributes at least one lance_* declaration. It exists for type-only
// headers such as fwd.h, which are part of the identity but contain no
// function declarations. Real parse errors still fail.
func parseHeaderAllowEmpty(src []byte) (map[string]cFunc, error) {
	text := stripComments(string(src))
	funcs := map[string]cFunc{}

	for _, loc := range lanceFunc.FindAllStringSubmatchIndex(text, -1) {
		name := text[loc[2]:loc[3]]
		nameStart := loc[2]
		lineStart := strings.LastIndexByte(text[:nameStart], '\n') + 1
		prefix := stripDeclarationAttributes(strings.TrimSpace(text[lineStart:nameStart]))

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
	return funcs, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// stripDeclarationAttributes removes the compile-time annotations lance-c
// places before a return type, so the remaining text is the type this tool
// classifies. LANCE_DEPRECATED carries an argument list that may contain
// parentheses and commas inside a quoted message.
//
// Headers without annotations, including the pinned v0.2.1-beta.3 header, pass
// through unchanged. An unknown or malformed attribute is left in place so the
// ABI check fails loudly instead of silently skipping the declaration.
func stripDeclarationAttributes(prefix string) string {
	for {
		prefix = strings.TrimLeft(prefix, " \t\r\n")
		switch {
		case hasAttributePrefix(prefix, "LANCE_API"):
			prefix = prefix[len("LANCE_API"):]
		case hasAttributePrefix(prefix, "LANCE_NODISCARD"):
			prefix = prefix[len("LANCE_NODISCARD"):]
		case hasAttributePrefix(prefix, "LANCE_DEPRECATED"):
			rest, ok := skipAttributeArguments(prefix[len("LANCE_DEPRECATED"):])
			if !ok {
				return prefix
			}
			prefix = rest
		default:
			return prefix
		}
	}
}

// hasAttributePrefix reports whether s starts with the attribute name followed
// by a boundary, so a longer identifier such as LANCE_APIX is not stripped.
func hasAttributePrefix(s, attribute string) bool {
	if !strings.HasPrefix(s, attribute) {
		return false
	}
	return len(s) == len(attribute) || !isIdentByte(s[len(attribute)])
}

// skipAttributeArguments consumes an attribute's parenthesized argument list
// and returns the text after it. Parentheses and commas inside a quoted or
// character literal do not close the list, so a deprecation message such as
// "Use lance_dataset_prepare_fts_match_query() instead" is kept together.
func skipAttributeArguments(s string) (string, bool) {
	s = strings.TrimLeft(s, " \t\r\n")
	if s == "" || s[0] != '(' {
		return "", false
	}

	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			switch c {
			case '\\':
				i++
			case quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[i+1:], true
			}
		}
	}
	return "", false
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
		if err := verifySignature(sym.name, sym.field, goFuncType, cFuncType); err != nil {
			return err
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
	return verifyOptional(model, header)
}

// verifySignature compares the C declaration of name with the Go function field
// that receives it.
func verifySignature(name, field string, goFuncType *ast.FuncType, cFuncType cFunc) error {
	goParams, goResult, err := describeGoFunc(goFuncType)
	if err != nil {
		return fmt.Errorf("bindings field %s (%s): %w", field, name, err)
	}
	cResult := cClass(cFuncType.result)
	if cResult != goResult {
		return fmt.Errorf("%s: C returns %q (%s), Go binding returns %s", name, cFuncType.result, cResult, goResult)
	}

	if len(goParams) != len(cFuncType.params) {
		return fmt.Errorf("%s: C takes %d parameter(s), Go binding takes %d", name, len(cFuncType.params), len(goParams))
	}
	for i, cParam := range cFuncType.params {
		cCls := cClass(cParamType(cParam))
		if cCls != goParams[i] {
			return fmt.Errorf("%s: parameter %d is %q (%s) in C but %s in the Go binding", name, i+1, cParam, cCls, goParams[i])
		}
	}
	return nil
}

// verifyOptional checks the symbols that the loader probes opportunistically.
// They stay out of the required symbol list and out of the mandatory symbol
// set, so a library that predates an optional ABI still loads. When the pinned
// header declares one, its signature must match the Go field; a header that
// predates it is allowed to omit it. Every function field of a nested group
// declared on bindings must have a registration, otherwise deleting the last
// registration of a group would silently remove its fields from the check.
func verifyOptional(model *bindingsModel, header map[string]cFunc) error {
	required := make(map[string]bool, len(model.symbols))
	for _, sym := range model.symbols {
		required[sym.name] = true
	}

	seen := make(map[string]bool, len(model.optional))
	covered := make(map[string]bool, len(model.optional))
	for _, sym := range model.optional {
		if required[sym.name] {
			return fmt.Errorf("symbol %s is bound both as required and as optional", sym.name)
		}
		if seen[sym.name] {
			return fmt.Errorf("optional symbol %s is bound more than once", sym.name)
		}
		seen[sym.name] = true

		groupField, fieldName, ok := strings.Cut(sym.field, ".")
		if !ok {
			return fmt.Errorf("optional symbol %s points at %s, which is not a nested function field", sym.name, sym.field)
		}
		typeName, ok := model.nested[groupField]
		if !ok {
			return fmt.Errorf("optional symbol %s points at %s, but %s is not a bindings field", sym.name, sym.field, groupField)
		}
		goFuncType, ok := model.groups[typeName][fieldName]
		if !ok {
			return fmt.Errorf("optional symbol %s points at bindings field %s, which is not a function field", sym.name, sym.field)
		}
		covered[sym.field] = true

		cFuncType, ok := header[sym.name]
		if !ok {
			continue
		}
		if err := verifySignature(sym.name, sym.field, goFuncType, cFuncType); err != nil {
			return err
		}
	}

	// Enumerate every declared bindings instance path instead of only the
	// groups that happened to register something. Otherwise deleting every
	// registration of a group would silently drop its fields from the check,
	// and two fields sharing a group type would cover each other.
	var uncovered []string
	for groupField, typeName := range model.nested {
		for fieldName := range model.groups[typeName] {
			path := groupField + "." + fieldName
			if !covered[path] {
				uncovered = append(uncovered, path)
			}
		}
	}
	if len(uncovered) > 0 {
		sort.Strings(uncovered)
		return fmt.Errorf("optional bindings do not cover function field(s) %s without a lib.Lookup binding", strings.Join(uncovered, ", "))
	}
	return nil
}

// optionalCounts splits the probed symbols into those whose signature the
// pinned header let us check and those absent from it.
func optionalCounts(model *bindingsModel, header map[string]cFunc) (checked, absent int) {
	for _, sym := range model.optional {
		if _, ok := header[sym.name]; ok {
			checked++
			continue
		}
		absent++
	}
	return checked, absent
}

// optionalAbsentNote describes optional bindings whose signatures could not be
// verified because the pinned header does not declare them.
func optionalAbsentNote(absent int) string {
	switch absent {
	case 0:
		return ""
	case 1:
		return " (1 optional binding absent from the header)"
	default:
		return fmt.Sprintf(" (%d optional bindings absent from the header)", absent)
	}
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
		case "uint16":
			return "uint16", nil
		case "uint8":
			return "uint8", nil
		case "float32":
			return "float32", nil
		case "float64":
			return "float64", nil
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
	case strings.Contains(typ, "["):
		return "pointer" // an array parameter decays to a pointer
	case strings.Contains(typ, "*"):
		return "pointer"
	}

	switch typ {
	case "void":
		return "void"
	case "int", "int32_t", "LanceErrorCode", "LancePollStatus", "LanceScalarIndexType", "LanceMetadataUpdateMode", "LanceDataType", "LanceMetricType", "LanceApproxMode", "LanceBlobHandling", "LanceMaterializationStyle":
		return "int32"
	case "LanceWaker", "LanceScanStatisticsCallback", "LanceIndexBuildProgressCallback":
		return "pointer"
	case "uint64_t", "size_t":
		return "uint64"
	case "int64_t":
		return "int64"
	case "uint32_t":
		return "uint32"
	case "uint16_t":
		return "uint16"
	case "uint8_t":
		return "uint8"
	case "float":
		return "float32"
	case "double":
		return "float64"
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

func isIdentByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
