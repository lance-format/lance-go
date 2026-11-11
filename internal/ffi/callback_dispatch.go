package ffi

import (
	"fmt"
	"unsafe"

	"github.com/lance-format/lance-go/internal/handle"
)

// status3Dispatcher handles one of the static callbacks with the
// (ctx, payload, error) -> status ABI. force distinguishes the two ABI-identical
// trampolines: storage-options force fetch and complete-fragment progress.
// Each owner decides what force means for its own domain.
type status3Dispatcher interface {
	dispatchStatus3(force bool, payload unsafe.Pointer, outError *callbackError) int32
}

// void2Dispatcher handles one of the static callbacks with the
// (ctx, payload) -> void ABI. Scanner statistics and write statistics share
// this trampoline while keeping their decoding in separate owners.
type void2Dispatcher interface {
	dispatchVoid2(payload unsafe.Pointer)
}

// dispatchStatus3 routes an ABI-identical status callback through the integer
// handle registry. Panics are contained before they can cross the C ABI and
// are reported through the callback error channel.
func dispatchStatus3(ctx uintptr, force bool, payload unsafe.Pointer, outError *callbackError) (ret int32) {
	clearCallbackError(outError)
	defer func() {
		if recovered := recover(); recovered != nil {
			setCallbackError(outError, fmt.Errorf("native callback panic"))
			ret = 1
		}
	}()
	if ctx == 0 {
		setCallbackError(outError, fmt.Errorf("native callback context is NULL"))
		return 1
	}
	value, ok := handle.Get(uint64(ctx))
	if !ok {
		setCallbackError(outError, fmt.Errorf("native callback context is no longer registered"))
		return 1
	}
	target, ok := value.(status3Dispatcher)
	if !ok {
		setCallbackError(outError, fmt.Errorf("native callback context has the wrong type"))
		return 1
	}
	return target.dispatchStatus3(force, payload, outError)
}

// dispatchVoid2 routes an ABI-identical void callback through the integer
// handle registry. It has no error channel, so invalid contexts and panics are
// ignored rather than allowed to unwind through native frames.
func dispatchVoid2(ctx uintptr, payload unsafe.Pointer) {
	defer func() {
		_ = recover()
	}()
	if ctx == 0 {
		return
	}
	value, ok := handle.Get(uint64(ctx))
	if !ok {
		return
	}
	target, ok := value.(void2Dispatcher)
	if !ok {
		return
	}
	target.dispatchVoid2(payload)
}
