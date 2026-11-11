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
	upstreamCommit       = "07cd7d0cd06d65c5c643972267cc7981decef067"
	upstreamHeaderSHA256 = "12981740fff3c240c7a5f3d46d8b6cd85e64a848d2bf421b1846286b774fbc4b"
)
