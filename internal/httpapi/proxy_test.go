package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestProxyPeerRequiresAuthenticatedAddress(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	tests := []struct {
		name, key, address string
		extraAddress       bool
		want               string
	}{
		{name: "untrusted header", address: "198.51.100.10", want: "172.18.0.3"},
		{name: "wrong secret", key: "wrong", address: "198.51.100.10", want: "172.18.0.3"},
		{name: "valid IPv4", key: secret, address: "198.51.100.10", want: "198.51.100.10"},
		{name: "valid IPv6", key: secret, address: "2001:db8::10", want: "2001:db8::10"},
		{name: "malformed address", key: secret, address: "198.51.100.10, 203.0.113.8", want: "172.18.0.3"},
		{name: "duplicate address", key: secret, address: "198.51.100.10", extraAddress: true, want: "172.18.0.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = "172.18.0.3:1234"
			if tt.key != "" {
				r.Header.Set("X-FastCAS-Proxy-Secret", tt.key)
			}
			if tt.address != "" {
				r.Header.Set("X-FastCAS-Client-IP", tt.address)
			}
			if tt.extraAddress {
				r.Header.Add("X-FastCAS-Client-IP", "203.0.113.8")
			}
			if got := (&Server{ProxySecret: secret}).peer(r); got != tt.want {
				t.Fatalf("peer = %q, want %q", got, tt.want)
			}
		})
	}
}
