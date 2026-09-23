package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Console serves a prebuilt application without changing the API's routes.
func Console(api http.Handler, dir string) (http.Handler, error) {
	if info, err := os.Stat(filepath.Join(dir, "index.html")); err != nil || info.IsDir() {
		return nil, fmt.Errorf("console build missing: %s", dir)
	}
	files := http.StripPrefix("/console/", http.FileServer(http.Dir(dir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/console" {
			http.Redirect(w, r, "/console/", http.StatusTemporaryRedirect)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/console/") {
			api.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/console/" && !strings.HasPrefix(r.URL.Path, "/console/assets/") {
			http.NotFound(w, r)
			return
		}
		asset := strings.TrimPrefix(r.URL.Path, "/console/assets/")
		if strings.Contains(r.URL.Path, "..") || (r.URL.Path != "/console/" && (asset == "" || strings.Contains(asset, "/") || strings.HasPrefix(asset, "."))) {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	}), nil
}
