package gateway

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
)

// contentSecurityPolicy applies to the HTML shell. Everything is served from
// our own origin; no inline scripts or style attributes are used.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; " +
	"font-src 'self'; connect-src 'self'; manifest-src 'self'; worker-src 'self'; frame-src 'self'; " +
	"object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

type asset struct {
	body        []byte
	gzipped     []byte // nil when compression does not pay off
	etag        string
	contentType string
	cache       string
}

// Static serves the embedded single page app from memory, with ETags and
// pre-compressed variants.
type Static struct {
	assets  map[string]*asset
	index   *asset
	Version string // hash of all the files, used for cache busting
}

var contentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".webmanifest": "application/manifest+json",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".ico":         "image/x-icon",
	".txt":         "text/plain; charset=utf-8",
	".woff2":       "font/woff2",
}

// NewStatic loads every file of fsys. The placeholder {{VERSION}} in
// index.html and sw.js is replaced with the content hash.
func NewStatic(fsys fs.FS) (*Static, error) {
	raw := map[string][]byte{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		raw[p] = b
		return nil
	})
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(raw))
	for p := range raw {
		names = append(names, p)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, p := range names {
		h.Write([]byte(p))
		h.Write(raw[p])
	}
	s := &Static{assets: map[string]*asset{}, Version: hex.EncodeToString(h.Sum(nil))[:12]}

	for _, p := range names {
		body := raw[p]
		if p == "index.html" || p == "sw.js" {
			body = bytes.ReplaceAll(body, []byte("{{VERSION}}"), []byte(s.Version))
		}
		ext := path.Ext(p)
		a := &asset{body: body, contentType: contentTypes[ext], cache: "no-cache"}
		if a.contentType == "" {
			a.contentType = "application/octet-stream"
		}
		if strings.HasPrefix(p, "icons/") {
			a.cache = "public, max-age=604800"
		}
		sum := sha256.Sum256(body)
		a.etag = `"` + hex.EncodeToString(sum[:8]) + `"`
		if compressible(ext) && len(body) > 512 {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			zw.Write(body)
			zw.Close()
			if buf.Len() < len(body) {
				a.gzipped = buf.Bytes()
			}
		}
		s.assets["/"+p] = a
	}
	s.index = s.assets["/index.html"]
	if s.index == nil {
		return nil, fs.ErrNotExist
	}
	return s, nil
}

func compressible(ext string) bool {
	switch ext {
	case ".html", ".js", ".css", ".json", ".webmanifest", ".svg", ".txt":
		return true
	}
	return false
}

// Lookup returns the asset for a URL path.
func (s *Static) Lookup(urlPath string) (*asset, bool) {
	if urlPath == "/" {
		return s.index, true
	}
	a, ok := s.assets[urlPath]
	return a, ok
}

// Serve writes an asset, honouring If-None-Match and Accept-Encoding.
func (s *Static) Serve(w http.ResponseWriter, r *http.Request, a *asset) {
	h := w.Header()
	h.Set("Content-Type", a.contentType)
	h.Set("Cache-Control", a.cache)
	h.Set("ETag", a.etag)
	h.Set("Vary", "Accept-Encoding")
	if a == s.index {
		h.Set("Content-Security-Policy", contentSecurityPolicy)
	}
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, a.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := a.body
	if a.gzipped != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		body = a.gzipped
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}

// ServeIndex writes the app shell (used for client-side routes).
func (s *Static) ServeIndex(w http.ResponseWriter, r *http.Request) {
	s.Serve(w, r, s.index)
}
