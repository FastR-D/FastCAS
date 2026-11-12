package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/go-chi/chi/v5"
	"github.com/zitadel/oidc/v3/pkg/op"
)

type Server struct {
	Store          *core.Store
	Provider       *op.Provider
	Issuer         string
	Dev            bool
	ConsoleEnabled bool
	ProxySecret    string
	handler        http.Handler
	authAssets     authAssets
	github         *gitHubProvider
}

func New(store *core.Store, issuer string, dev bool) (*Server, error) {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return nil, errors.New("issuer must be an origin without a trailing slash")
	}
	if parsed.Scheme != "https" && !(dev && parsed.Scheme == "http" && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1")) {
		return nil, errors.New("HTTPS issuer required except explicit loopback development")
	}
	if store.Keys == nil {
		return nil, errors.New("keys must be initialized before starting")
	}
	options := []op.Option{
		op.WithCustomAuthEndpoint(op.NewEndpoint("oauth/authorize")), op.WithCustomTokenEndpoint(op.NewEndpoint("oauth/token")),
		op.WithCustomUserinfoEndpoint(op.NewEndpoint("oauth/userinfo")), op.WithCustomKeysEndpoint(op.NewEndpoint("oauth/jwks")),
		op.WithCustomIntrospectionEndpoint(op.NewEndpoint("oauth/introspect")), op.WithCustomRevocationEndpoint(op.NewEndpoint("oauth/revoke")),
		op.WithCustomEndSessionEndpoint(op.NewEndpoint("oauth/end-session")),
		op.WithCustomDeviceAuthorizationEndpoint(op.NewEndpoint("oauth/device_authorization")),
	}
	if dev {
		options = append(options, op.WithAllowInsecure())
	}
	provider, err := op.NewOpenIDProvider(issuer, &op.Config{CryptoKey: store.Keys.Encryption, CryptoKeyId: core.Hash(string(store.Keys.Encryption[:]))[:24], CodeMethodS256: true, AuthMethodPost: true, GrantTypeRefreshToken: true, DefaultLogoutRedirectURI: issuer + "/login", DeviceAuthorization: op.DeviceAuthorizationConfig{Lifetime: 10 * time.Minute, PollInterval: 5 * time.Second, UserFormPath: "/device", UserCode: op.UserCodeBase20}}, store, options...)
	if err != nil {
		return nil, err
	}
	s := &Server{Store: store, Provider: provider, Issuer: issuer, Dev: dev}
	r := chi.NewRouter()
	r.Use(s.boundary)
	r.Get("/health/live", func(w http.ResponseWriter, r *http.Request) { send(w, 200, map[string]bool{"alive": true}) })
	r.Get("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		if store.Health(r.Context()) != nil {
			send(w, 503, map[string]bool{"ready": false})
			return
		}
		send(w, 200, map[string]bool{"ready": true})
	})
	r.Get("/login", s.loginPage)
	r.Get("/authorize/continue", s.loginPage)
	r.Post("/login", s.login)
	r.Post("/auth/github/start", s.githubStart)
	r.Get("/auth/github/callback", s.githubCallback)
	r.Post("/auth/github/mfa", s.githubMFA)
	r.Get("/device", s.devicePage)
	r.Post("/device", s.deviceConfirm)
	r.Get("/api/v1/me", s.me)
	r.Post("/api/v1/logout", s.logout)
	s.accountRoutes(r)
	r.Post("/api/v1/link-intents", s.linkIntent)
	r.Post("/api/v1/link-intents/{id}/prepare", s.prepare)
	r.Post("/api/v1/link-intents/{id}/activate", s.activate)
	r.Get("/api/v1/account-links/resolve", s.resolve)
	r.Get("/api/v1/account-links/{id}", s.link)
	r.Post("/api/v1/account-links/{id}/revoke", s.revoke)
	r.Mount("/", provider)
	s.handler = r
	return s, nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }
