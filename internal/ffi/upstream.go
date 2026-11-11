package ffi

// Upstream lance-c revision this package is written against.
//
// upstreamRelease is the most recent release tag at the time of the pin. The
// release tag is informational; the expanded bindings require a source build
// from upstreamCommit with the exact header recorded below.
//
// There is no version symbol in the pinned lance-c ABI, so these values cannot
// be verified at runtime. They
// document intent and give scripts/check-abi-symbols.go and
// scripts/build-lance-c.sh a single source of truth.
const (
	upstreamRepo         = "https://github.com/lance-format/lance-c"
	upstreamRelease      = "v0.1.9"
	upstreamCommit       = "407a751a9cfcaa0e868c005ab7a78ee9be599a31"
	upstreamHeaderSHA256 = "2bcbf8d121ccae8b20768c4bf3cf3988b653559f47c96831ceb6ea32d66df6ae"
)
