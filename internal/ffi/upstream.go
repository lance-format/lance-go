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
	upstreamCommit       = "88c07cf8ccafebf579e769416251be370b45180e"
	upstreamHeaderSHA256 = "c23406d5c683133eb647cfeb150fa05182ffc8ba7da283b3902dbb4bf0926c6c"
)
