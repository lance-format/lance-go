// Package file provides the raw Lance file API.
//
// The package reads and writes individual .lance files. Dataset operations
// live in the root lance package. Importing file initializes the default
// native bundle through that root dependency, including in CGO-free builds.
package file

import (
	"errors"
	"fmt"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/internal/ffi"
)

func wrapError(op, uri string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ffi.ErrClosed) {
		return closedError(op, uri)
	}
	var native *ffi.Error
	if errors.As(err, &native) {
		code := lance.Code(native.Code)
		if native.Code == ffi.CodeDatasetAlreadyExists {
			code = lance.CodeAlreadyExists
		}
		return &lance.Error{Op: op, URI: uri, Code: code, Message: native.Message}
	}
	if uri == "" {
		return fmt.Errorf("lance: %s: %w", op, err)
	}
	return fmt.Errorf("lance: %s %s: %w", op, uri, err)
}

func invalidError(op, uri, format string, args ...any) error {
	return &lance.Error{Op: op, URI: uri, Code: lance.CodeInvalidArgument, Message: fmt.Sprintf(format, args...)}
}

func closedError(op, uri string) error {
	if uri == "" {
		return fmt.Errorf("lance: %s: %w", op, lance.ErrClosed)
	}
	return fmt.Errorf("lance: %s %s: %w", op, uri, lance.ErrClosed)
}
