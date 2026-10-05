//go:build js && wasm

// Command wasm is the game engine for the browser: webplay compiled to
// WebAssembly, exposed to the page as a global `rfogGame` whose
// functions take and return JSON strings. The page draws; every rule is
// decided here, by the same engine the server runs.
//
//	GOOS=js GOARCH=wasm go build -o web/engine.wasm ./cmd/wasm   (make web)
package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"

	"rfog/assets"
	"rfog/engine"
	"rfog/webplay"
)

var (
	content *engine.Content
	game    *webplay.Game
	viewer  *webplay.Viewer
)

func main() {
	c, err := webplay.Content()
	if err != nil {
		js.Global().Get("console").Call("error", "rfog: "+err.Error())
		return
	}
	content = c
	api := map[string]any{
		"content": fn(func(args []js.Value) any { return content }),
		// How each hero and unit is drawn: pixel figures and their palette
		// (T and t are the team colour), plus the old sheet index as a
		// fallback.
		"cast": fn(func(args []js.Value) any {
			pal := map[string]string{}
			for k, v := range assets.Palette {
				pal[string(k)] = v
			}
			return map[string]any{"size": assets.UnitSize, "palette": pal, "units": assets.Units, "floating": assets.Floating, "poses": assets.Poses, "sheet": assets.Cast}
		}),
		"newGame": fn(func(args []js.Value) any {
			g, err := webplay.NewGame(content, args[0].String(), args[1].String(), args[2].String(), uint64(args[3].Float()))
			if err != nil {
				return fail(err)
			}
			game = g
			return map[string]bool{"ok": true}
		}),
		// Two players at this device: heroes for each, a seed.
		"newHotseat": fn(func(args []js.Value) any {
			g, err := webplay.NewHotseat(content, args[0].String(), args[1].String(), uint64(args[2].Float()))
			if err != nil {
				return fail(err)
			}
			game = g
			return map[string]bool{"ok": true}
		}),
		"planner": fn(func(args []js.Value) any { return game.Planner() }),
		// A match in progress as JSON, for the page to keep; load resumes one.
		"save": fn(func(args []js.Value) any { return game.Save() }),
		"load": fn(func(args []js.Value) any {
			var sv webplay.Save
			if err := json.Unmarshal([]byte(args[0].String()), &sv); err != nil {
				return fail(err)
			}
			g, err := webplay.Load(content, sv)
			if err != nil {
				return fail(err)
			}
			game = g
			return map[string]bool{"ok": true}
		}),
		"view": fn(func(args []js.Value) any { return game.View() }),
		// Replays: the offline match so far as a replay; load a replay (from
		// the server or this device) and step through its turns.
		"gameReplay": fn(func(args []js.Value) any { return game.Replay() }),
		"replayLoad": fn(func(args []js.Value) any {
			v, err := webplay.LoadReplay(content, []byte(args[0].String()))
			if err != nil {
				return fail(err)
			}
			viewer = v
			return map[string]any{"turns": v.Turns(), "setup": v.Setup, "start": v.Start()}
		}),
		"replayTurn": fn(func(args []js.Value) any {
			t, err := viewer.Turn(args[0].Int())
			if err != nil {
				return fail(err)
			}
			return t
		}),
		// Online: the rules' fingerprint (sent in Hello), the server's rules
		// when they differ (from Welcome), and the server's view of a match
		// to preview orders against.
		"fingerprint": fn(func(args []js.Value) any { return content.Fingerprint() }),
		"useContent": fn(func(args []js.Value) any {
			var c engine.Content
			if err := json.Unmarshal([]byte(args[0].String()), &c); err != nil {
				return fail(err)
			}
			content = &c
			return map[string]bool{"ok": true}
		}),
		"remote": fn(func(args []js.Value) any {
			var v engine.State
			if err := json.Unmarshal([]byte(args[0].String()), &v); err != nil {
				return fail(err)
			}
			game = webplay.Remote(content, v, args[1].Int())
			return map[string]bool{"ok": true}
		}),
		"moves": fn(func(args []js.Value) any {
			return game.Moves(args[0].Int())
		}),
		"targets": fn(func(args []js.Value) any {
			return game.Targets(args[0].Int(), engine.Pos{X: args[1].Int(), Y: args[2].Int()})
		}),
		"abilityTargets": fn(func(args []js.Value) any {
			var pending []engine.Order
			_ = json.Unmarshal([]byte(args[2].String()), &pending)
			ts, err := game.AbilityTargets(args[0].Int(), args[1].String(), pending)
			if err != nil {
				return fail(err)
			}
			return ts
		}),
		"check": fn(func(args []js.Value) any {
			var orders []engine.Order
			_ = json.Unmarshal([]byte(args[0].String()), &orders)
			return game.Check(orders)
		}),
		"commit": fn(func(args []js.Value) any {
			var orders []engine.Order
			if err := json.Unmarshal([]byte(args[0].String()), &orders); err != nil {
				return fail(err)
			}
			t, err := game.Commit(orders)
			if err != nil {
				return fail(err)
			}
			return t
		}),
	}
	obj := js.Global().Get("Object").New()
	for k, v := range api {
		obj.Set(k, v)
	}
	js.Global().Set("rfogGame", obj)
	if ready := js.Global().Get("onRfogReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {} // keep the engine alive for the page
}

// fn wraps a call so it returns its result as a JSON string. A call
// before a game exists, or one that panics, returns an error instead: a
// panic escaping into the page would stop the engine for good.
func fn(f func(args []js.Value) any) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) (out any) {
		defer func() {
			if r := recover(); r != nil {
				b, _ := json.Marshal(fail(fmt.Errorf("engine: %v", r)))
				out = string(b)
			}
		}()
		b, err := json.Marshal(f(args))
		if err != nil {
			b, _ = json.Marshal(fail(err))
		}
		return string(b)
	})
}

func fail(err error) map[string]string { return map[string]string{"error": err.Error()} }
