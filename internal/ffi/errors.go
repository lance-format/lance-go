package ffi

import "errors"

// ErrClosed reports use of a native handle after it was closed.
var ErrClosed = errors.New("ffi: native handle is closed")
