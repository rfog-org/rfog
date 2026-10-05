package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"rfog/assets"
	"rfog/data"
	"rfog/render/art"
)

// runAsciify is the art pipeline: convert an image to text frames for
// every tier, regenerate the procedural hero placeholders, or preview a
// stored frame.
//
//	rfog asciify -placeholders                      # hero_<id>.<tier>.gz for heroes without art
//	go run ./tools/portraits                          # hero and unit art from the sprite sheet
//	rfog asciify -name wren portrait.png            # one image, all tiers
//	rfog asciify -show hero_wren                    # preview from the embedded store
func runAsciify(args []string) error {
	fs := flag.NewFlagSet("rfog asciify", flag.ContinueOnError)
	out := fs.String("out", "assets/art", "output directory")
	name := fs.String("name", "", "frame name (defaults to the image file name)")
	cols := fs.Int("cols", art.PortraitCols, "frame width in cells")
	rows := fs.Int("rows", art.PortraitRows, "frame height in cells")
	placeholders := fs.Bool("placeholders", false, "write procedural placeholder portraits for heroes that have none")
	force := fs.Bool("force", false, "with -placeholders: overwrite existing portraits too")
	show := fs.String("show", "", "print a stored frame (all tiers) to the terminal")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tiers := []string{"t0", "t1", "t2"}
	switch {
	case *show != "":
		for _, tier := range tiers {
			f, err := art.Load(assets.Art(), *show, tier)
			if err != nil {
				return err
			}
			r := lipgloss.NewRenderer(os.Stdout)
			r.SetColorProfile(termenv.TrueColor)
			st := art.Styler{Renderer: r, Plain: r.NewStyle(), Accent: r.NewStyle().Bold(true), Dim: r.NewStyle().Faint(true), Color: tier != "t0"}
			fmt.Println(tier)
			for _, l := range f.Render(st) {
				fmt.Println(l)
			}
			fmt.Println()
		}
		return nil
	case *placeholders:
		c, err := data.Load()
		if err != nil {
			return err
		}
		for _, id := range c.HeroIDs() {
			if _, err := art.Load(assets.Art(), "hero_"+id, "t1"); err == nil && !*force {
				fmt.Println("hero_" + id + ": has art, kept (-force to replace)")
				continue
			}
			h := c.Heroes[id]
			img := art.Placeholder(id, h.Class, *cols*3, *rows*6)
			if err := writeFrames(*out, "hero_"+id, img, *cols, *rows, tiers); err != nil {
				return err
			}
			// The smaller variants: separate conversions, not scaled
			// frames, so the dithering is right for each size.
			medium := art.Placeholder(id, h.Class, art.MediumCols*3, art.MediumRows*6)
			if err := writeFrames(*out, art.MediumName("hero_"+id), medium, art.MediumCols, art.MediumRows, tiers); err != nil {
				return err
			}
			small := art.Placeholder(id, h.Class, art.SmallCols*3, art.SmallRows*6)
			if err := writeFrames(*out, art.SmallName("hero_"+id), small, art.SmallCols, art.SmallRows, tiers); err != nil {
				return err
			}
			fmt.Println("hero_" + id)
		}
		return nil
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: rfog asciify [-name n] [-cols w -rows h] image.png | -placeholders | -show name")
	}
	path := fs.Arg(0)
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	img, _, err := image.Decode(fh)
	if err != nil {
		return err
	}
	n := *name
	if n == "" {
		n = filepath.Base(path)
		n = n[:len(n)-len(filepath.Ext(n))]
	}
	return writeFrames(*out, n, img, *cols, *rows, tiers)
}

func writeFrames(dir, name string, img image.Image, cols, rows int, tiers []string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, tier := range tiers {
		f := art.Convert(img, tier, cols, rows)
		fh, err := os.Create(filepath.Join(dir, art.FileName(name, tier)))
		if err != nil {
			return err
		}
		if err := art.Encode(fh, f); err != nil {
			fh.Close()
			return err
		}
		fh.Close()
	}
	return nil
}
