package httpapi_test

import (
	"github.com/FastR-D/FastCAS/internal/httpapi"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsoleRoutingAndSecurityHeaders(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{"index.html": "console entry", "assets/app.js": "console.log('ready')"} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) })
	handler, err := httpapi.Console(api, dir)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		method, path string
		want         int
	}{{"GET", "/api/v1/me", 201}, {"GET", "/console", 307}, {"GET", "/console/", 200}, {"HEAD", "/console/assets/app.js", 200}, {"GET", "/console/assets/app.js", 200}, {"GET", "/console/other", 404}, {"POST", "/console/", 405}}
	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s %s: got %d want %d", tc.method, tc.path, rec.Code, tc.want)
		}
		if strings.HasPrefix(tc.path, "/console/") && rec.Header().Get("Content-Security-Policy") == "" {
			t.Error("missing CSP")
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/console/", nil))
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "console entry" {
		t.Fatal("wrong asset")
	}
	if _, err := httpapi.Console(api, filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing build accepted")
	}
}
