package ffi

// Upstream lance-c revision this package is written against.
//
// upstreamRelease is the most recent release tag at the time of the pin. The
// pin is 60 commits past that tag, but every symbol bound here has the same
// signature in both revisions, so a v0.1.9 release build is suitable for
// smoke tests while a source build from upstreamCommit is authoritative.
//
// There is no version symbol in lance-c yet (tracked upstream as
// lance-format/lance-c#165), so these values cannot be verified at runtime. They
// document intent and give scripts/check-abi-symbols.go and
// scripts/build-lance-c.sh a single source of truth.
const (
	upstreamRepo         = "https://github.com/lance-format/lance-c"
	upstreamRelease      = "v0.1.9"
	upstreamCommit       = "875c2569ac3467b56a721db0cf9e4dff9b4b2cc5"
	upstreamHeaderSHA256 = "58d86926a3903b68e4a33613367109ff71fd87f3526e7a7392e416acdac0a8eb"
)
