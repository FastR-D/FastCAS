package core

import (
	"context"
	"regexp"
	"slices"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/jackc/pgx/v5"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

var installationRefPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type DeviceInstallation struct {
	ID              string     `json:"id"`
	ClientID        string     `json:"client_id"`
	ClientName      string     `json:"client_name"`
	InstallationRef string     `json:"installation_id"`
	CreatedAt       time.Time  `json:"created_at"`
	RevokedAt       *time.Time `json:"revoked_at"`
}

func (s *Store) DeviceAccessToken(ctx context.Context, raw, issuer string) (*Token, error) {
	if len(raw) < 100 || len(raw) > 16<<10 {
		return nil, ErrUnauthorized
	}
	parsed, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil || len(parsed.Headers) != 1 || parsed.Headers[0].KeyID == "" {
		return nil, ErrUnauthorized
	}
	keys, err := s.KeySet(ctx)
	if err != nil {
		return nil, err
	}
	var claims jwt.Claims
	var extra struct {
		Use      string `json:"token_use"`
		ClientID string `json:"client_id"`
	}
	verified := false
	for _, key := range keys {
		if key.ID() == parsed.Headers[0].KeyID && parsed.Claims(key.Key(), &claims, &extra) == nil {
			verified = true
			break
		}
	}
	if !verified || claims.Validate(jwt.Expected{Issuer: issuer, Time: time.Now()}) != nil || claims.ID == "" || extra.Use != "access" {
		return nil, ErrUnauthorized
	}
	token, err := s.ActiveToken(ctx, claims.ID)
	if err != nil || token.Grant != "device_code" || token.ClientID != extra.ClientID || token.Subject != claims.Subject || len(claims.Audience) != 1 || claims.Audience[0] != token.ClientID {
		return nil, ErrUnauthorized
	}
	return token, nil
}

// Registration consumes one approved device access token for one local
// installation. The returned management secret can only query/revoke this row.
func (s *Store) RegisterDeviceInstallation(ctx context.Context, token *Token, installationRef string) (*DeviceInstallation, string, error) {
	if token == nil || token.Grant != "device_code" || token.Service || token.Delegated || token.SessionID != "" || !installationRefPattern.MatchString(installationRef) {
		return nil, "", ErrForbidden
	}
	active, err := s.ActiveToken(ctx, token.ID)
	if err != nil || active.ClientID != token.ClientID || active.Subject != token.Subject || active.Grant != "device_code" {
		return nil, "", ErrUnauthorized
	}
	client, err := s.Client(ctx, token.ClientID)
	if err != nil || !client.Public || !slices.Contains(client.Grants, oidc.GrantTypeDeviceCode) {
		return nil, "", ErrForbidden
	}
	secret := RandomToken()
	item := &DeviceInstallation{ID: RandomToken(), ClientID: token.ClientID, ClientName: client.Name, InstallationRef: installationRef}
	err = s.DB.QueryRow(ctx, `INSERT INTO device_installations(id,client_id,subject,installation_ref,device_token_id,secret_hash)
 VALUES($1,$2,$3,$4::uuid,$5,$6) RETURNING created_at`, item.ID, item.ClientID, token.Subject, item.InstallationRef, token.ID, Hash(secret)).Scan(&item.CreatedAt)
	if err != nil {
		return nil, "", classify(err)
	}
	return item, secret, nil
}

const deviceInstallationPageSize = 50

func (s *Store) DeviceInstallations(ctx context.Context, subject, before string) ([]DeviceInstallation, string, error) {
	var rows pgx.Rows
	var err error
	query := `SELECT d.id,d.client_id,a.name,d.installation_ref::text,d.created_at,d.revoked_at
 FROM device_installations d JOIN applications a ON a.id=d.client_id WHERE d.subject=$1`
	if before == "" {
		rows, err = s.DB.Query(ctx, query+` ORDER BY d.created_at DESC,d.id DESC LIMIT $2`, subject, deviceInstallationPageSize+1)
	} else {
		var cursorTime time.Time
		if err = s.DB.QueryRow(ctx, `SELECT created_at FROM device_installations WHERE id=$1 AND subject=$2`, before, subject).Scan(&cursorTime); err != nil {
			return nil, "", ErrForbidden
		}
		rows, err = s.DB.Query(ctx, query+` AND (d.created_at,d.id)<($2,$3) ORDER BY d.created_at DESC,d.id DESC LIMIT $4`,
			subject, cursorTime, before, deviceInstallationPageSize+1)
	}
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []DeviceInstallation{}
	for rows.Next() {
		var item DeviceInstallation
		if err = rows.Scan(&item.ID, &item.ClientID, &item.ClientName, &item.InstallationRef, &item.CreatedAt, &item.RevokedAt); err != nil {
			return nil, "", err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > deviceInstallationPageSize {
		items = items[:deviceInstallationPageSize]
		next = items[len(items)-1].ID
	}
	return items, next, nil
}

func (s *Store) DeviceInstallationBySecret(ctx context.Context, id, secret string) (*DeviceInstallation, error) {
	if len(secret) < 32 {
		return nil, ErrUnauthorized
	}
	var item DeviceInstallation
	err := s.DB.QueryRow(ctx, `SELECT d.id,d.client_id,a.name,d.installation_ref::text,d.created_at,d.revoked_at
 FROM device_installations d JOIN applications a ON a.id=d.client_id WHERE d.id=$1 AND d.secret_hash=$2`, id, Hash(secret)).
		Scan(&item.ID, &item.ClientID, &item.ClientName, &item.InstallationRef, &item.CreatedAt, &item.RevokedAt)
	if err != nil {
		return nil, ErrUnauthorized
	}
	return &item, nil
}

func (s *Store) RevokeDeviceInstallation(ctx context.Context, id, subject string) error {
	return s.revokeDeviceInstallation(ctx, id, `subject=$2`, subject, subject)
}

func (s *Store) RevokeDeviceInstallationWithSecret(ctx context.Context, id, secret string) error {
	if len(secret) < 32 {
		return ErrUnauthorized
	}
	return s.revokeDeviceInstallation(ctx, id, `secret_hash=$2`, Hash(secret), "device:"+id)
}

func (s *Store) revokeDeviceInstallation(ctx context.Context, id, condition, value, actor string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var alreadyRevoked bool
	var deviceTokenID string
	err = tx.QueryRow(ctx, `SELECT revoked_at IS NOT NULL,device_token_id FROM device_installations WHERE id=$1 AND `+condition+` FOR UPDATE`, id, value).Scan(&alreadyRevoked, &deviceTokenID)
	if err == pgx.ErrNoRows {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE access_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1`, deviceTokenID); err != nil {
		return err
	}
	if alreadyRevoked {
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE device_installations SET revoked_at=now() WHERE id=$1`, id); err != nil {
		return err
	}
	if err = audit(ctx, tx, actor, "device_installation.revoke", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