func (s *Server) boundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		requestID := core.RandomToken()
		w.Header().Set("X-Request-ID", requestID)
		r = r.WithContext(core.WithAuditRequestID(r.Context(), requestID))
		// Issuer and cookie security never depend on client-supplied forwarding headers.
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		next.ServeHTTP(w, r)
	})
}
func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, err error) {
	status, code, message := 500, "internal_error", "Request could not be completed"
	switch {
	case errors.Is(err, core.ErrUnauthorized):
		status, code, message = 401, "authentication_required", "Please sign in again"
	case errors.Is(err, core.ErrForbidden):
		status, code, message = 403, "forbidden", "Operation not permitted"
	case errors.Is(err, core.ErrPrecondition):
		status, code, message = 412, "version_changed", "The account link changed; reload its current version"
	case errors.Is(err, core.ErrConflict):
		status, code, message = 409, "conflict", "Account link conflicts with an existing relationship"
	case errors.Is(err, core.ErrExpired):
		status, code, message = 400, "expired", "Request expired or was already consumed"
	}
	problem(w, status, code, message)
}
func problem(w http.ResponseWriter, status int, code, message string) {
	send(w, status, map[string]any{"code": code, "message": message, "request_id": w.Header().Get("X-Request-ID"), "retryable": status >= 500 || status == 429})
}
func decode(r *http.Request, dst any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}
func cookie(r *http.Request, name string) string {
	v, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return v.Value
}

func (s *Server) peer(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	// The edge proxy overwrites both headers. A caller cannot select an IP by
	// supplying forwarding headers unless it knows the private proxy secret.
	if len(s.ProxySecret) >= 32 && len(r.Header.Values("X-FastCAS-Proxy-Secret")) == 1 &&
		subtle.ConstantTimeCompare([]byte(r.Header.Get("X-FastCAS-Proxy-Secret")), []byte(s.ProxySecret)) == 1 &&
		len(r.Header.Values("X-FastCAS-Client-IP")) == 1 {
		if address, err := netip.ParseAddr(r.Header.Get("X-FastCAS-Client-IP")); err == nil && address.Zone() == "" {
			return address.Unmap().String()
		}
	}
	return host
}
func (s *Server) setCookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.Issuer, "https:"), SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func (s *Server) originOK(r *http.Request) bool { return r.Header.Get("Origin") == s.Issuer }
func (s *Server) client(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, secret, ok := r.BasicAuth()
	var err error
	id, err = url.QueryUnescape(id)
	if err != nil {
		ok = false
	}
	secret, err = url.QueryUnescape(secret)
	if err != nil {
		ok = false
	}
	if !ok || s.Store.AuthorizeClientIDSecret(r.Context(), id, secret) != nil {
		failure(w, core.ErrUnauthorized)
		return "", false
	}
	return id, true
}

