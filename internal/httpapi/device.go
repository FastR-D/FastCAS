package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
)

var deviceCodePattern = regexp.MustCompile(`^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$`)

func safeDeviceCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if deviceCodePattern.MatchString(value) {
		return value
	}
	return ""
}

func (s *Server) deviceFormProof(csrf, code string) string {
	mac := hmac.New(sha256.New, s.Store.Keys.Encryption[:])
	_, _ = mac.Write([]byte(csrf + ":" + code))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

var deviceTemplate = authTemplate("device", `<!doctype html><html lang="zh-CN"><head>{{template "auth-head" .}}</head><body><main>{{template "auth-toolbar" .}}<h1>确认设备</h1>
{{if .User}}<p>当前 FastCAS 身份：{{.User.Name}}</p>{{if .Request}}<p>应用：{{.Request.ClientName}}</p><p>权限：{{range .Request.Scopes}}{{.}} {{end}}</p><p>请核对设备屏幕上的代码 <strong class="auth-code">{{.Code}}</strong>，仅在您正主动配对自己的设备时继续。</p>
<form method="post" action="/device"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="user_code" value="{{.Code}}"><button class="fastui-button fastui-button--primary" name="decision" value="approve">允许这台设备</button><button class="fastui-button" name="decision" value="deny">拒绝</button></form>
{{else}}<form method="get" action="/device"><label>设备代码 <input class="fastui-input" name="user_code" required maxlength="9" autocomplete="off"></label><button class="fastui-button fastui-button--primary">检查</button></form>{{end}}
{{else}}<p>请先登录 FastCAS，然后返回此页输入设备上的代码。</p><a href="/login{{if .Code}}?return_device={{.Code}}{{end}}">登录</a>{{end}}{{template "auth-footer" .}}</main></body></html>`)

func (s *Server) devicePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "same-origin")
	code := safeDeviceCode(r.URL.Query().Get("user_code"))
	session, err := s.Store.Session(r.Context(), cookie(r, "fastcas_session"))
	var user *core.Identity
	if err == nil {
		user, _ = s.Store.Identity(r.Context(), session.IdentityID)
	}
	data := map[string]any{"Title": "确认设备", "Assets": s.authAssets, "Code": code, "User": user}
	if user != nil && code != "" {
		allowed, err := s.Store.Allow(r.Context(), "device-verify:"+s.peer(r), 20, 15*time.Minute)
		if err != nil || !allowed {
			failure(w, core.ErrForbidden)
			return
		}
		request, err := s.Store.DeviceRequest(r.Context(), code)
		if err != nil {
			failure(w, err)
			return
		}
		data["Request"] = request
		csrf := core.RandomToken()
		s.setCookie(w, "fastcas_device_form", s.deviceFormProof(csrf, code), 300)
		data["CSRF"] = csrf
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = deviceTemplate.Execute(w, data)
}

func (s *Server) deviceConfirm(w http.ResponseWriter, r *http.Request) {
	if !s.originOK(r) || r.ParseForm() != nil {
		failure(w, core.ErrForbidden)
		return
	}
	csrf, expected := r.Form.Get("csrf"), cookie(r, "fastcas_device_form")
	code := safeDeviceCode(r.Form.Get("user_code"))
	if csrf == "" || code == "" || expected == "" || subtle.ConstantTimeCompare([]byte(s.deviceFormProof(csrf, code)), []byte(expected)) != 1 {
		failure(w, core.ErrForbidden)
		return
	}
	s.setCookie(w, "fastcas_device_form", "", -1)
	decision := r.Form.Get("decision")
	if decision != "approve" && decision != "deny" {
		failure(w, core.ErrForbidden)
		return
	}
	session, err := s.Store.Session(r.Context(), cookie(r, "fastcas_session"))
	if err != nil {
		failure(w, err)
		return
	}
	if err = s.Store.ConfirmDevice(r.Context(), code, session.IdentityID, decision == "approve"); err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = deviceCompleteTemplate.Execute(w, map[string]any{"Title": "设备确认完成", "Assets": s.authAssets})
}
