package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	fastcas "github.com/FastR-D/FastCAS/sdk/go"
)

// Service-only processes must not accidentally start an interactive flow.
type noBrowserTransactions struct{}

func (noBrowserTransactions) Put(context.Context, fastcas.Transaction) error {
	return errors.New("this service example cannot start browser login")
}
func (noBrowserTransactions) Take(context.Context, string) (*fastcas.Transaction, error) {
	return nil, errors.New("this service example cannot consume browser callbacks")
}

func run() error {
	issuer := strings.TrimSuffix(os.Getenv("FASTCAS_ISSUER"), "/")
	clientID, secret := os.Getenv("FASTCAS_CLIENT_ID"), os.Getenv("FASTCAS_CLIENT_SECRET")
	audience, scope := os.Getenv("FASTCAS_AUDIENCE"), os.Getenv("FASTCAS_SCOPE")
	if issuer == "" || clientID == "" || secret == "" || audience == "" || scope == "" {
		return errors.New("missing FASTCAS service example configuration")
	}
	sdk, err := fastcas.New(fastcas.Config{Issuer: issuer, ClientID: clientID, ClientSecret: secret,
		RedirectURI: issuer + "/unused-service-callback", AllowLoopbackHTTP: os.Getenv("FASTCAS_ALLOW_LOOPBACK_HTTP") == "true"}, noBrowserTransactions{})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	token, err := sdk.ClientCredentials(ctx, []string{scope})
	if err != nil {
		return err
	}
	claims, err := sdk.VerifyAccessToken(ctx, token.AccessToken, audience, scope)
	if err != nil {
		return err
	}
	current, err := sdk.IntrospectToken(ctx, token.AccessToken)
	if err != nil {
		return err
	}
	if claims.Subject != clientID || !claims.Service || !current.Active {
		return errors.New("service token identity or central status mismatch")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"client_id": clientID, "audience": audience, "scope": scope, "active": true})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
