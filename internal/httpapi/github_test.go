package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/testutil"
	"github.com/pquerna/otp/totp"
)

type githubTransport func(*http.Request) (*http.Response, error)

func (f githubTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type githubTest struct {
	t                                   *testing.T
	server                              *Server
	cookies                             map[string]*http.Cookie
	profileID                           int64
	login, email, membership, challenge string
	emailVerified                       bool
	membershipStatus                    int
	orgCalls, exchanges                 int
}

func newGitHubTest(t *testing.T, org string) *githubTest {
	t.Helper()
	return newGitHubTestEnvironment(t, org, true)
}

func newGitHubTestEnvironment(t *testing.T, org string, dev bool) *githubTest {
	t.Helper()
	store := testutil.Store(t)
	store.Dev = dev
	issuer := "https://cas.example.test"
	if dev {
		issuer = "http://127.0.0.1:8900"
	}
	s, err := New(store, issuer, dev)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfigureGitHub("test-client", "test-secret", org); err != nil {
		t.Fatal(err)
	}
	g := &githubTest{t: t, server: s, cookies: map[string]*http.Cookie{}, profileID: 123, login: "octocat", email: "octocat@example.test", membership: "active", emailVerified: true, membershipStatus: 200}
	s.github.client.Transport = githubTransport(func(r *http.Request) (*http.Response, error) {
		var data any
		status := 200
		switch r.URL.Host + r.URL.Path {
		case "github.com/login/oauth/access_token":
			g.exchanges++
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if (g.challenge != "" && base64.RawURLEncoding.EncodeToString(challenge[:]) != g.challenge) || len(r.Form.Get("code_verifier")) != 43 || r.Form.Get("client_secret") != "test-secret" || r.Form.Get("redirect_uri") != s.Issuer+"/auth/github/callback" {
				t.Error("token exchange lost PKCE or trusted callback")
			}
			data = map[string]any{"access_token": "private-github-access-token", "token_type": "bearer"}
		case "api.github.com/user":
			data = map[string]any{"id": g.profileID, "login": g.login, "name": "Octo Cat"}
		case "api.github.com/user/emails":
			data = []any{map[string]any{"email": g.email, "primary": true, "verified": g.emailVerified}}
		case "api.github.com/user/memberships/orgs/FastR-D":
			g.orgCalls++
			status = g.membershipStatus
			data = map[string]any{"state": g.membership}
		default:
			t.Fatalf("unexpected GitHub request: %s", r.URL)
		}
		if r.URL.Host == "api.github.com" && r.Header.Get("Authorization") != "Bearer private-github-access-token" {
			t.Error("missing API authentication")
		}
		body, _ := json.Marshal(data)
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	return g
}
func (g *githubTest) request(method, path string, form url.Values) *httptest.ResponseRecorder {
	g.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, g.server.Issuer+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", g.server.Issuer)
	}
	for _, c := range g.cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	g.server.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(g.cookies, c.Name)
		} else {
			g.cookies[c.Name] = c
		}
	}
	return w
}
func (g *githubTest) start(purpose, query string) string {
	g.t.Helper()
	page := g.request("GET", "/login"+query, nil)
	if page.Code != 200 {
		g.t.Fatalf("login page: %d %s", page.Code, page.Body.String())
	}
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "https://github.com") {
		g.t.Error("CSP blocks GitHub form redirect")
	}
	form := url.Values{"csrf": {g.cookies["fastcas_form"].Value}, "purpose": {purpose}}
	q, _ := url.ParseQuery(strings.TrimPrefix(query, "?"))
	form.Set("auth_request_id", q.Get("auth_request_id"))
	form.Set("return_device", q.Get("return_device"))
	w := g.request("POST", "/auth/github/start", form)
	if w.Code != 303 {
		g.t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	return g.authorization(w.Header().Get("Location"))
}
func (g *githubTest) authorization(location string) string {
	g.t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		g.t.Fatal(err)
	}
	if u.Host != "github.com" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("state") == "" {
		g.t.Fatal("invalid authorization request")
	}
	g.challenge = u.Query().Get("code_challenge")
	return "/auth/github/callback?code=test-code&state=" + url.QueryEscape(u.Query().Get("state"))
}
func (g *githubTest) localSession(user *core.Identity) {
	g.t.Helper()
	token, csrf, err := g.server.Store.NewSession(context.Background(), user.ID)
	if err != nil {
		g.t.Fatal(err)
	}
	g.cookies["fastcas_session"] = &http.Cookie{Name: "fastcas_session", Value: token}
	g.cookies["fastcas_csrf"] = &http.Cookie{Name: "fastcas_csrf", Value: csrf}
}
func (g *githubTest) bindRequest() *httptest.ResponseRecorder {
	g.t.Helper()
	req := httptest.NewRequest("POST", g.server.Issuer+"/api/v1/me/github", nil)
	req.Header.Set("Origin", g.server.Issuer)
	req.Header.Set("X-CSRF-Token", g.cookies["fastcas_csrf"].Value)
	for _, c := range g.cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	g.server.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		g.cookies[c.Name] = c
	}
	return w
}
func (g *githubTest) bindStart() string {
	w := g.bindRequest()
	if w.Code != 200 {
		g.t.Fatalf("bind start: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		URL string `json:"authorization_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		g.t.Fatal(err)
	}
	return g.authorization(result.URL)
}
func (g *githubTest) assertNoAccount() {
	g.t.Helper()
	var count int
	if err := g.server.Store.DB.QueryRow(context.Background(), `SELECT count(*) FROM github_accounts`).Scan(&count); err != nil {
		g.t.Fatal(err)
	}
	if count != 0 || g.cookies["fastcas_session"] != nil {
		g.t.Fatal("rejected flow created an account or browser session")
	}
}
func TestGitHubConfiguration(t *testing.T) {
	s := &Server{}
	if err := s.ConfigureGitHub("id", "secret", ""); err != nil || s.github.registrationOrg != "FastR-D" {
		t.Fatal("production must default to FastR-D")
	}
	for _, tc := range [][3]string{{"id", "", ""}, {"", "secret", ""}, {"id", "secret", "../org"}} {
		if s.ConfigureGitHub(tc[0], tc[1], tc[2]) == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	s = &Server{Dev: true}
	if err := s.ConfigureGitHub("id", "secret", ""); err != nil || s.github.registrationOrg != "" {
		t.Fatal("development should allow unrestricted registration")
	}
	s = &Server{}
	if s.ConfigureGitHub("", "", "") != nil || s.github != nil {
		t.Fatal("unconfigured provider should stay disabled")
	}
}
func TestGitHubRegistrationPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		status      int
		verified    bool
		want        int
	}{
		{"active member", "active", 200, true, 303}, {"pending invitation", "pending", 200, true, 403},
		{"not member", "", 404, true, 403}, {"org approval denied", "", 403, true, 403},
		{"GitHub outage", "", 503, true, 403}, {"unverified email", "active", 200, false, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGitHubTest(t, "FastR-D")
			g.membership = tc.state
			g.membershipStatus = tc.status
			g.emailVerified = tc.verified
			callback := g.start("register", "")
			w := g.request("GET", callback, nil)
			if w.Code != tc.want {
				t.Fatalf("callback: %d %s", w.Code, w.Body.String())
			}
			if g.orgCalls != 1 {
				t.Fatal("registration did not check membership")
			}
			if tc.want != 303 {
				g.assertNoAccount()
				return
			}
			me := g.request("GET", "/api/v1/me", nil)
			if me.Code != 200 || !strings.Contains(me.Body.String(), `"email_verified":true`) || !strings.Contains(me.Body.String(), `"password_enabled":false`) {
				t.Fatalf("registered identity: %d %s", me.Code, me.Body.String())
			}
			if strings.Contains(me.Body.String(), "private-github-access-token") {
				t.Fatal("GitHub token exposed")
			}
			if replay := g.request("GET", callback, nil); replay.Code != 400 {
				t.Fatal("callback replay accepted")
			}
			g.membershipStatus = 503
			g.orgCalls = 0
			g.login = "renamed-user"
			if login := g.request("GET", g.start("login", ""), nil); login.Code != 303 || g.orgCalls != 0 {
				t.Fatal("existing login must use stable ID without registration membership checks")
			}
		})
	}
	t.Run("development unrestricted", func(t *testing.T) {
		g := newGitHubTest(t, "")
		if w := g.request("GET", g.start("register", ""), nil); w.Code != 303 || g.orgCalls != 0 {
			t.Fatal("development registration failed")
		}
	})
	t.Run("login cannot create account", func(t *testing.T) {
		g := newGitHubTest(t, "FastR-D")
		if w := g.request("GET", g.start("login", ""), nil); w.Code != 403 {
			t.Fatal("login created an unlinked account")
		}
		g.assertNoAccount()
	})
	t.Run("email collision cannot merge", func(t *testing.T) {
		g := newGitHubTest(t, "FastR-D")
		if _, err := g.server.Store.CreateIdentity(context.Background(), g.email, "Existing", "existing-password-123", "member"); err != nil {
			t.Fatal(err)
		}
		if w := g.request("GET", g.start("register", ""), nil); w.Code != 409 {
			t.Fatal("email collision accepted")
		}
		g.assertNoAccount()
	})
}
func TestGitHubStateAndCSRF(t *testing.T) {
	t.Run("origin and form CSRF", func(t *testing.T) {
		g := newGitHubTest(t, "")
		if w := g.request("POST", "/auth/github/start", url.Values{"purpose": {"register"}}); w.Code != 403 {
			t.Fatal("missing CSRF accepted")
		}
	})
	for _, scenario := range []string{"wrong browser", "expired", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			g := newGitHubTest(t, "")
			callback := g.start("register", "")
			switch scenario {
			case "wrong browser":
				g.cookies["fastcas_github"].Value = "attacker"
			case "expired":
				if _, err := g.server.Store.DB.Exec(context.Background(), `UPDATE github_flows SET expires_at=now()-interval '1 second'`); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				callback = strings.ReplaceAll(callback, "code=test-code", "error=access_denied")
			}
			if w := g.request("GET", callback, nil); w.Code != 400 {
				t.Fatalf("invalid state accepted: %d", w.Code)
			}
			if g.exchanges != 0 {
				t.Fatal("invalid callback reached GitHub")
			}
			g.assertNoAccount()
		})
	}
}
func TestGitHubExistingAccountBinding(t *testing.T) {
	for _, scenario := range []string{"success", "stale start", "stale callback", "different session", "CSRF", "duplicate GitHub", "duplicate identity"} {
		t.Run(scenario, func(t *testing.T) {
			g := newGitHubTest(t, "FastR-D")
			ctx := context.Background()
			user, err := g.server.Store.CreateIdentity(ctx, "existing@example.test", "Existing", "existing-password-123", "member")
			if err != nil {
				t.Fatal(err)
			}
			g.localSession(user)
			session, _ := g.server.Store.Session(ctx, g.cookies["fastcas_session"].Value)
			if scenario == "stale start" {
				if _, err = g.server.Store.DB.Exec(ctx, `UPDATE browser_sessions SET authenticated_at=now()-interval '6 minutes'`); err != nil {
					t.Fatal(err)
				}
				if w := g.bindRequest(); w.Code != 401 {
					t.Fatal("stale session accepted")
				}
				return
			}
			if scenario == "CSRF" {
				g.cookies["fastcas_csrf"].Value = "wrong"
				if w := g.bindRequest(); w.Code != 403 {
					t.Fatal("invalid CSRF accepted")
				}
				return
			}
			if scenario == "duplicate identity" {
				if err = g.server.Store.BindGitHub(ctx, user.ID, session.ID, 456, "other"); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "duplicate GitHub" {
				if _, err = g.server.Store.RegisterGitHub(ctx, 123, "octocat", g.email, "Octocat"); err != nil {
					t.Fatal(err)
				}
			}
			callback := g.bindStart()
			if scenario == "different session" {
				g.localSession(user)
			}
			if scenario == "stale callback" {
				if _, err = g.server.Store.DB.Exec(ctx, `UPDATE browser_sessions SET authenticated_at=now()-interval '6 minutes'`); err != nil {
					t.Fatal(err)
				}
			}
			w := g.request("GET", callback, nil)
			want := 303
			if scenario == "different session" || scenario == "stale callback" {
				want = 401
			}
			if strings.HasPrefix(scenario, "duplicate") {
				want = 409
			}
			if w.Code != want {
				t.Fatalf("binding: %d %s", w.Code, w.Body.String())
			}
			if g.orgCalls != 0 {
				t.Fatal("binding must not require org membership")
			}
			if want == 303 {
				actual, err := g.server.Store.GitHubIdentity(ctx, 123)
				if err != nil || actual.ID != user.ID || actual.Email != "existing@example.test" {
					t.Fatal("binding changed identity")
				}
			}
		})
	}
}
func TestGitHubMFAAndAccountStatus(t *testing.T) {
	for _, scenario := range []string{"MFA", "disabled", "reset invalidates MFA"} {
		t.Run(scenario, func(t *testing.T) {
			g := newGitHubTest(t, "")
			ctx := context.Background()
			user, err := g.server.Store.RegisterGitHub(ctx, 123, g.login, g.email, "Octocat")
			if err != nil {
				t.Fatal(err)
			}
			g.localSession(user)
			session, _ := g.server.Store.Session(ctx, g.cookies["fastcas_session"].Value)
			uri, err := g.server.Store.BeginMFA(ctx, user.ID)
			if err != nil {
				t.Fatal(err)
			}
			parsed, _ := url.Parse(uri)
			code, err := totp.GenerateCode(parsed.Query().Get("secret"), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			recovery, err := g.server.Store.ConfirmMFA(ctx, user.ID, session.ID, code)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "disabled" {
				if err = g.server.Store.SetIdentityStatus(ctx, "admin", user.ID, "disabled"); err != nil {
					t.Fatal(err)
				}
			}
			delete(g.cookies, "fastcas_session")
			delete(g.cookies, "fastcas_csrf")
			w := g.request("GET", g.start("login", ""), nil)
			if scenario == "disabled" {
				if w.Code != 403 {
					t.Fatal("disabled identity logged in")
				}
				return
			}
			if w.Code != 200 || g.cookies["fastcas_session"] != nil {
				t.Fatalf("MFA bypass: %d %s", w.Code, w.Body.String())
			}
			match := regexp.MustCompile(`name="challenge" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
			if len(match) != 2 {
				t.Fatal("missing MFA challenge")
			}
			if scenario == "reset invalidates MFA" {
				if err = g.server.Store.SetIdentityStatus(ctx, "admin", user.ID, "disabled"); err != nil {
					t.Fatal(err)
				}
				if err = g.server.Store.SetIdentityStatus(ctx, "admin", user.ID, "active"); err != nil {
					t.Fatal(err)
				}
			}
			w = g.request("POST", "/auth/github/mfa", url.Values{"challenge": {match[1]}, "code": {recovery[0]}})
			if scenario == "reset invalidates MFA" {
				if w.Code != 400 {
					t.Fatal("old MFA challenge survived disable")
				}
				return
			}
			if w.Code != 303 || g.cookies["fastcas_session"] == nil {
				t.Fatalf("MFA login: %d %s", w.Code, w.Body.String())
			}
			session, err = g.server.Store.Session(ctx, g.cookies["fastcas_session"].Value)
			if err != nil {
				t.Fatal(err)
			}
			if err = g.server.Store.RequireRecentAccountAuth(ctx, user.ID, session.ID); err != nil {
				t.Fatal("MFA not marked on GitHub session")
			}
			if w = g.request("POST", "/auth/github/mfa", url.Values{"challenge": {match[1]}, "code": {recovery[1]}}); w.Code != 400 {
				t.Fatal("MFA challenge replay accepted")
			}
		})
	}
}
func TestGitHubReturnsToDeviceAndOIDCConsent(t *testing.T) {
	g := newGitHubTest(t, "")
	w := g.request("GET", g.start("register", "?return_device=BCDF-GHJK"), nil)
	if w.Code != 303 || w.Header().Get("Location") != "/device?user_code=BCDF-GHJK" {
		t.Fatalf("device return lost: %d %s", w.Code, w.Header().Get("Location"))
	}
	ctx := context.Background()
	if err := g.server.Store.RegisterClient(ctx, core.Client{ID: "rp", Name: "Relying Party", Development: true, Redirects: []string{"http://127.0.0.1:3000/callback"}, Scopes: []string{"openid"}}, "test-client-secret-long-enough-1234"); err != nil {
		t.Fatal(err)
	}
	challenge := sha256.Sum256([]byte(core.RandomToken()))
	q := url.Values{"client_id": {"rp"}, "redirect_uri": {"http://127.0.0.1:3000/callback"}, "response_type": {"code"}, "scope": {"openid"}, "state": {"rp-state"}, "nonce": {"rp-nonce"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}}
	w = g.request("GET", "/oauth/authorize?"+q.Encode(), nil)
	loginURL, err := url.Parse(w.Header().Get("Location"))
	if err != nil || loginURL.Query().Get("auth_request_id") == "" {
		t.Fatalf("OIDC start: %d %s", w.Code, w.Body.String())
	}
	w = g.request("GET", g.start("login", "?"+loginURL.RawQuery), nil)
	if w.Code != 303 || w.Header().Get("Location") != loginURL.RequestURI() {
		t.Fatalf("OIDC consent return lost: %d %s", w.Code, w.Header().Get("Location"))
	}
	w = g.request("GET", w.Header().Get("Location"), nil)
	if !strings.Contains(w.Body.String(), "确认并继续") {
		t.Fatal("GitHub login bypassed consent")
	}
	w = g.request("POST", "/login", url.Values{"csrf": {g.cookies["fastcas_form"].Value}, "auth_request_id": {loginURL.Query().Get("auth_request_id")}, "action": {"approve"}})
	if w.Code != 303 {
		t.Fatalf("consent: %d %s", w.Code, w.Body.String())
	}
	providerURL, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	w = g.request("GET", providerURL.RequestURI(), nil)
	result, err := url.Parse(w.Header().Get("Location"))
	if err != nil || w.Code != 302 || result.Query().Get("state") != "rp-state" || result.Query().Get("code") == "" {
		t.Fatal("GitHub login lost OIDC authorization result")
	}
}

func TestGitHubAgainstRealBrowser(t *testing.T) {
	if os.Getenv("FASTCAS_BROWSER_CONTRACT") != "1" {
		t.Skip("set FASTCAS_BROWSER_CONTRACT=1 to run Chrome")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	for _, scenario := range []string{"register", "bind"} {
		t.Run(scenario, func(t *testing.T) {
			g := newGitHubTest(t, "FastR-D")
			host := httptest.NewUnstartedServer(nil)
			g.server.Issuer = "http://" + host.Listener.Addr().String()
			g.server.ConsoleEnabled = true
			handler, err := Console(g.server, filepath.Join(root, "web/dist"))
			if err != nil {
				t.Fatal(err)
			}
			host.Config.Handler = handler
			host.Start()
			defer host.Close()
			if scenario == "bind" {
				if _, err = g.server.Store.CreateIdentity(context.Background(), "existing@example.test", "Existing", "existing-password-123", "member"); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "tests/github_contract.mjs")
			cmd.Dir = filepath.Join(root, "web")
			cmd.Env = append(os.Environ(), "FASTCAS_CONTRACT_ORIGIN="+g.server.Issuer, "FASTCAS_GITHUB_SCENARIO="+scenario)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("GitHub browser contract: %v\n%s", err, output)
			}
			t.Log(string(output))
		})
	}
}

func TestGitHubRegistrationRoleByEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name string
		dev  bool
		role string
	}{
		{"production", false, "admin"}, {"development", true, "member"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGitHubTestEnvironment(t, "", tc.dev)
			w := g.request("GET", g.start("register", ""), nil)
			if w.Code != 303 {
				t.Fatalf("registration: %d %s", w.Code, w.Body.String())
			}
			user, err := g.server.Store.GitHubIdentity(context.Background(), g.profileID)
			if err != nil {
				t.Fatal(err)
			}
			if user.Role != tc.role {
				t.Fatalf("stored role=%s want=%s", user.Role, tc.role)
			}
			me := g.request("GET", "/api/v1/me", nil)
			if me.Code != 200 || !strings.Contains(me.Body.String(), `"role":"`+tc.role+`"`) {
				t.Fatalf("session role: %d %s", me.Code, me.Body.String())
			}
			if !tc.dev {
				if g.orgCalls != 1 {
					t.Fatal("production administrator registration did not check FastR-D membership")
				}
				if admin := g.request("GET", "/api/v1/admin/identities", nil); admin.Code != 403 {
					t.Fatal("new administrator bypassed MFA requirement")
				}
			}
		})
	}
	t.Run("production rejects nonmember", func(t *testing.T) {
		g := newGitHubTestEnvironment(t, "", false)
		g.membershipStatus = 404
		if w := g.request("GET", g.start("register", ""), nil); w.Code != 403 {
			t.Fatal("nonmember became administrator")
		}
		g.assertNoAccount()
	})
	t.Run("production existing member keeps role", func(t *testing.T) {
		g := newGitHubTestEnvironment(t, "", false)
		ctx := context.Background()
		user, err := g.server.Store.CreateIdentity(ctx, "existing@example.test", "Existing", "existing-password-123", "member")
		if err != nil {
			t.Fatal(err)
		}
		g.localSession(user)
		if w := g.request("GET", g.bindStart(), nil); w.Code != 303 {
			t.Fatalf("binding: %d %s", w.Code, w.Body.String())
		}
		for _, purpose := range []string{"login", "register"} {
			if w := g.request("GET", g.start(purpose, ""), nil); w.Code != 303 {
				t.Fatalf("%s: %d %s", purpose, w.Code, w.Body.String())
			}
			actual, err := g.server.Store.Identity(ctx, user.ID)
			if err != nil {
				t.Fatal(err)
			}
			if actual.Role != "member" {
				t.Fatalf("existing account promoted during %s", purpose)
			}
		}
		if g.orgCalls != 0 {
			t.Fatal("existing binding triggered registration policy")
		}
	})
}
