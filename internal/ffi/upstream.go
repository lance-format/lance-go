package ffi

// Upstream lance-c revision this package is written against.
//
// upstreamRelease names the official release whose archives provide the
// shared library for current releases. CI and release packaging acquire those
// published bytes for this release and require the header recorded below;
// local development may instead build upstreamCommit from source with the same
// header.
//
// The pinned lance-c ABI reports only its binding release through
// lance_c_version(), not this commit or header, so these values cannot be
// verified at runtime. They document intent and give the release
// acquisition tool, internal/tools/check-abi-symbols, and
// scripts/build-lance-c.sh a single source of truth.
const (
	upstreamRepo         = "https://github.com/lance-format/lance-c"
	upstreamRelease      = "v0.2.1-beta.3"
	upstreamCommit       = "a800fd5f28636bfff34778e3ad6663f3cf099a38"
	upstreamHeaderSHA256 = "6bab6899ce240b93cf47a1c93f7fb4c8bb2b20ba0a82e3c4a0ec04e7bfc915ad"
)
