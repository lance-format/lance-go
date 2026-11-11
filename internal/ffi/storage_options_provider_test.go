package ffi

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/lance-format/lance-go/internal/handle"
)

func fakeStorageProviderBindings(specOut *storageOptionsProviderSpec, idOut *string) *bindings {
	return &bindings{
		storageProviderNew: func(spec *storageOptionsProviderSpec) uintptr {
			*specOut = *spec
			if idOut != nil && spec.ProviderID != nil {
				*idOut = goString(spec.ProviderID)
			}
			return 1
		},
		storageProviderClose: func(uintptr) {},
		lastErrorCode:        func() int32 { return 0 },
		lastErrorMessage:     func() *byte { return nil },
	}
}

func releaseProviderContext(t *testing.T, ctx uintptr) {
	t.Helper()
	if ctx == 0 {
		return
	}
	value, ok := handle.Take(uint64(ctx))
	if !ok {
		return
	}
	if releaser, ok := value.(contextReleaser); ok {
		releaser.releaseContext()
	}
}

func TestStorageOptionsProviderConstructorFailureReclaimsContext(t *testing.T) {
	before := handle.Len()
	b := &bindings{
		storageProviderNew: func(*storageOptionsProviderSpec) uintptr { return 0 },
		lastErrorCode:      func() int32 { return int32(CodeInvalidInput) },
		lastErrorMessage:   func() *byte { return nil },
	}
	_, err := newStorageOptionsProvider(b, "provider/failure", func(context.Context, bool) (map[string]string, error) {
		return nil, nil
	}, nil)
	if err == nil {
		t.Fatal("provider constructor succeeded, want error")
	}
	if got := handle.Len(); got != before {
		t.Fatalf("handle count after failed provider construction = %d, want %d", got, before)
	}
}

