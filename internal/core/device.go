package core

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

var _ op.DeviceAuthorizationStorage = (*Store)(nil)

func (s *Store) StoreDeviceAuthorization(ctx context.Context, clientID, deviceCode, userCode string, expires time.Time, scopes []string) error {
	c, err := s.Client(ctx, clientID)
	if err != nil || !slices.Contains(c.Grants, oidc.GrantTypeDeviceCode) || !c.Public {
		return ErrForbidden
	}
	if len(scopes) == 0 || !slices.Contains(scopes, oidc.ScopeOpenID) || slices.Contains(scopes, oidc.ScopeOfflineAccess) {
		return oidc.ErrInvalidScope()
	}
	for _, scope := range scopes {
		if !slices.Contains(c.Scopes, scope) {
			return oidc.ErrInvalidScope()
		}
	}
	allowed, err := s.Allow(ctx, "device-start:"+clientID, 30, time.Minute)
	if err != nil || !allowed {
		return ErrForbidden
	}
	data, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `DELETE FROM device_authorizations WHERE expires_at<now()-interval '1 hour'`)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO device_authorizations(device_code_hash,user_code,client_id,scopes,expires_at)
	 VALUES($1,$2,$3,$4,$5)`, Hash(deviceCode), userCode, clientID, data, expires)
	return classify(err)
}

// The polling timestamp and consume transition are serialized in PostgreSQL.
// The OAuth library maps context.DeadlineExceeded to the RFC 8628 slow_down error.
func (s *Store) GetDeviceAuthorizatonState(ctx context.Context, clientID, deviceCode string) (*op.DeviceAuthorizationState, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var state op.DeviceAuthorizationState
	var scopes []byte
	var status, subject string
	var authTime *time.Time
	var nextPoll time.Time
	var interval int
	err = tx.QueryRow(ctx, `SELECT client_id,scopes,expires_at,status,COALESCE(subject,''),auth_time,next_poll_at,poll_interval_seconds
	 FROM device_authorizations WHERE device_code_hash=$1 AND client_id=$2 FOR UPDATE`, Hash(deviceCode), clientID).
		Scan(&state.ClientID, &scopes, &state.Expires, &status, &subject, &authTime, &nextPoll, &interval)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if err = json.Unmarshal(scopes, &state.Scopes); err != nil {
		return nil, err
	}
	if time.Now().After(state.Expires) {
		state.Expires = time.Now().Add(-time.Second)
		return &state, tx.Commit(ctx)
	}
	if status == "denied" {
		state.Denied = true
		return &state, tx.Commit(ctx)
	}
	if status == "consumed" {
		return nil, ErrUnauthorized
	}
	now := time.Now()
	if now.Before(nextPoll) {
		interval = min(interval+5, 30)
		_, err = tx.Exec(ctx, `UPDATE device_authorizations SET poll_interval_seconds=$2::integer,next_poll_at=now()+$2::integer*interval '1 second' WHERE device_code_hash=$1`, Hash(deviceCode), interval)
		if err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, context.DeadlineExceeded
	}
	if status == "approved" {
		var active bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identities WHERE id=$1 AND status='active')`, subject).Scan(&active)
		if err != nil || !active {
			return nil, ErrUnauthorized
		}
		_, err = tx.Exec(ctx, `UPDATE device_authorizations SET status='consumed' WHERE device_code_hash=$1`, Hash(deviceCode))
		if err != nil {
			return nil, err
		}
		state.Done, state.Subject = true, subject
		state.AMR = []string{"pwd"}
		if authTime != nil {
			state.AuthTime = *authTime
		}
	} else {
		_, err = tx.Exec(ctx, `UPDATE device_authorizations SET next_poll_at=now()+poll_interval_seconds*interval '1 second' WHERE device_code_hash=$1`, Hash(deviceCode))
		if err != nil {
			return nil, err
		}
	}
	return &state, tx.Commit(ctx)
}

type DeviceRequest struct {
	ClientName string    `json:"client_name"`
	Scopes     []string  `json:"scopes"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func (s *Store) DeviceRequest(ctx context.Context, userCode string) (*DeviceRequest, error) {
	userCode = strings.ToUpper(strings.TrimSpace(userCode))
	var request DeviceRequest
	var scopes []byte
	err := s.DB.QueryRow(ctx, `SELECT a.name,d.scopes,d.expires_at FROM device_authorizations d JOIN applications a ON a.id=d.client_id
	 WHERE d.user_code=$1 AND d.status='pending' AND d.expires_at>now() AND a.active`, userCode).
		Scan(&request.ClientName, &scopes, &request.ExpiresAt)
	if err != nil {
		return nil, ErrExpired
	}
	if err = json.Unmarshal(scopes, &request.Scopes); err != nil {
		return nil, err
	}
	return &request, nil
}

func (s *Store) ConfirmDevice(ctx context.Context, userCode, subject string, approve bool) error {
	userCode = strings.ToUpper(strings.TrimSpace(userCode))
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id, clientID string
	err = tx.QueryRow(ctx, `SELECT d.device_code_hash,d.client_id FROM device_authorizations d JOIN applications a ON a.id=d.client_id
	 WHERE d.user_code=$1 AND d.status='pending' AND d.expires_at>now() AND a.active FOR UPDATE OF d`, userCode).Scan(&id, &clientID)
	if err != nil {
		return ErrExpired
	}
	var active bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identities WHERE id=$1 AND status='active')`, subject).Scan(&active)
	if err != nil || !active {
		return ErrUnauthorized
	}
	status := "denied"
	if approve {
		status = "approved"
	}
	_, err = tx.Exec(ctx, `UPDATE device_authorizations SET status=$2,subject=$3,auth_time=now() WHERE device_code_hash=$1`, id, status, subject)
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, subject, "device."+status, clientID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
