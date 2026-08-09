// Package webassets embeds the frontend (web/) so it ships inside the
// binary. In dev, config.WebDir overrides this with a live directory served
// straight off disk instead (see internal/httpapi.WebFS).
package webassets

import (
	"embed"
	"io/fs"
)

//go:embed web
var embedded embed.FS

// FS returns the embedded frontend rooted at web/.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "web")
	if err != nil {
		panic(err)
	}
	return sub
}
