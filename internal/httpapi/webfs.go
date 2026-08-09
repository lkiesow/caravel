package httpapi

import (
	"io/fs"
	"os"
)

// WebFS returns the filesystem to serve static frontend assets from.
// If dir is non-empty it serves live from that directory on disk (the dev
// workflow: edit a file, refresh the browser, no rebuild needed). Otherwise
// it serves from the provided embedded filesystem, which is what ships
// inside a built binary.
func WebFS(embedded fs.FS, dir string) fs.FS {
	if dir == "" {
		return embedded
	}
	return os.DirFS(dir)
}
