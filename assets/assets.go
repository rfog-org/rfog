// Package assets embeds converted art frames (see render/art and
// `rfog asciify`). Source images, when they exist, live next to the
// frames only if small; the frames are what ships.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed art/*.gz
var files embed.FS

// Art is the frame store: <name>.<tier>.gz files.
func Art() fs.FS {
	sub, err := fs.Sub(files, "art")
	if err != nil {
		panic(err)
	}
	return sub
}
