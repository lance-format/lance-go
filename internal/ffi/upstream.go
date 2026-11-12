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
	upstreamCommit       = "ac2634c553b450c7787577ffe964fc2b283b8334"
	upstreamHeaderSHA256 = "c19789e9e003930c6065181b5cd9f6f7b464e08a5dc4daca8b21ad82d5160f5b"
)
