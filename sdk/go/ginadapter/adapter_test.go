package ginadapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fastcas "github.com/FastR-D/FastCAS/sdk/go"
	"github.com/gin-gonic/gin"
)

type testTransactions struct{}

func (testTransactions) Put(context.Context, fastcas.Transaction) error { return nil }
func (testTransactions) Take(context.Context, string) (*fastcas.Transaction, error) {
	return nil, nil
}

func TestSignedNotificationTransportBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client, err := fastcas.New(fastcas.Config{Issuer: "https://cas.example.test", ClientID: "task", ClientSecret: strings.Repeat("s", 32), RedirectURI: "https://task.example.test/callback"}, testTransactions{})
	if err != nil {
		t.Fatal(err)
	}
	provider := func() (*fastcas.Client, error) { return client, nil }
	router := gin.New()
	router.POST("/events", Events(provider, func(context.Context, fastcas.Notification) error {
		t.Fatal("unverified event reached application")
		return nil
	}))
	router.POST("/logout", Logout(provider, func(context.Context, fastcas.LogoutNotice) error {
		t.Fatal("unverified logout reached application")
		return nil
	}))
	router.POST("/unavailable", Events(func() (*fastcas.Client, error) { return nil, errors.New("off") }, func(context.Context, fastcas.Notification) error { return nil }))
	cases := []struct {
		path, contentType, body string
		want                    int
	}{
		{"/events", "application/json", "{}", 415},
		{"/events", "application/jwt", strings.Repeat("x", MaxNotificationBytes+1), 413},
		{"/events", "application/jwt", "not-a-signed-token", 401},
		{"/logout", "application/x-www-form-urlencoded", "x=y", 400},
		{"/logout", "application/x-www-form-urlencoded", "logout_token=invalid", 401},
		{"/unavailable", "application/jwt", "ignored", 503},
	}
	for _, test := range cases {
		req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
		req.Header.Set("Content-Type", test.contentType)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != test.want {
			t.Errorf("%s %s: got %d, want %d", test.path, test.contentType, w.Code, test.want)
		}
	}
}

func TestResourceMiddlewareDoesNotInstallGlobalGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/local", func(c *gin.Context) { c.Status(204) })
	router.GET("/resource", RequireAccessToken(func() (*fastcas.Client, error) { return nil, errors.New("off") }, "task", []string{"task:read"}, true), func(c *gin.Context) { c.Status(204) })
	for _, test := range []struct {
		path, bearer string
		want         int
	}{
		{"/local", "", 204},
		{"/resource", "", 401},
		{"/resource", "Bearer opaque", 503},
	} {
		req := httptest.NewRequest(http.MethodGet, test.path, nil)
		if test.bearer != "" {
			req.Header.Set("Authorization", test.bearer)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != test.want {
			t.Errorf("%s: got %d, want %d", test.path, w.Code, test.want)
		}
	}
}
