// Command portraits cuts hero and unit portraits out of a 16x16 pixel
// sprite sheet and converts them into frames in assets/art.
//
//	go run ./tools/portraits                     # Kenney's Tiny Dungeon (CC0)
//	go run ./tools/portraits -sheet other.png    # another 12-wide 16px sheet
//
// A half-block cell is one pixel wide and two tall, so a sprite scaled by
// a whole number lands on cells exactly, with no blur: the full portrait
// is a 2x bust, the medium one the whole figure at 1x, the small one the
// head at 1x. The cast is assets.Cast. It also writes each character's board piece (mini_<name>):
// head and shoulders at half size. The cast below maps names to tile
// indices (row*12+col).
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"rfog/assets"
	"rfog/render/art"
)

var cast = assets.Cast

const tile = 16

func main() {
	sheetPath := flag.String("sheet", "assets/src/tiny-dungeon/tilemap_packed.png", "sprite sheet, 16px tiles")
	perRow := flag.Int("per-row", 12, "tiles per sheet row")
	out := flag.String("out", "assets/art", "output directory")
	flag.Parse()
	f, err := os.Open(*sheetPath)
	if err != nil {
		fail(err)
	}
	sheet, err := png.Decode(f)
	f.Close()
	if err != nil {
		fail(err)
	}
	sizes := []struct {
		name       func(string) string
		cols, rows int
		scale      int
	}{
		{func(n string) string { return n }, art.PortraitCols, art.PortraitRows, 2},
		{art.MediumName, art.MediumCols, art.MediumRows, 1},
		{art.SmallName, art.SmallCols, art.SmallRows, 1},
	}
	for name, idx := range cast {
		tx, ty := (idx%*perRow)*tile, (idx / *perRow)*tile
		minX, minY, maxX, maxY := tile, tile, -1, -1
		for y := 0; y < tile; y++ {
			for x := 0; x < tile; x++ {
				if _, _, _, a := sheet.At(tx+x, ty+y).RGBA(); a > 0 {
					minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
				}
			}
		}
		cx := (minX + maxX + 1) / 2
		for _, sz := range sizes {
			w, h := sz.cols, sz.rows*2
			img := image.NewNRGBA(image.Rect(0, 0, w, h))
			// Centred on the figure. Zoomed in, the top outline row goes so
			// the chin fits; at 1x a canvas that holds the figure centres
			// it, a smaller one keeps the head.
			ox, oy := cx-w/sz.scale/2, minY+1
			if sz.scale == 1 && h >= tile {
				oy = minY - (h-(maxY-minY+1))/2
			} else if sz.scale == 1 {
				oy = minY
			}
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					sx, sy := ox+x/sz.scale, oy+y/sz.scale
					if sx < 0 || sy < 0 || sx >= tile || sy >= tile {
						continue
					}
					img.Set(x, y, color.NRGBAModel.Convert(sheet.At(tx+sx, ty+sy)))
				}
			}
			for _, tier := range []string{"t0", "t1", "t2"} {
				fr := art.Convert(img, tier, sz.cols, sz.rows)
				fh, err := os.Create(filepath.Join(*out, art.FileName(sz.name(name), tier)))
				if err != nil {
					fail(err)
				}
				if err := art.Encode(fh, fr); err != nil {
					fail(err)
				}
				fh.Close()
			}
		}
		// The board piece: the top twelve rows of the sprite (head and
		// shoulders) at half size, 8x6 pixels, three rows of half blocks.
		// A pixel is kept where at least two of its four source pixels are.
		mini := image.NewNRGBA(image.Rect(0, 0, MiniCols, MiniRows*2))
		for my := 0; my < MiniRows*2; my++ {
			for mx := 0; mx < MiniCols; mx++ {
				var r, g, b, n uint32
				for dy := 0; dy < 2; dy++ {
					for dx := 0; dx < 2; dx++ {
						sx, sy := cx-MiniCols+mx*2+dx, minY+my*2+dy
						if sx < 0 || sy >= tile || sx >= tile {
							continue
						}
						cr, cg, cb, ca := sheet.At(tx+sx, ty+sy).RGBA()
						if ca > 0 {
							r, g, b, n = r+cr>>8, g+cg>>8, b+cb>>8, n+1
						}
					}
				}
				if n >= 2 {
					mini.Set(mx, my, color.NRGBA{uint8(r / n), uint8(g / n), uint8(b / n), 255})
				}
			}
		}
		for _, tier := range []string{"t1"} { // pieces are colour art: no mono tier
			fr := art.Convert(mini, tier, MiniCols, MiniRows)
			fh, err := os.Create(filepath.Join(*out, art.FileName("mini_"+name, tier)))
			if err != nil {
				fail(err)
			}
			if err := art.Encode(fh, fr); err != nil {
				fail(err)
			}
			fh.Close()
		}
		fmt.Println(name)
	}
}

// The board piece's size in cells (8x6 pixels).
const MiniCols, MiniRows = 8, 3

func fail(err error) {
	fmt.Fprintln(os.Stderr, "portraits:", err)
	os.Exit(1)
}
