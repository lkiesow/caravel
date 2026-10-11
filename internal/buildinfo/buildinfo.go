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

import "regexp"

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

// RepoURL is the project's home on GitHub, which URL builds its links on.
const RepoURL = "https://github.com/lkiesow/caravel"

var (
	releaseVersion = regexp.MustCompile(`^\d+\.\d+\.\d+([-+][0-9A-Za-z.+-]*)?$`)
	commitVersion  = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// URL is where Version can be looked up: the release notes of a tagged
// release, or the commit a SHA build came from. It is "" for anything that
// names no published state - "dev", "unknown", and every "-dirty" build, whose
// SHA is a commit the build only started from.
//
// Release tags are assumed to be "v" plus the version, which is the form the
// image workflow's semver tag rule parses and strips back off.
func URL() string {
	return urlFor(Version)
}

func urlFor(version string) string {
	switch {
	case releaseVersion.MatchString(version):
		return RepoURL + "/releases/tag/v" + version
	case commitVersion.MatchString(version):
		return RepoURL + "/commit/" + version
	}
	return ""
}
