package buildinfo

import "testing"

func TestURLFor(t *testing.T) {
	for _, tc := range []struct {
		version string
		want    string
	}{
		{"1.4.0", RepoURL + "/releases/tag/v1.4.0"},
		{"1.4.0-rc.1", RepoURL + "/releases/tag/v1.4.0-rc.1"},
		{"bc3d84e", RepoURL + "/commit/bc3d84e"},
		{"bc3d84e8f0a1b2c3d4e5f60718293a4b5c6d7e8f", RepoURL + "/commit/bc3d84e8f0a1b2c3d4e5f60718293a4b5c6d7e8f"},
		// A dirty build is not the commit it names.
		{"bc3d84e-dirty", ""},
		{"dev", ""},
		{"unknown", ""},
		// What the image workflow used to stamp on main.
		{"latest", ""},
		// Too short to be a SHA, and not a release either.
		{"abc", ""},
		{"", ""},
	} {
		if got := urlFor(tc.version); got != tc.want {
			t.Errorf("urlFor(%q) = %q, want %q", tc.version, got, tc.want)
		}
	}
}
