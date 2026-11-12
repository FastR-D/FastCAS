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
	server, isServer := api.(*Server)
	if isServer {
		assets, err := readAuthAssets(dir)
		if err != nil {
			return nil, err
		}
		server.authAssets = assets
	}
	files := http.StripPrefix("/console/", http.FileServer(http.Dir(dir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entry := r.URL.Path == "/" || r.URL.Path == "/console" || r.URL.Path == "/console/"
		if entry && (r.Method == "GET" || r.Method == "HEAD") {
			login := r.URL.Path == "/"
			if isServer {
				login = true
				if token := cookie(r, "fastcas_session"); token != "" {
					_, err := server.Store.Session(r.Context(), token)
					login = err != nil
				}
			}
			flow := r.URL.Query().Get("flow")
			if login || flow == "invite" || flow == "recovery" || flow == "mfa_reset" {
				destination := "/login"
				if r.URL.RawQuery != "" {
					destination += "?" + r.URL.RawQuery
				}
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("Referrer-Policy", "no-referrer")
				http.Redirect(w, r, destination, http.StatusTemporaryRedirect)
				return
			}
		}
		if r.URL.Path == "/" && (r.Method == "GET" || r.Method == "HEAD") {
			http.Redirect(w, r, "/console/", http.StatusTemporaryRedirect)
			return
		}
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
