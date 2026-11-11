package lance_test

import (
	"errors"
	"testing"

	lance "github.com/lance-format/lance-go"
)

func TestErrorSentinels(t *testing.T) {
	tests := []struct {
		code lance.Code
		want error
	}{
		{lance.CodeInvalidArgument, lance.ErrInvalidArgument},
		{lance.CodeIO, lance.ErrIO},
		{lance.CodeNotFound, lance.ErrNotFound},
		{lance.CodeAlreadyExists, lance.ErrAlreadyExists},
		{lance.CodeIndex, lance.ErrIndex},
		{lance.CodeInternal, lance.ErrInternal},
		{lance.CodeNotSupported, lance.ErrNotSupported},
		{lance.CodeCommitConflict, lance.ErrCommitConflict},
		{lance.CodePanic, lance.ErrPanic},
	}
	for _, tt := range tests {
		err := error(&lance.Error{Op: "open", URI: "/tmp/x", Code: tt.code})
		if !errors.Is(err, tt.want) {
			t.Errorf("errors.Is(%v, %v) = false", err, tt.want)
		}
	}
}