func TestStorageOptionsProviderCallbacksPresenceAndRelease(t *testing.T) {
	var spec storageOptionsProviderSpec
	var capturedID string
	b := fakeStorageProviderBindings(&spec, &capturedID)
	fetch := func(context.Context, bool) (map[string]string, error) {
		return map[string]string{"region": "west", "allow_http": "true"}, nil
	}
	provider, err := newStorageOptionsProvider(b, "provider/callbacks", fetch, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	defer releaseProviderContext(t, spec.ProviderCtx)
	if spec.Fetch == 0 || spec.Release == 0 || spec.ForceFetch != 0 {
		t.Fatalf("unexpected provider spec: %+v", spec)
	}
	if capturedID != "provider/callbacks" {
		t.Fatalf("provider id = %q", capturedID)
	}

	var out storageOptionsFetchResult
	var callbackErr callbackError
	if rc := storageOptionsFetchCallback(spec.ProviderCtx, &out, &callbackErr); rc != 0 {
		t.Fatalf("fetch callback rc = %d, error = %+v", rc, callbackErr)
	}
	if out.HasOptions != 1 || out.Count != 2 || out.SnapshotCtx == 0 || out.ReleaseSnapshot == 0 {
		t.Fatalf("fetch result = %+v", out)
	}
	runtime.GC()
	keys := unsafe.Slice(out.Keys, out.Count)
	values := unsafe.Slice(out.Values, out.Count)
	got := map[string]string{}
	for i := range keys {
		got[goString(keys[i])] = goString(values[i])
	}
	if got["region"] != "west" || got["allow_http"] != "true" {
		t.Fatalf("fetched options after GC = %v", got)
	}

	contextReleaseCallback(out.SnapshotCtx)
	if _, ok := handle.Get(uint64(out.SnapshotCtx)); ok {
		t.Fatal("snapshot context still registered after release")
	}
	// A second release must be a harmless no-op, not a double free.
	contextReleaseCallback(out.SnapshotCtx)
}

func TestStorageOptionsProviderForceCallbackWiring(t *testing.T) {
	var spec storageOptionsProviderSpec
	b := fakeStorageProviderBindings(&spec, nil)
	forceCalls := 0
	provider, err := newStorageOptionsProvider(b, "provider/force", func(context.Context, bool) (map[string]string, error) {
		return map[string]string{"source": "fetch"}, nil
	}, func(context.Context, bool) (map[string]string, error) {
		forceCalls++
		return map[string]string{"source": "force"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	defer releaseProviderContext(t, spec.ProviderCtx)
	if spec.ForceFetch == 0 {
		t.Fatal("force callback was not installed")
	}

	var out storageOptionsFetchResult
	var callbackErr callbackError
	if rc := storageOptionsForceFetchCallback(spec.ProviderCtx, &out, &callbackErr); rc != 0 {
		t.Fatalf("force callback rc = %d, error = %+v", rc, callbackErr)
	}
	if forceCalls != 1 || out.HasOptions != 1 || out.Count != 1 {
		t.Fatalf("force calls = %d, result = %+v", forceCalls, out)
	}
	contextReleaseCallback(out.SnapshotCtx)
}

func TestStorageOptionsProviderNativeID(t *testing.T) {
	var spec storageOptionsProviderSpec
	var capturedID string
	b := fakeStorageProviderBindings(&spec, &capturedID)
	b.storageProviderID = func(uintptr) *byte {
		return newOwnedString("native/provider").Data
	}
	b.freeString = func(ptr *byte) {
		ownedStringReleaseCallback(&ownedString{Data: ptr})
	}
	provider, err := newStorageOptionsProvider(b, "provider/native-id", func(context.Context, bool) (map[string]string, error) {
		return nil, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	defer releaseProviderContext(t, spec.ProviderCtx)

	id, err := provider.NativeID()
	if err != nil {
		t.Fatal(err)
	}
	if id != "native/provider" {
		t.Fatalf("native provider id = %q", id)
	}
}

func TestStorageOptionsFetchCallbackNilAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name      string
		options   map[string]string
		wantHas   int32
		wantCtx   bool
		wantCount uint64
	}{
		{name: "nil", options: nil, wantHas: 0, wantCtx: false},
		{name: "empty", options: map[string]string{}, wantHas: 1, wantCtx: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &storageOptionsProviderAdapter{fetch: func(context.Context, bool) (map[string]string, error) {
				return tc.options, nil
			}}
			ctx := handle.New(adapter)
			defer handle.Take(ctx)
			var out storageOptionsFetchResult
			var callbackErr callbackError
			if rc := storageOptionsFetchCallback(ctx, &out, &callbackErr); rc != 0 {
				t.Fatalf("callback rc = %d, error = %+v", rc, callbackErr)
			}
			if out.HasOptions != tc.wantHas || out.Count != tc.wantCount || (out.SnapshotCtx != 0) != tc.wantCtx {
				t.Fatalf("result = %+v, want has=%d count=%d ctx=%v", out, tc.wantHas, tc.wantCount, tc.wantCtx)
			}
			if out.SnapshotCtx != 0 {
				contextReleaseCallback(out.SnapshotCtx)
			}
		})
	}
}

func TestStorageOptionsFetchCallbackForceFallbackAndPanic(t *testing.T) {
	fetchCalls, forceCalls := 0, 0
	adapter := &storageOptionsProviderAdapter{
		fetch: func(context.Context, bool) (map[string]string, error) {
			fetchCalls++
			return map[string]string{"source": "fetch"}, nil
		},
		force: func(context.Context, bool) (map[string]string, error) {
			forceCalls++
			return map[string]string{"source": "force"}, nil
		},
	}
	ctx := handle.New(adapter)
	defer handle.Take(ctx)

	var out storageOptionsFetchResult
	var callbackErr callbackError
	if rc := storageOptionsFetchCallback(ctx, &out, &callbackErr); rc != 0 {
		t.Fatalf("fetch rc = %d, error = %+v", rc, callbackErr)
	}
	if fetchCalls != 1 || forceCalls != 0 {
		t.Fatalf("fetch/force calls = %d/%d", fetchCalls, forceCalls)
	}
	contextReleaseCallback(out.SnapshotCtx)

	if rc := storageOptionsForceFetchCallback(ctx, &out, &callbackErr); rc != 0 {
		t.Fatalf("force fetch rc = %d, error = %+v", rc, callbackErr)
	}
	if fetchCalls != 1 || forceCalls != 1 {
		t.Fatalf("fetch/force calls after force = %d/%d", fetchCalls, forceCalls)
	}
	if out.SnapshotCtx != 0 {
		contextReleaseCallback(out.SnapshotCtx)
	}

	fallbackCalls := 0
	fallbackCtx := handle.New(&storageOptionsProviderAdapter{fetch: func(context.Context, bool) (map[string]string, error) {
		fallbackCalls++
		return map[string]string{"source": "fallback"}, nil
	}})
	defer handle.Take(fallbackCtx)
	if rc := storageOptionsForceFetchCallback(fallbackCtx, &out, &callbackErr); rc != 0 {
		t.Fatalf("force fallback rc = %d, error = %+v", rc, callbackErr)
	}
	if fallbackCalls != 1 {
		t.Fatalf("force fallback calls = %d, want 1", fallbackCalls)
	}
	if out.SnapshotCtx != 0 {
		contextReleaseCallback(out.SnapshotCtx)
	}

	panicCtx := handle.New(&storageOptionsProviderAdapter{fetch: func(context.Context, bool) (map[string]string, error) {
		panic("provider exploded")
	}})
	defer handle.Take(panicCtx)
	out = storageOptionsFetchResult{HasOptions: 1, Count: 99, SnapshotCtx: 123}
	callbackErr = callbackError{}
	if rc := storageOptionsFetchCallback(panicCtx, &out, &callbackErr); rc == 0 {
		t.Fatal("panic callback succeeded")
	}
	if out.HasOptions != 0 || out.Count != 0 || out.SnapshotCtx != 0 || out.ReleaseSnapshot != 0 {
		t.Fatalf("panic left partial output: %+v", out)
	}
	if callbackErr.Code == 0 || !strings.Contains(goString(callbackErr.Message.Data), "panic") {
		t.Fatalf("panic callback error = %+v", callbackErr)
	}
}

func TestStorageOptionsFetchCallbackTypedErrorAndValidation(t *testing.T) {
	typed := &testCallbackCodeError{code: CodeInvalidInput}
	for _, tc := range []struct {
		name string
		err  error
		opts map[string]string
		want Code
	}{
		{name: "typed", err: typed, want: CodeInvalidInput},
		{name: "plain", err: errors.New("no credentials"), want: CodeInternal},
		{name: "nul key", opts: map[string]string{"bad\x00key": "v"}, want: CodeInvalidInput},
		{name: "nul value", opts: map[string]string{"key": "bad\x00value"}, want: CodeInvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &storageOptionsProviderAdapter{fetch: func(context.Context, bool) (map[string]string, error) {
				return tc.opts, tc.err
			}}
			ctx := handle.New(adapter)
			defer handle.Take(ctx)
			var out storageOptionsFetchResult
			var callbackErr callbackError
			if rc := storageOptionsFetchCallback(ctx, &out, &callbackErr); rc == 0 {
				t.Fatal("callback succeeded, want error")
			}
			if Code(callbackErr.Code) != tc.want {
				t.Fatalf("error code = %d, want %d (%s)", callbackErr.Code, tc.want, goString(callbackErr.Message.Data))
			}
			if out.HasOptions != 0 || out.SnapshotCtx != 0 {
				t.Fatalf("error left partial output: %+v", out)
			}
		})
	}
}

type testCallbackCodeError struct{ code Code }

func (e *testCallbackCodeError) Error() string    { return fmt.Sprintf("callback error %d", e.code) }
func (e *testCallbackCodeError) ErrorCode() int32 { return int32(e.code) }

func TestContextReleaseCallbackExactlyOnce(t *testing.T) {
	releaser := &countingReleaser{}
	ctx := handle.New(releaser)
	contextReleaseCallback(ctx)
	contextReleaseCallback(ctx)
	if releaser.count != 1 {
		t.Fatalf("release count = %d, want 1", releaser.count)
	}
}

type countingReleaser struct{ count int }

func (c *countingReleaser) releaseContext() { c.count++ }

func TestStorageOptionsAccessorSnapshotPartialErrorClosesSnapshot(t *testing.T) {
	closes := 0
	b := &bindings{
		storageAccessorSnapshot:   func(uintptr) uintptr { return 7 },
		storageSnapshotHasOptions: func(uintptr) int32 { return -1 },
		storageSnapshotClose:      func(uintptr) { closes++ },
		lastErrorCode:             func() int32 { return int32(CodeInvalidInput) },
		lastErrorMessage:          func() *byte { return nil },
	}
	accessor := &StorageOptionsAccessor{b: b, handle: 1}
	if _, _, err := accessor.Snapshot(); err == nil {
		t.Fatal("snapshot succeeded, want error")
	}
	if closes != 1 {
		t.Fatalf("snapshot close count = %d, want 1", closes)
	}
}
