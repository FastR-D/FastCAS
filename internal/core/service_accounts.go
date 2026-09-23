package core

import (
	"context"
	"encoding/json"
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"
)

type ServiceAccount struct {
	Client    Client    `json:"client"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) ServiceAccounts(ctx context.Context, after string) ([]ServiceAccount, error) {
	rows, err := s.DB.Query(ctx, `SELECT config,active,created_at FROM applications WHERE id>$1 AND config->'grant_types'='["client_credentials"]'::jsonb ORDER BY id LIMIT 100`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []ServiceAccount{}
	for rows.Next() {
		var item ServiceAccount
		var data []byte
		if err = rows.Scan(&data, &item.Active, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &item.Client); err != nil {
			return nil, err
		}
		accounts = append(accounts, item)
	}
	return accounts, rows.Err()
}

func (s *Store) CreateServiceAccountAs(ctx context.Context, actor, id, name string, scopes, resources []string, development bool) (string, error) {
	secret := RandomToken()
	client := Client{ID: id, Name: name, Scopes: scopes, Resources: resources, Grants: []oidc.GrantType{oidc.GrantTypeClientCredentials}, Development: development}
	if err := s.RegisterClientAs(ctx, actor, client, secret); err != nil {
		return "", err
	}
	return secret, nil
}

// A disabled service identity loses its old secret and all centrally tracked
// tokens. Re-enabling returns a new secret exactly once; an old one never works.
func (s *Store) SetServiceAccountActiveAs(ctx context.Context, actor, id string, active bool) (string, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	var current bool
	if err = tx.QueryRow(ctx, `SELECT config,active FROM applications WHERE id=$1 FOR UPDATE`, id).Scan(&raw, &current); err != nil {
		return "", classify(err)
	}
	var client Client
	if err = json.Unmarshal(raw, &client); err != nil {
		return "", err
	}
	if !serviceClient(client) {
		return "", ErrForbidden
	}
	if current == active {
		return "", nil
	}
	secret := RandomToken()
	if _, err = tx.Exec(ctx, `UPDATE applications SET active=$2,secret_hash=$3,previous_secret_hash=NULL,previous_secret_expires_at=NULL WHERE id=$1`, id, active, Hash(secret)); err != nil {
		return "", err
	}
	if !active {
		if _, err = tx.Exec(ctx, `UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE client_id=$1 AND revoked_at IS NULL`, id); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `UPDATE token_families SET revoked_at=COALESCE(revoked_at,now()) WHERE client_id=$1 AND revoked_at IS NULL`, id); err != nil {
			return "", err
		}
	}
	action := "service_account.disable"
	if active {
		action = "service_account.enable"
	}
	if err = audit(ctx, tx, actor, action, id); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	if active {
		return secret, nil
	}
	return "", nil
}

func (s *Store) RotateServiceAccountSecretAs(ctx context.Context, actor, id string, grace time.Duration) (string, error) {
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT config FROM applications WHERE id=$1 AND active`, id).Scan(&raw); err != nil {
		return "", classify(err)
	}
	var client Client
	if err := json.Unmarshal(raw, &client); err != nil {
		return "", err
	}
	if !serviceClient(client) {
		return "", ErrForbidden
	}
	return s.RotateClientSecretAs(ctx, actor, id, grace)
}
