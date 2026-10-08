// Package web embeds the graphical client: index.html draws the board and
// runs the engine in the page as WebAssembly (engine.wasm, built from
// cmd/wasm by `make web`), so play is instant; online it speaks the game
// protocol to the server at /net.
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html play.js play.css sprites.png engine.wasm wasm_exec.js icon.svg favicon-32.png apple-touch-icon.png icon-192.png icon-512.png og.png manifest.webmanifest
var files embed.FS

// Files is the static site served at /.
func Files() fs.FS { return files }
