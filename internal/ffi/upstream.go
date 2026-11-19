package ffi

// Upstream lance-c revision this package is written against.
//
// upstreamRelease names the official release whose archives provide the
// shared library for current releases. CI and release packaging acquire those
// published bytes for this release and require the header recorded below;
// local development may instead build upstreamCommit from source with the same
// header.
//
// There is no version symbol in the pinned lance-c ABI, so these values cannot
// be verified at runtime. They document intent and give the release
// acquisition tool, internal/tools/check-abi-symbols, and
// scripts/build-lance-c.sh a single source of truth.
const (
	upstreamRepo         = "https://github.com/lance-format/lance-c"
	upstreamRelease      = "v0.2.0"
	upstreamCommit       = "facb369c9fa98b7a3e5ce2ed12e2d8be090627ce"
	upstreamHeaderSHA256 = "947a912a14cb53f97237dbc7c03e5c09ef4337d46b459edc600d4d3c2c888d5c"
)
