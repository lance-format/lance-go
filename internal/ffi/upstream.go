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
	upstreamRepo                 = "https://github.com/lance-format/lance-c"
	upstreamRelease              = "v0.2.1-beta.4"
	upstreamCommit               = "2f80f2aa0c7fdb955e8cdc799c827c011617e05b"
	upstreamHeaderSHA256         = "652d14311b97bfee28adb910a544551ef315d875c8dcaf0ef3c709d26ef728ac"
	upstreamHeaderIdentitySHA256 = "d4515c1d0a416978df606c884b882cc1128572f62e094084ecdf650bd121efba"
)
