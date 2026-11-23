package compaction_test

import (
	"fmt"

	"github.com/lance-format/lance-go/compaction"
)

// This example builds options for Dataset.CompactFiles and
// Dataset.PlanCompaction. A pointer field sets an explicit value, while an
// Optional field distinguishes an unset value, an explicit native None, and a
// value including zero.
func ExampleOptions() {
	targetRows := uint64(1 << 18)
	options := &compaction.Options{
		// A non-nil pointer forwards the value; nil leaves the native default.
		TargetRowsPerFragment: &targetRows,
		// Some forwards an explicit value, including zero.
		CompactionMode: compaction.Some(compaction.ModeTryBinaryCopy),
		// None clears a native option whose default is Some(...).
		MaxOverlaysPerFragment: compaction.None[uint64](),
		// Fields left at the zero Optional stay unset, so native defaults apply.
	}

	// options is passed to dataset.CompactFiles(ctx, options) or
	// dataset.PlanCompaction(ctx, options).

	mode, hasMode := options.CompactionMode.Get()
	fmt.Println("compaction mode:", mode, hasMode)
	fmt.Println("max overlays is none:", options.MaxOverlaysPerFragment.IsNone())
	fmt.Println("num threads is set:", options.NumThreads.IsSet())
	// Output:
	// compaction mode: TryBinaryCopy true
	// max overlays is none: true
	// num threads is set: false
}
