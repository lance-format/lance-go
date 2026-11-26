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
	upstreamRelease              = "v0.2.1-beta.7"
	upstreamCommit               = "b7c10e3df23d3596eddb5e7c2b0cffef442c2fce"
	upstreamHeaderSHA256         = "652d14311b97bfee28adb910a544551ef315d875c8dcaf0ef3c709d26ef728ac"
	upstreamHeaderIdentitySHA256 = "68b4f0108cf5d537db51814d0b3fefd46ce5f100789d8e2ac8f049f1f62d15fa"
)
