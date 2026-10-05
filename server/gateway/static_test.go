package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"rfog/web"
)

// The graphical client, its engine and the terminal page are all served,
// the engine gzipped as application/wasm, and a known ETag gets a 304.
func TestStaticSite(t *testing.T) {
	h := newStatic(web.Files())
	get := func(path, enc, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if enc != "" {
			r.Header.Set("Accept-Encoding", enc)
		}
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, p := range []string{"/", "/play.js", "/play.css", "/sprites.png", "/wasm_exec.js",
		"/icon.svg", "/favicon-32.png", "/apple-touch-icon.png", "/icon-192.png", "/icon-512.png", "/manifest.webmanifest"} {
		if w := get(p, "", ""); w.Code != 200 || w.Body.Len() == 0 {
			t.Errorf("%s: %d, %d bytes", p, w.Code, w.Body.Len())
		}
	}
	w := get("/engine.wasm", "gzip, br", "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/wasm" || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("engine.wasm: %d %q %q", w.Code, w.Header().Get("Content-Type"), w.Header().Get("Content-Encoding"))
	}
	if w2 := get("/engine.wasm", "gzip", w.Header().Get("ETag")); w2.Code != http.StatusNotModified {
		t.Errorf("revalidation: %d, want 304", w2.Code)
	}
	if ct := get("/manifest.webmanifest", "", "").Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("manifest type %q", ct)
	}
	if ct := get("/icon.svg", "", "").Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("svg type %q", ct)
	}
	if w := get("/nope.js", "", ""); w.Code != 404 {
		t.Errorf("missing file: %d", w.Code)
	}
}
