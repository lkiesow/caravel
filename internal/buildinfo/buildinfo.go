// Package buildinfo carries the identity of the running binary, so any
// question of the form "which build am I actually talking to?" has an answer
// the binary itself gives.
//
// It exists because `make dev-marker` (Stage 08 Milestone 3) only answers that
// question if you can supply a marker string the code actually uses — and an
// unused Go const is folded away and never reaches the binary, so inventing a
// marker per test is both fiddly and easy to get wrong. A version stamped in at
// link time is always there.
package buildinfo

// Version identifies the build. The Makefile stamps it at link time:
//
//	go build -ldflags "-X caravel/internal/buildinfo.Version=$(VERSION)"
//
// where VERSION is the short git SHA plus "-dirty" when the tree has
// uncommitted changes. It stays "dev" for a plain `go build`/`go run` with no
// ldflags, which is honest rather than broken: that build's identity genuinely
// isn't known.
//
// The "-dirty" suffix pins the commit a build came from but cannot distinguish
// one uncommitted state from another, so `make dev-marker` is still the tool
// for "is my specific edit in this binary".
var Version = "dev"
