// Package ffi binds the subset of the lance-c C ABI used by lance-go.
//
// Responsibility split: this package owns native calls, native error
// extraction and the purego bindings. Library discovery and loading belong to
// internal/loader; Arrow C Data structures will belong to internal/cdata.
//
// Threading contract: lance-c stores its last error in Rust thread-local
// storage. Every native call that can set an error must run while the
// goroutine is locked to its OS thread, and lance_last_error_code must be read
// before lance_last_error_message. Reading the message takes the pending error
// out of TLS, so a later code read would incorrectly report success. The
// returned message is owned by lance-c and must be copied and then released
// with lance_free_string; reading it after the free would be a use-after-free.
//
// Only the minimal dataset open/close surface is bound in this foundation
// slice. Do not add empty bindings or public stubs ahead of the features that
// use them.
package ffi
