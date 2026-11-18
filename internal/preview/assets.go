package preview

import (
	"embed"
	"net/http"
	"path"
	"strings"
)

//go:embed all:assets
var assets embed.FS

// assetTypes is fixed rather than taken from the OS mime table: a module
// script refused on the wrong content type would break the page, and the
// table differs between platforms.
var assetTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".woff2": "font/woff2",
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	serveAssetFile(w, r, "assets/index.html")
}

// serveAssets serves the preview page: its own app.js and preview.css at the
// top level, plus the vendored libraries, styles and fonts under their
// subtrees. The vendoring bookkeeping (checksums, manifest) stays out of the
// URL space.
func (s *Server) serveAssets(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	dir, _, nested := strings.Cut(name, "/")
	if nested {
		if dir != "lib" && dir != "css" && dir != "fonts" {
			http.NotFound(w, r)
			return
		}
	} else if name != "app.js" && name != "preview.css" {
		http.NotFound(w, r)
		return
	}
	serveAssetFile(w, r, path.Join("assets", name))
}

// serveAssetFile answers with one embedded file. The assets ship inside the
// binary, so a running server serves them unchanged: a year-long cache keeps
// reloads off the wire.
func serveAssetFile(w http.ResponseWriter, r *http.Request, name string) {
	data, err := assets.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctype := assetTypes[strings.ToLower(path.Ext(name))]
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
