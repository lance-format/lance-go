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
	upstreamRelease      = "v0.2.1-beta.1"
	upstreamCommit       = "ef465b43f79f8a8a6fd82938bdeeb3b28758b8ca"
	upstreamHeaderSHA256 = "5d59978a8ab60bbd43f5817b3cd6f340b9489655b2dcca610ef99b2959c4852f"
)
