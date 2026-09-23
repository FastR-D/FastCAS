package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/jackc/pgx/v5"
)

// DeliverEvent claims one durable event. Lease fencing prevents a slow worker
// from overwriting a newer worker's result. Receivers must deduplicate by jti.
func (s *Store) DeliverEvent(ctx context.Context, issuer string) (bool, error) {
	if s.Keys == nil {
		return false, errors.New("event signing key unavailable")
	}
	var id, client, lease, kind string
	var payload json.RawMessage
	var attempts int
	lease = RandomToken()
	err := s.DB.QueryRow(ctx, `WITH candidate AS (
 SELECT id FROM outbox WHERE delivered_at IS NULL AND dead_at IS NULL
 AND available_at<=now() AND (lease_until IS NULL OR lease_until<now())
 ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT 1
	 ) UPDATE outbox o SET lease_token=$1,lease_until=now()+interval '30 seconds',attempts=attempts+1
	 FROM candidate c WHERE o.id=c.id RETURNING o.id,o.client_id,o.payload,o.attempts,o.kind`, lease).Scan(&id, &client, &payload, &attempts, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	started := time.Now()
	status := 0
	outcome := "configuration_error"
	c, err := s.Client(ctx, client)
	endpoint, contentType := "", "application/jwt"
	if err == nil {
		if kind == "logout" {
			endpoint, contentType = c.BackchannelURL, "application/x-www-form-urlencoded"
		} else if kind == "event" {
			endpoint = c.EventsURL
		}
	}
	if err == nil && endpoint != "" {
		outcome = "signing_error"
		var signer jose.Signer
		tokenType := "fastcas-event+jwt"
		if kind == "logout" {
			tokenType = "logout+jwt"
		}
		signer, err = jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: s.Keys.Private}, (&jose.SignerOptions{}).WithType(jose.ContentType(tokenType)).WithHeader("kid", s.Keys.KeyID))
		if err == nil {
			now := time.Now()
			var token string
			claims := jwt.Claims{Issuer: issuer, Audience: jwt.Audience{client}, ID: id, IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(5 * time.Minute))}
			extra := map[string]any{"event": payload}
			if kind == "logout" {
				var logout struct {
					Subject string `json:"subject"`
					SID     string `json:"sid"`
				}
				err = json.Unmarshal(payload, &logout)
				if err == nil && logout.Subject != "" {
					claims.Subject = logout.Subject
					extra = map[string]any{"events": map[string]any{"http://schemas.openid.net/event/backchannel-logout": map[string]any{}}}
					if logout.SID != "" {
						extra["sid"] = logout.SID
					}
				} else if err == nil {
					err = errors.New("logout subject missing")
				}
			}
			if err == nil {
				token, err = jwt.Signed(signer).Claims(claims).Claims(extra).Serialize()
			}
			if err == nil {
				outcome = "transport_error"
				var req *http.Request
				body := bytes.NewBufferString(token)
				if kind == "logout" {
					body = bytes.NewBufferString(url.Values{"logout_token": {token}}.Encode())
				}
				req, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
				if err == nil {
					req.Header.Set("Content-Type", contentType)
					// Never forward signed account information to a redirect target.
					transport := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
					var response *http.Response
					response, err = transport.Do(req)
					if err == nil {
						status = response.StatusCode
						outcome = "http_error"
						if status >= 200 && status < 300 {
							outcome = "delivered"
						}
						_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
						response.Body.Close()
					}
				}
			}
		}
	}
	// Delivery errors are persisted as retry state, without sensitive response bodies.
	delay := time.Second * time.Duration(1<<min(attempts, 12))
	var httpStatus any
	if status != 0 {
		httpStatus = status
	}
	_, err = s.DB.Exec(ctx, `WITH completed AS (UPDATE outbox SET lease_until=NULL,lease_token=NULL,last_status=$3,
 delivered_at=CASE WHEN $4 THEN now() ELSE NULL END,
 dead_at=CASE WHEN NOT $4 AND attempts>=12 THEN now() ELSE NULL END,
 available_at=now()+$5::interval WHERE id=$1 AND lease_token=$2 RETURNING id)
 INSERT INTO outbox_attempts(event_id,attempt,outcome,http_status,duration_ms)
 SELECT id,$6,$7,$8,$9 FROM completed`, id, lease, status, status >= 200 && status < 300, delay.String(), attempts, outcome, httpStatus, max(0, time.Since(started).Milliseconds()))
	return true, err
}
