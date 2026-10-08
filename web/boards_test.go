package web

import (
	"os"
	"regexp"
	"testing"

	"rfog/render"
)

// The web app and the terminal client offer the same boards, in the same
// order, in the same colours; the page's default (:root in play.css) is
// the first of them.
func TestBoardsMatchTerminal(t *testing.T) {
	js, err := os.ReadFile("play.js")
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile(`\['([a-z-]+)', '(#[0-9a-f]{6})', '(#[0-9a-f]{6})', '(#[0-9a-f]{6})'\]`)
	block := regexp.MustCompile(`(?s)var BOARDS = \[(.*?)\];`).FindSubmatch(js)
	if block == nil {
		t.Fatal("no BOARDS in play.js")
	}
	var web []render.Board
	for _, m := range row.FindAllStringSubmatch(string(block[1]), -1) {
		web = append(web, render.Board{Name: m[1], Light: m[2], Dark: m[3], Wall: m[4]})
	}
	if len(web) != len(render.Boards) {
		t.Fatalf("play.js has %d boards, render.Boards %d", len(web), len(render.Boards))
	}
	for i, b := range render.Boards {
		if web[i] != b {
			t.Errorf("board %d: play.js %+v, render.Boards %+v", i, web[i], b)
		}
	}
	css, err := os.ReadFile("play.css")
	if err != nil {
		t.Fatal(err)
	}
	f := render.Boards[0]
	want := "--light: " + f.Light + "; --dark: " + f.Dark + "; --wall: " + f.Wall + ";"
	if !regexp.MustCompile(regexp.QuoteMeta(want)).Match(css) {
		t.Errorf("play.css :root should carry %s (%s)", want, f.Name)
	}
}
