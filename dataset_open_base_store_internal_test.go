package lance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lance-format/lance-go/internal/loader"
)

func TestOpenWithBaseStoreParamsValidatesAndOwnsAccessors(t *testing.T) {
	t.Run("snapshot", func(t *testing.T) {
		blockSize := uint64(4096)
		refresh := 90*time.Second + 500*time.Millisecond
		useConstant := false
		lexical := false
		params := map[string]BaseStoreParams{
			"exact/base": {
				BlockSize:                  &blockSize,
				S3CredentialsRefreshOffset: &refresh,
				UseConstantSizeUploadParts: &useConstant,
				ListIsLexicallyOrdered:     &lexical,
			},
		}
		config := openConfig{}
		if err := WithBaseStoreParams(params)(&config); err != nil {
			t.Fatalf("WithBaseStoreParams: %v", err)
		}
		if config.baseStoreParams == nil {
			t.Fatal("non-nil base store params became native None")
		}

		blockSize = 1
		refresh = 0
		useConstant = true
		lexical = true
		delete(params, "exact/base")
		params["later"] = BaseStoreParams{}

		if len(config.baseStoreParams) != 1 {
			t.Fatalf("snapshot map length = %d, want 1", len(config.baseStoreParams))
		}
		got, ok := config.baseStoreParams["exact/base"]
		if !ok {
			t.Fatal("snapshot lost the original key")
		}
		if got.BlockSize == nil || *got.BlockSize != 4096 {
			t.Fatalf("snapshot block size = %v", got.BlockSize)
		}
		if got.S3CredentialsRefreshOffset == nil || *got.S3CredentialsRefreshOffset != 90*time.Second+500*time.Millisecond {
			t.Fatalf("snapshot refresh offset = %v", got.S3CredentialsRefreshOffset)
		}
		if got.UseConstantSizeUploadParts == nil || *got.UseConstantSizeUploadParts {
			t.Fatalf("snapshot constant upload parts = %v", got.UseConstantSizeUploadParts)
		}
		if got.ListIsLexicallyOrdered == nil || *got.ListIsLexicallyOrdered {
			t.Fatalf("snapshot lexical ordering = %v", got.ListIsLexicallyOrdered)
		}
	})

	t.Run("nil and empty presence", func(t *testing.T) {
		nilConfig := openConfig{}
		if err := WithBaseStoreParams(nil)(&nilConfig); err != nil {
			t.Fatalf("WithBaseStoreParams(nil): %v", err)
		}
		if nilConfig.baseStoreParams != nil {
			t.Fatal("nil base store params did not preserve native None")
		}

		emptyConfig := openConfig{}
		if err := WithBaseStoreParams(map[string]BaseStoreParams{})(&emptyConfig); err != nil {
			t.Fatalf("WithBaseStoreParams(empty): %v", err)
		}
		if emptyConfig.baseStoreParams == nil || len(emptyConfig.baseStoreParams) != 0 {
			t.Fatalf("empty base store params = %v, want present empty", emptyConfig.baseStoreParams)
		}
	})

	t.Run("validation", func(t *testing.T) {
		negative := -time.Second
		for _, tc := range []struct {
			name   string
			params map[string]BaseStoreParams
			want   string
		}{
			{
				name:   "embedded NUL path",
				params: map[string]BaseStoreParams{"bad\x00path": {}},
				want:   "embedded NUL",
			},
			{
				name:   "negative refresh offset",
				params: map[string]BaseStoreParams{"base": {S3CredentialsRefreshOffset: &negative}},
				want:   "must not be negative",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				err := WithBaseStoreParams(tc.params)(&openConfig{})
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v, want containing %q", err, tc.want)
				}
			})
		}
	})

	t.Run("closed accessor", func(t *testing.T) {
		_, err := Open(context.Background(), "file:///unused", WithBaseStoreParams(map[string]BaseStoreParams{
			"base": {StorageOptionsAccessor: &StorageOptionsAccessor{}},
		}))
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("closed accessor open = %v, want ErrClosed", err)
		}
	})

	t.Run("partial acquisition release", func(t *testing.T) {
		if _, err := loader.Load(); err != nil {
			if errors.Is(err, loader.ErrNotFound) {
				t.Skipf("lance-c is not available: %v", err)
			}
			t.Fatalf("load lance-c: %v", err)
		}
		good, err := NewStorageOptionsAccessor(nil, nil)
		if err != nil {
			t.Fatalf("NewStorageOptionsAccessor: %v", err)
		}
		_, err = Open(context.Background(), "file:///unused", WithBaseStoreParams(map[string]BaseStoreParams{
			"a-good":   {StorageOptionsAccessor: good},
			"z-closed": {StorageOptionsAccessor: &StorageOptionsAccessor{}},
		}))
		if !errors.Is(err, ErrClosed) {
			good.Close()
			t.Fatalf("partial acquisition open = %v, want ErrClosed", err)
		}

		done := make(chan struct{})
		go func() {
			good.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("accessor close blocked after partial acquisition failure")
		}
	})
}

func TestOpenWithBaseStoreParamsRejectsCommitLock(t *testing.T) {
	_, err := Open(context.Background(), "file:///unused",
		WithOpenCommitLock(&CommitLock{}),
		WithBaseStoreParams(map[string]BaseStoreParams{}),
	)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("base store params with commit lock = %v, want ErrInvalidArgument", err)
	}
	if !strings.Contains(err.Error(), "WithBaseStoreParams") || !strings.Contains(err.Error(), "WithOpenCommitLock") {
		t.Fatalf("combination error = %v, want both option names", err)
	}

	_, err = Open(context.Background(), "file:///unused",
		WithOpenCommitLock(&CommitLock{}),
		WithBaseStoreParams(nil),
	)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("nil base store params with commit lock = %v, want ordinary closed lock error", err)
	}
}

func TestOpenFromNamespaceRejectsBaseStoreParams(t *testing.T) {
	_, err := OpenFromNamespace(context.Background(), nil, []string{"table"},
		WithBaseStoreParams(map[string]BaseStoreParams{}),
	)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("namespace base store params = %v, want ErrInvalidArgument", err)
	}
	if !strings.Contains(err.Error(), "WithBaseStoreParams") {
		t.Fatalf("namespace rejection = %v, want option name", err)
	}

	_, err = OpenFromNamespace(context.Background(), nil, []string{"table"},
		WithBaseStoreParams(nil),
	)
	if err == nil || !strings.Contains(err.Error(), "namespace must not be nil") {
		t.Fatalf("nil namespace base store params = %v, want ordinary namespace validation", err)
	}
}
