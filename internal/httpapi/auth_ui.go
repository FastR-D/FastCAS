package httpapi

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

type authAssets struct {
	Bootstrap string
	Script    string
	Styles    []string
}

// Read the Vite manifest at startup. Authentication pages consume the same
// npm-built foundation as the console, without moving form validation into JS.
func readAuthAssets(dir string) (authAssets, error) {
	var result authAssets
	raw, err := os.ReadFile(filepath.Join(dir, ".vite", "manifest.json"))
	if err != nil {
		return result, fmt.Errorf("console auth manifest missing; rebuild web: %w", err)
	}
	type entry struct {
		File    string   `json:"file"`
		CSS     []string `json:"css"`
		Imports []string `json:"imports"`
	}
	var manifest map[string]entry
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return result, fmt.Errorf("invalid console auth manifest: %w", err)
	}
	asset := func(path, extension string) (string, error) {
		name := strings.TrimPrefix(path, "assets/")
		if name == path || name == "" || strings.ContainsAny(name, "/\\?#") || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, extension) {
			return "", fmt.Errorf("invalid console auth asset: %s", path)
		}
		if info, err := os.Stat(filepath.Join(dir, path)); err != nil || info.IsDir() {
			return "", fmt.Errorf("console auth asset missing: %s", path)
		}
		return "/console/" + path, nil
	}
	visited, styles := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(key string) error {
		if visited[key] {
			return nil
		}
		visited[key] = true
		item, ok := manifest[key]
		if !ok {
			return fmt.Errorf("console auth entry missing: %s", key)
		}
		if _, err := asset(item.File, ".js"); err != nil {
			return err
		}
		for _, dependency := range item.Imports {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		for _, path := range item.CSS {
			url, err := asset(path, ".css")
			if err != nil {
				return err
			}
			if !styles[url] {
				styles[url] = true
				result.Styles = append(result.Styles, url)
			}
		}
		return nil
	}
	if err := visit("src/auth.ts"); err != nil {
		return result, err
	}
	result.Script, err = asset(manifest["src/auth.ts"].File, ".js")
	if err == nil {
		result.Bootstrap, err = asset("assets/theme-init.js", ".js")
	}
	if err == nil && len(result.Styles) == 0 {
		return result, fmt.Errorf("console auth styles missing")
	}
	return result, err
}

func authTemplate(name, body string) *template.Template {
	return template.Must(template.New(name).Parse(`
{{define "auth-head"}}<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}} · FastCAS</title>
{{if .Assets.Bootstrap}}<script src="{{.Assets.Bootstrap}}"></script>{{end}}{{range .Assets.Styles}}<link rel="stylesheet" href="{{.}}">{{end}}{{if .Assets.Script}}<script type="module" src="{{.Assets.Script}}"></script>{{end}}{{end}}
{{define "auth-toolbar"}}<div class="auth-toolbar"><span class="auth-brand">FASTR-D / IDENTITY</span>{{if .Assets.Script}}<select class="fastui-select fastui-select--sm" data-theme-select aria-label="界面主题"><option value="system">跟随系统</option><option value="light">浅色主题</option><option value="dark">深色主题</option></select>{{end}}</div>{{end}}
` + body))
}

var deviceCompleteTemplate = authTemplate("device-complete", `<!doctype html><html lang="zh-CN" data-fastui-theme="dark"><head>{{template "auth-head" .}}</head><body><main>{{template "auth-toolbar" .}}<h1>设备确认完成</h1><p>设备请求已处理，可返回设备继续操作。</p><a href="/console/">返回账号中心</a></main></body></html>`)
