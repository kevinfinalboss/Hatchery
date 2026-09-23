package panelapi

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func uiHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		clean := path.Clean("/" + r.URL.Path)
		if clean != "/" {
			// http.Dir already refuses to leave dir; this Stat only decides file vs. SPA route.
			if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(clean))); err == nil && !info.IsDir() {
				// Vite fingerprints everything under /assets/, so those never change under the same name.
				if strings.HasPrefix(clean, "/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		// index.html references the current asset hashes: never let a browser keep an old one.
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	})
}
