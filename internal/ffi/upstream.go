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
	upstreamRelease      = "v0.2.0-beta.1"
	upstreamCommit       = "e2564b89be36bb18f5beff6d673daf9dfff8c986"
	upstreamHeaderSHA256 = "90e52d19f7cedb6cd52a83024927453f0ded5f13e24ed2adae8684cd9d4b0d8d"
)
