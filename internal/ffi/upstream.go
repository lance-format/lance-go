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
	upstreamRelease              = "v0.2.1-beta.5"
	upstreamCommit               = "7482a15c57da79bc138e221a1c122701f7304205"
	upstreamHeaderSHA256         = "652d14311b97bfee28adb910a544551ef315d875c8dcaf0ef3c709d26ef728ac"
	upstreamHeaderIdentitySHA256 = "989c34df5e9a1032f459d98eb2dc50b9a9c7165eba40c325ea5d68c670ca0149"
)
