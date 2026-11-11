// Package bundled activates the compiled-in Lance native library selectors.
//
// The package has no API. Import it for side effects from every public entry
// point that can load the native library without requiring a root-package
// import, such as the namespace package. Platform selector files are generated
// by scripts/sync-lance-c-gen and copied here.
package bundled
