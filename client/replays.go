package client

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"rfog/engine"
)

// replayBrowser lists saved replays and plays one back through the match
// screen in spectator mode (no fog).
type replayBrowser struct {
	files []string
	sel   int
	err   string
}

func newReplayBrowser(a *App) *replayBrowser {
	r := &replayBrowser{}
	entries, _ := os.ReadDir(DataDir())
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			r.files = append(r.files, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(r.files)))
	return r
}

func (r *replayBrowser) update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	if mm, ok := msg.(tea.MouseMsg); ok {
		if id, hit, act := a.pointer(mm, r.sel); hit {
			r.sel = id
			if act {
				return r.open(a)
			}
		}
		return r, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return r, nil
	}
	switch {
	case isKey(k, "j", "down"):
		if len(r.files) > 0 {
			r.sel = (r.sel + 1) % len(r.files)
		}
	case isKey(k, "k", "up"):
		if len(r.files) > 0 {
			r.sel = (r.sel + len(r.files) - 1) % len(r.files)
		}
	case isKey(k, "enter"):
		return r.open(a)
	case isKey(k, "q", "esc"):
		return newMenuScreen(), nil
	}
	return r, nil
}

// open plays the highlighted replay. Shared by enter and a tap.
func (r *replayBrowser) open(a *App) (screen, tea.Cmd) {
	if len(r.files) == 0 {
		return r, nil
	}
	b, err := os.ReadFile(filepath.Join(DataDir(), r.files[r.sel]))
	if err != nil {
		r.err = err.Error()
		return r, nil
	}
	rp, err := engine.UnmarshalReplay(b)
	if err != nil {
		r.err = err.Error()
		return r, nil
	}
	return newMatchScreen(a, newReplayMatch(a.c, rp)), tick(50)
}

func (r *replayBrowser) view(a *App) string {
	var lines []string
	lines = append(lines, a.st.Title.Render("replays"), "", a.st.Dim.Render(DataDir()), "")
	if len(r.files) == 0 {
		lines = append(lines, a.st.Dim.Render("no replays yet — finish a match and press s on the end screen"))
	}
	for i, f := range r.files {
		a.clickRow(len(lines), i)
		if i == r.sel {
			lines = append(lines, a.st.Accent.Render("> "+f))
		} else {
			lines = append(lines, "  "+f)
		}
	}
	if r.err != "" {
		lines = append(lines, "", a.st.Danger.Render(r.err))
	}
	lines = append(lines, "", a.st.Dim.Render("enter watch  esc back"))
	return a.centered(lines)
}