var loginTemplate = authTemplate("login", `<!doctype html><html lang="zh-CN" data-fastui-theme="dark"><head>{{template "auth-head" .}}</head><body><main>{{template "auth-toolbar" .}}<h1>FastCAS</h1>
{{if .User}}<p>已登录：{{.User.Name}}</p><h2>授权 {{.Client.Name}}</h2>
{{if .Link}}<p>为项目账号 {{.Link.LocalRef}} 完成 FastCAS 认证。项目密码和内容不会交给 FastCAS。</p>{{else}}<p>允许该应用读取以下身份信息并登录：</p>{{end}}<p>{{.Scopes}}</p>
<form method="post" action="/login"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="auth_request_id" value="{{.RequestID}}"><button class="fastui-button fastui-button--primary" name="action" value="approve">确认并继续</button></form>
{{else}}
{{if .GitHubEnabled}}<form method="post" action="/auth/github/start"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="auth_request_id" value="{{.RequestID}}"><input type="hidden" name="return_device" value="{{.ReturnDevice}}"><button class="fastui-button" name="purpose" value="login">使用 GitHub 登录</button><button class="fastui-button" name="purpose" value="register">使用 GitHub 注册</button></form>{{if .GitHubOrg}}<p>GitHub 注册仅限 {{.GitHubOrg}} 组织正式成员。</p>{{end}}{{end}}
{{if .Assets.Script}}
<div class="auth-tabs" role="tablist" aria-label="账号服务">
<a id="login-tab" role="tab" data-auth-tab="login" aria-controls="login-panel" aria-selected="{{if .Register}}false{{else}}true{{end}}" tabindex="{{if .Register}}-1{{else}}0{{end}}" href="{{.LoginURL}}">登录</a>
<a id="register-tab" role="tab" data-auth-tab="register" aria-controls="register-panel" aria-selected="{{if .Register}}true{{else}}false{{end}}" tabindex="{{if .Register}}0{{else}}-1{{end}}" href="{{.RegisterURL}}">注册</a>
</div>
{{end}}
<div id="login-panel"{{if .Assets.Script}} role="tabpanel" data-auth-panel="login" aria-labelledby="login-tab"{{end}}{{if .Register}} hidden{{end}}>
<h2>登录 FastCAS</h2>
{{if .PasswordChanged}}<div class="fastui-alert fastui-alert--success" role="status">FastCAS 密码已更新，请使用新密码重新登录</div>{{end}}
<form method="post" action="/login"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="auth_request_id" value="{{.RequestID}}">
<label>邮箱 <input class="fastui-input" name="email" type="email" required autocomplete="username"></label>
<label>密码 <input class="fastui-input" name="password" type="password" required autocomplete="current-password"></label>
<label>动态验证码或恢复码（已启用时） <input class="fastui-input" name="mfa_code" autocomplete="one-time-code"></label>
<input type="hidden" name="return_device" value="{{.ReturnDevice}}"><button class="fastui-button fastui-button--primary" name="action" value="login">登录</button></form>
</div>
{{if .Assets.Script}}
<div id="register-panel" role="tabpanel" data-auth-panel="register" aria-labelledby="register-tab"{{if not .Register}} hidden{{end}}>
<div id="registration-root"></div>
<noscript><p>邀请注册、密码恢复与双重验证重置需要启用 JavaScript。</p></noscript>
</div>
{{end}}{{end}}</main></body></html>`)

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	// A native form POST from a page with no-referrer is sent by Chromium with
	// Origin: null. Same-origin keeps the auth request ID off external sites
	// while allowing the strict Origin check on the login action.
	w.Header().Set("Referrer-Policy", "same-origin")
	if id := r.URL.Query().Get("auth_request_id"); id != "" {
		session, _ := s.Store.Session(r.Context(), cookie(r, "fastcas_session"))
		if s.resumeAuthorization(w, r, id, session) {
			return
		}
	}
	csrf := core.RandomToken()
	s.setCookie(w, "fastcas_form", csrf, 300)
	data := map[string]any{"Title": "登录", "Assets": s.authAssets, "CSRF": csrf, "RequestID": r.URL.Query().Get("auth_request_id"), "ReturnDevice": safeDeviceCode(r.URL.Query().Get("return_device"))}
	if s.github != nil {
		data["GitHubEnabled"] = true
		data["GitHubOrg"] = s.github.registrationOrg
		w.Header().Set("Content-Security-Policy", "default-src 'self'; form-action 'self' https://github.com; frame-ancestors 'none'; base-uri 'none'")
	}
	query := r.URL.Query()
	data["PasswordChanged"] = query.Get("notice") == "password-changed"
	flow := query.Get("flow")
	data["Register"] = s.authAssets.Script != "" && (query.Get("tab") == "register" || flow == "invite" || flow == "recovery" || flow == "mfa_reset")
	query.Del("flow")
	query.Del("token")
	query.Del("tab")
	data["LoginURL"] = "/login"
	if len(query) > 0 {
		data["LoginURL"] = "/login?" + query.Encode()
	}
	query.Set("tab", "register")
	data["RegisterURL"] = "/login?" + query.Encode()
	if id := r.URL.Query().Get("auth_request_id"); id != "" {
		request, err := s.Store.AuthRequestByID(r.Context(), id)
		if err != nil {
			failure(w, err)
			return
		}
		a := request.(*core.Authorization)
		if !s.authorizationFormPolicy(w, a) {
			return
		}
		client, err := s.Store.Client(r.Context(), a.GetClientID())
		if err != nil {
			failure(w, err)
			return
		}
		data["Client"] = client
		data["Scopes"] = strings.Join(a.GetScopes(), ", ")
		link, err := s.Store.IntentForAuthorization(r.Context(), a)
		if err != nil {
			failure(w, err)
			return
		}
		data["Link"] = link
		session, err := s.Store.Session(r.Context(), cookie(r, "fastcas_session"))
		if err == nil && a.SessionFresh(session) && (a.RequestedSubject == "" || a.RequestedSubject == session.IdentityID) {
			user, err := s.Store.Identity(r.Context(), session.IdentityID)
			if err == nil {
				data["User"] = user
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = loginTemplate.Execute(w, data)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.originOK(r) || r.ParseForm() != nil {
		failure(w, core.ErrForbidden)
		return
	}
	got, want := r.Form.Get("csrf"), cookie(r, "fastcas_form")
	if got == "" || want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		failure(w, core.ErrForbidden)
		return
	}
	s.setCookie(w, "fastcas_form", "", -1)
	id := r.Form.Get("auth_request_id")
	switch r.Form.Get("action") {
	case "login":
		user, err := s.Store.Authenticate(r.Context(), r.Form.Get("email"), r.Form.Get("password"), s.peer(r))
		if err != nil {
			failure(w, err)
			return
		}
		enabled, err := s.Store.MFAEnabled(r.Context(), user.ID)
		if err != nil {
			failure(w, err)
			return
		}
		if enabled {
			if err = s.Store.VerifyMFA(r.Context(), user.ID, r.Form.Get("mfa_code")); err != nil {
				failure(w, err)
				return
			}
		}
		token, csrf, err := s.Store.NewSession(r.Context(), user.ID)
		if err != nil {
			failure(w, err)
			return
		}
		s.setCookie(w, "fastcas_session", token, 28800)
		if enabled {
			session, err := s.Store.Session(r.Context(), token)
			if err != nil {
				failure(w, err)
				return
			}
			if err = s.Store.MarkMFA(r.Context(), user.ID, session.ID); err != nil {
				failure(w, err)
				return
			}
		}
		s.setCookie(w, "fastcas_csrf", csrf, 28800)
		if id == "" {
			if code := safeDeviceCode(r.Form.Get("return_device")); code != "" {
				http.Redirect(w, r, "/device?user_code="+url.QueryEscape(code), http.StatusSeeOther)
				return
			}
			if s.ConsoleEnabled {
				http.Redirect(w, r, "/console/", http.StatusSeeOther)
			} else {
				http.Redirect(w, r, "/api/v1/me", http.StatusSeeOther)
			}
			return
		}
		session, err := s.Store.Session(r.Context(), token)
		if err != nil {
			failure(w, err)
			return
		}
		if s.resumeAuthorization(w, r, id, session) {
			return
		}
		http.Redirect(w, r, "/login?auth_request_id="+url.QueryEscape(id), http.StatusSeeOther)
	case "approve":
		session, err := s.Store.Session(r.Context(), cookie(r, "fastcas_session"))
		if err != nil {
			failure(w, err)
			return
		}
		request, err := s.Store.AuthRequestByID(r.Context(), id)
		if err != nil {
			failure(w, err)
			return
		}
		if !request.(*core.Authorization).SessionFresh(session) {
			failure(w, core.ErrUnauthorized)
			return
		}
		if err = s.Store.CompleteAuthorization(r.Context(), id, session); err != nil {
			failure(w, err)
			return
		}
		http.Redirect(w, r, op.AuthCallbackURL(s.Provider)(r.Context(), id), http.StatusSeeOther)
	default:
		failure(w, core.ErrForbidden)
	}
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	session, err := s.Store.Session(r.Context(), cookie(r, "fastcas_session"))
	if err != nil {
		failure(w, err)
		return
	}
	user, err := s.Store.Identity(r.Context(), session.IdentityID)
	if err != nil {
		failure(w, err)
		return
	}
	enabled, err := s.Store.MFAEnabled(r.Context(), user.ID)
	if err != nil {
		failure(w, err)
		return
	}
	github, err := s.Store.GitHubAccount(r.Context(), user.ID)
	if err != nil {
		failure(w, err)
		return
	}
	var passwordEnabled bool
	if err = s.Store.DB.QueryRow(r.Context(), `SELECT password_hash<>'' FROM identities WHERE id=$1`, user.ID).Scan(&passwordEnabled); err != nil {
		failure(w, err)
		return
	}
	send(w, 200, map[string]any{"user": user, "session": session, "csrf": cookie(r, "fastcas_csrf"), "mfa_enabled": enabled, "password_enabled": passwordEnabled, "github_enabled": s.github != nil, "github_account": github})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	token := cookie(r, "fastcas_session")
	if !s.originOK(r) || s.Store.CheckCSRF(r.Context(), token, r.Header.Get("X-CSRF-Token")) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	session, err := s.Store.Session(r.Context(), token)
	if err != nil {
		failure(w, err)
		return
	}
	err = s.Store.LogoutSession(r.Context(), session.IdentityID, session.ID)
	if err != nil {
		failure(w, err)
		return
	}
	for _, name := range []string{"fastcas_session", "fastcas_csrf", "fastcas_form"} {
		s.setCookie(w, name, "", -1)
	}
	w.WriteHeader(204)
}
func (s *Server) linkIntent(w http.ResponseWriter, r *http.Request) {
	client, ok := s.client(w, r)
	if !ok {
		return
	}
	var input struct {
		LocalRef string `json:"local_account_ref"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	result, err := s.Store.CreateLinkIntent(r.Context(), client, input.LocalRef, r.Header.Get("Idempotency-Key"))
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 201, result)
}
func (s *Server) prepare(w http.ResponseWriter, r *http.Request) {
	client, ok := s.client(w, r)
	if !ok {
		return
	}
	var input struct {
		Nonce   string `json:"nonce"`
		Subject string `json:"subject"`
	}
	if decode(r, &input) != nil {
		failure(w, core.ErrForbidden)
		return
	}
	result, err := s.Store.PrepareLink(r.Context(), client, chi.URLParam(r, "id"), input.Nonce, input.Subject)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, result)
}
func (s *Server) activate(w http.ResponseWriter, r *http.Request) {
	client, ok := s.client(w, r)
	if !ok {
		return
	}
	result, err := s.Store.ActivateLink(r.Context(), client, chi.URLParam(r, "id"))
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, result)
}
func (s *Server) link(w http.ResponseWriter, r *http.Request) {
	client, ok := s.client(w, r)
	if !ok {
		return
	}
	result, err := s.Store.Link(r.Context(), client, chi.URLParam(r, "id"))
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, result)
}
func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	client, ok := s.client(w, r)
	if !ok {
		return
	}
	result, err := s.Store.ResolveLink(r.Context(), client, r.URL.Query().Get("subject"))
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, result)
}
func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	client, ok := s.client(w, r)
	if !ok {
		return
	}
	version, err := strconv.ParseInt(strings.Trim(r.Header.Get("If-Match"), `"`), 10, 64)
	if err != nil {
		problem(w, 428, "version_required", "If-Match must contain the current link version")
		return
	}
	result, err := s.Store.RevokeLink(r.Context(), client, chi.URLParam(r, "id"), version)
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, result)
}
