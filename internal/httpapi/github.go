package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
)

type gitHubProvider struct {
	clientID, secret, registrationOrg string
	client                            *http.Client
}

// ConfigureGitHub enables GitHub's OAuth authorization-code flow. FastCAS
// remains the OIDC provider for relying parties. Non-development environments
// default to requiring active FastR-D membership for new GitHub registrations.
func (s *Server) ConfigureGitHub(clientID, secret, registrationOrg string) error {
	if clientID == "" && secret == "" {
		return nil
	}
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(secret) == "" {
		return errors.New("both FASTCAS_GITHUB_CLIENT_ID and FASTCAS_GITHUB_CLIENT_SECRET are required")
	}
	if !s.Dev && registrationOrg == "" {
		registrationOrg = "FastR-D"
	}
	if registrationOrg != "" && !regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`).MatchString(registrationOrg) {
		return errors.New("invalid FASTCAS_GITHUB_REGISTRATION_ORG")
	}
	s.github = &gitHubProvider{clientID: clientID, secret: secret, registrationOrg: registrationOrg, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	return nil
}

func (s *Server) githubAvailable(w http.ResponseWriter) bool {
	if s.github == nil {
		problem(w, 404, "github_disabled", "GitHub 登录尚未配置")
		return false
	}
	return true
}
func (s *Server) githubRateLimit(w http.ResponseWriter, r *http.Request) bool {
	allowed, err := s.Store.Allow(r.Context(), "github:"+s.peer(r), 30, 5*time.Minute)
	if err != nil {
		failure(w, err)
		return false
	}
	if !allowed {
		w.Header().Set("Retry-After", "300")
		problem(w, 429, "rate_limited", "尝试次数过多，请稍后重试")
		return false
	}
	return true
}
func (s *Server) newGitHubAuthorization(w http.ResponseWriter, r *http.Request, f core.GitHubFlow) (string, error) {
	f.Verifier = core.RandomToken()
	state, browser, err := s.Store.NewGitHubFlow(r.Context(), f)
	if err != nil {
		return "", err
	}
	s.setCookie(w, "fastcas_github", browser, 600)
	challenge := sha256.Sum256([]byte(f.Verifier))
	q := url.Values{"client_id": {s.github.clientID}, "redirect_uri": {s.Issuer + "/auth/github/callback"}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}}
	if f.Purpose == "register" {
		scopes := "user:email"
		if s.github.registrationOrg != "" {
			scopes += " read:org"
		}
		q.Set("scope", scopes)
	}
	return "https://github.com/login/oauth/authorize?" + q.Encode(), nil
}
func (s *Server) githubStart(w http.ResponseWriter, r *http.Request) {
	if !s.githubAvailable(w) {
		return
	}
	if !s.originOK(r) || r.ParseForm() != nil {
		failure(w, core.ErrForbidden)
		return
	}
	got, want := r.Form.Get("csrf"), cookie(r, "fastcas_form")
	if got == "" || want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		failure(w, core.ErrForbidden)
		return
	}
	purpose := r.Form.Get("purpose")
	if purpose != "login" && purpose != "register" {
		failure(w, core.ErrForbidden)
		return
	}
	if !s.githubRateLimit(w, r) {
		return
	}
	f := core.GitHubFlow{Purpose: purpose, AuthRequestID: r.Form.Get("auth_request_id"), DeviceCode: safeDeviceCode(r.Form.Get("return_device"))}
	if f.AuthRequestID != "" {
		if _, err := s.Store.AuthRequestByID(r.Context(), f.AuthRequestID); err != nil {
			failure(w, err)
			return
		}
	}
	destination, err := s.newGitHubAuthorization(w, r, f)
	if err != nil {
		failure(w, err)
		return
	}
	s.setCookie(w, "fastcas_form", "", -1)
	http.Redirect(w, r, destination, http.StatusSeeOther)
}
func (s *Server) githubBind(w http.ResponseWriter, r *http.Request) {
	if !s.githubAvailable(w) {
		return
	}
	session, ok := s.authenticated(w, r, false)
	if !ok {
		return
	}
	if err := s.Store.RequireRecentAccountAuth(r.Context(), session.IdentityID, session.ID); err != nil {
		problem(w, 401, "reauthentication_required", "请先重新验证身份，再绑定 GitHub")
		return
	}
	if !s.githubRateLimit(w, r) {
		return
	}
	destination, err := s.newGitHubAuthorization(w, r, core.GitHubFlow{Purpose: "link", Subject: session.IdentityID, SessionID: session.ID})
	if err != nil {
		failure(w, err)
		return
	}
	send(w, 200, map[string]string{"authorization_url": destination})
}
func (g *gitHubProvider) request(ctx context.Context, method, path, token string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	endpoint := "https://api.github.com" + path
	if path == "/login/oauth/access_token" {
		endpoint = "https://github.com" + path
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "FastCAS")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("GitHub unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("GitHub request rejected")
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

var githubResultTemplate = authTemplate("github-result", `<!doctype html><html lang="zh-CN" data-fastui-theme="dark"><head>{{template "auth-head" .}}</head><body><main>{{template "auth-toolbar" .}}<h1>{{.Title}}</h1><p role="alert">{{.Message}}</p><a href="/login">返回登录</a></main></body></html>`)

func (s *Server) githubError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = githubResultTemplate.Execute(w, map[string]any{"Title": "GitHub 认证未完成", "Assets": s.authAssets, "Message": message})
}
func (s *Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	if !s.githubAvailable(w) {
		return
	}
	state, browser := r.URL.Query().Get("state"), cookie(r, "fastcas_github")
	if state == "" || browser == "" {
		s.githubError(w, 400, "认证请求无效，请重新发起。")
		return
	}
	f, err := s.Store.TakeGitHubFlow(r.Context(), state, browser, false)
	if err != nil {
		s.githubError(w, 400, "认证请求已过期、已使用或不属于当前浏览器，请重新发起。")
		return
	}
	s.setCookie(w, "fastcas_github", "", -1)
	if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		s.githubError(w, 400, "GitHub 授权已取消，请重新发起。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	err = s.github.request(ctx, "POST", "/login/oauth/access_token", "", url.Values{"client_id": {s.github.clientID}, "client_secret": {s.github.secret}, "code": {r.URL.Query().Get("code")}, "redirect_uri": {s.Issuer + "/auth/github/callback"}, "code_verifier": {f.Verifier}}, &token)
	if err != nil || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "bearer") {
		s.githubError(w, 502, "无法完成 GitHub 授权，请稍后重试。")
		return
	}
	var profile struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err = s.github.request(ctx, "GET", "/user", token.AccessToken, nil, &profile); err != nil || profile.ID <= 0 || profile.Login == "" {
		s.githubError(w, 502, "无法获取 GitHub 身份，请稍后重试。")
		return
	}
	if f.Purpose == "link" {
		session, err := s.Store.Session(ctx, cookie(r, "fastcas_session"))
		if err != nil || session.ID != f.SessionID || session.IdentityID != f.Subject {
			s.githubError(w, 401, "绑定时的登录会话已变化，请重新登录后发起绑定。")
			return
		}
		if err = s.Store.BindGitHub(ctx, f.Subject, f.SessionID, profile.ID, profile.Login); err != nil {
			if errors.Is(err, core.ErrConflict) {
				s.githubError(w, 409, "GitHub 或 FastCAS 账号已有绑定，不能重复绑定。")
			} else if errors.Is(err, core.ErrUnauthorized) {
				s.githubError(w, 401, "请重新验证 FastCAS 身份后再次发起绑定。")
			} else {
				s.githubError(w, 500, "无法保存 GitHub 绑定，请稍后重试。")
			}
			return
		}
		if s.ConsoleEnabled {
			http.Redirect(w, r, "/console/#security", http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/api/v1/me", http.StatusSeeOther)
		}
		return
	}
	user, err := s.Store.GitHubIdentity(ctx, profile.ID)
	if errors.Is(err, core.ErrGitHubNotLinked) && f.Purpose == "register" {
		if org := s.github.registrationOrg; org != "" {
			var membership struct {
				State string `json:"state"`
			}
			if err = s.github.request(ctx, "GET", "/user/memberships/orgs/"+url.PathEscape(org), token.AccessToken, nil, &membership); err != nil || membership.State != "active" {
				s.githubError(w, 403, "GitHub 注册仅允许 "+org+" 组织的正式成员；请确认成员资格及组织对 OAuth 应用的授权后重试。")
				return
			}
		}
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err = s.github.request(ctx, "GET", "/user/emails", token.AccessToken, nil, &emails); err != nil {
			s.githubError(w, 502, "无法读取 GitHub 邮箱，请重新授权后重试。")
			return
		}
		email := ""
		for _, e := range emails {
			if e.Primary && e.Verified {
				email = e.Email
				break
			}
		}
		if email == "" {
			s.githubError(w, 403, "请先在 GitHub 设置并验证主邮箱，再重新注册。")
			return
		}
		user, err = s.Store.RegisterGitHub(ctx, profile.ID, profile.Login, email, profile.Name)
	}
	if err != nil {
		switch {
		case errors.Is(err, core.ErrGitHubNotLinked):
			s.githubError(w, 403, "该 GitHub 尚未绑定 FastCAS。已有账号请先用原方式登录并绑定；新用户请选择 GitHub 注册。")
		case errors.Is(err, core.ErrConflict):
			s.githubError(w, 409, "邮箱或 GitHub 已被账号使用。请先登录已有 FastCAS 账号，再从安全中心绑定 GitHub。")
		case errors.Is(err, core.ErrUnauthorized):
			s.githubError(w, 403, "该 FastCAS 账号不可用，请联系管理员。")
		default:
			s.githubError(w, 500, "无法完成 GitHub 登录，请稍后重试。")
		}
		return
	}
	enabled, version, err := s.Store.GitHubLoginState(ctx, user.ID)
	if err != nil {
		failure(w, err)
		return
	}
	f.SubjectVersion = version
	if enabled {
		f.Purpose = "mfa"
		f.Subject = user.ID
		f.Verifier = ""
		challenge, browser, err := s.Store.NewGitHubFlow(ctx, *f)
		if err != nil {
			failure(w, err)
			return
		}
		s.setCookie(w, "fastcas_github_mfa", browser, 600)
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if f.AuthRequestID != "" {
			request, err := s.Store.AuthRequestByID(r.Context(), f.AuthRequestID)
			if err != nil {
				failure(w, err)
				return
			}
			if !s.authorizationFormPolicy(w, request.(*core.Authorization)) {
				return
			}
		}
		_ = githubMFATemplate.Execute(w, map[string]any{"Title": "双重验证", "Assets": s.authAssets, "Challenge": challenge})
		return
	}
	s.githubSession(w, r, user.ID, f, false)
}

var githubMFATemplate = authTemplate("github-mfa", `<!doctype html><html lang="zh-CN" data-fastui-theme="dark"><head>{{template "auth-head" .}}</head><body><main>{{template "auth-toolbar" .}}<h1>双重验证</h1><p>GitHub 身份已确认，请完成 FastCAS 双重验证。</p><form method="post" action="/auth/github/mfa"><input type="hidden" name="challenge" value="{{.Challenge}}"><label>动态验证码或恢复码<input class="fastui-input" name="code" autocomplete="one-time-code" required></label><button class="fastui-button fastui-button--primary">验证并登录</button></form></main></body></html>`)

func (s *Server) githubMFA(w http.ResponseWriter, r *http.Request) {
	if !s.githubAvailable(w) {
		return
	}
	if !s.originOK(r) || r.ParseForm() != nil {
		failure(w, core.ErrForbidden)
		return
	}
	if !s.githubRateLimit(w, r) {
		return
	}
	f, err := s.Store.TakeGitHubFlow(r.Context(), r.Form.Get("challenge"), cookie(r, "fastcas_github_mfa"), true)
	if err != nil {
		s.githubError(w, 400, "验证请求已过期或已使用，请重新登录。")
		return
	}
	s.setCookie(w, "fastcas_github_mfa", "", -1)
	if err = s.Store.VerifyMFA(r.Context(), f.Subject, r.Form.Get("code")); err != nil {
		s.githubError(w, 401, "双重验证码无效，请重新登录。")
		return
	}
	s.githubSession(w, r, f.Subject, f, true)
}
func (s *Server) githubSession(w http.ResponseWriter, r *http.Request, subject string, f *core.GitHubFlow, mfa bool) {
	token, csrf, err := s.Store.NewGitHubSession(r.Context(), subject, f.SubjectVersion, mfa)
	if err != nil {
		failure(w, err)
		return
	}
	s.setCookie(w, "fastcas_session", token, 28800)
	s.setCookie(w, "fastcas_csrf", csrf, 28800)
	destination := "/api/v1/me"
	if s.ConsoleEnabled {
		destination = "/console/"
	}
	if f.DeviceCode != "" {
		destination = "/device?user_code=" + url.QueryEscape(f.DeviceCode)
	}
	if f.AuthRequestID != "" {
		session, err := s.Store.Session(r.Context(), token)
		if err != nil {
			failure(w, err)
			return
		}
		if s.resumeAuthorization(w, r, f.AuthRequestID, session) {
			return
		}
		destination = "/login?auth_request_id=" + url.QueryEscape(f.AuthRequestID)
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}
