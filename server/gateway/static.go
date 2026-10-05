package gateway

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

var timeZero time.Time

// static serves the embedded site. Every file gets an ETag from its
// contents and Cache-Control: no-cache, so a browser asks on every load
// and a deploy is picked up at once, but an unchanged file costs a 304,
// not a download. Text and WebAssembly are sent gzipped when the browser
// accepts it (the engine is 4.7 MB raw, about 1.3 MB gzipped); the
// compressed copy is made once, on first request.
type static struct {
	fsys  fs.FS
	mu    sync.Mutex
	files map[string]*staticFile
}

type staticFile struct {
	typ  string
	etag string
	raw  []byte
	gz   []byte // nil when not worth compressing
}

func newStatic(fsys fs.FS) *static { return &static{fsys: fsys, files: map[string]*staticFile{}} }

func (s *static) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	f := s.file(name)
	if f == nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Type", f.typ)
	h.Set("Vary", "Accept-Encoding")
	body, etag := f.raw, f.etag
	if f.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		body, etag = f.gz, f.etag+"-gz"
		h.Set("Content-Encoding", "gzip")
	}
	h.Set("ETag", `"`+etag+`"`)
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, `"`+etag+`"`) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	http.ServeContent(w, r, "", timeZero, bytes.NewReader(body))
}

// file loads and caches one embedded file (nil if there is none).
func (s *static) file(name string) *staticFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.files[name]; ok {
		return f
	}
	raw, err := fs.ReadFile(s.fsys, name)
	if err != nil {
		return nil
	}
	sum := sha256.Sum256(raw)
	f := &staticFile{raw: raw, etag: hex.EncodeToString(sum[:8])}
	ext := path.Ext(name)
	f.typ = mime.TypeByExtension(ext)
	switch ext {
	case ".wasm":
		f.typ = "application/wasm"
	case ".webmanifest":
		f.typ = "application/manifest+json"
	case ".svg":
		f.typ = "image/svg+xml"
	}
	if f.typ == "" {
		f.typ = http.DetectContentType(raw)
	}
	switch ext {
	case ".html", ".js", ".css", ".wasm", ".json", ".svg", ".txt", ".webmanifest":
		var b bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
		zw.Write(raw)
		zw.Close()
		if b.Len() < len(raw) {
			f.gz = b.Bytes()
		}
	}
	s.files[name] = f
	return f
}
