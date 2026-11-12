package core

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrGitHubNotLinked = errors.New("GitHub account is not linked")

type GitHubFlow struct {
	Purpose, Verifier, SessionID, Subject, AuthRequestID, DeviceCode string
	SubjectVersion                                                   int64
}

func (s *Store) NewGitHubFlow(ctx context.Context, flow GitHubFlow) (state, browser string, err error) {
	state, browser = RandomToken(), RandomToken()
	_, err = s.DB.Exec(ctx, `INSERT INTO github_flows(state_hash,browser_hash,purpose,verifier,session_id,subject,subject_version,auth_request_id,device_code) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, Hash(state), Hash(browser), flow.Purpose, flow.Verifier, flow.SessionID, flow.Subject, flow.SubjectVersion, flow.AuthRequestID, flow.DeviceCode)
	return
}

// Both independently random secrets are required. DELETE makes callback replay
// impossible across replicas; a mismatched browser cannot consume a valid flow.
func (s *Store) TakeGitHubFlow(ctx context.Context, state, browser string, mfa bool) (*GitHubFlow, error) {
	var f GitHubFlow
	err := s.DB.QueryRow(ctx, `DELETE FROM github_flows WHERE state_hash=$1 AND browser_hash=$2 AND expires_at>now() AND (purpose='mfa')=$3 RETURNING purpose,verifier,session_id,subject,subject_version,auth_request_id,device_code`, Hash(state), Hash(browser), mfa).Scan(&f.Purpose, &f.Verifier, &f.SessionID, &f.Subject, &f.SubjectVersion, &f.AuthRequestID, &f.DeviceCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrExpired
	}
	return &f, err
}

func (s *Store) GitHubIdentity(ctx context.Context, githubID int64) (*Identity, error) {
	var subject string
	err := s.DB.QueryRow(ctx, `SELECT identity_id FROM github_accounts WHERE github_id=$1`, githubID).Scan(&subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrGitHubNotLinked
	}
	if err != nil {
		return nil, err
	}
	return s.Identity(ctx, subject)
}

// RequireRecentAccountAuth protects adding an alternative login credential.
func (s *Store) RequireRecentAccountAuth(ctx context.Context, subject, sessionID string) error {
	var allowed bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identities i JOIN browser_sessions b ON b.identity_id=i.id
 WHERE i.id=$1 AND b.id=$2 AND i.status='active' AND i.deleted_at IS NULL
 AND b.revoked_at IS NULL AND b.expires_at>now() AND b.authenticated_at>now()-interval '5 minutes'
 AND (i.mfa_secret IS NULL OR b.mfa_at>now()-interval '5 minutes'))`, subject, sessionID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrUnauthorized
	}
	return nil
}

func (s *Store) BindGitHub(ctx context.Context, subject, sessionID string, githubID int64, login string) error {
	if githubID <= 0 || login == "" || len(login) > 100 {
		return ErrForbidden
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Lock the account and session through commit, including against concurrent
	// deletion, MFA enrollment, password changes, logout and disable operations.
	var id string
	err = tx.QueryRow(ctx, `SELECT i.id FROM identities i JOIN browser_sessions b ON b.identity_id=i.id
 WHERE i.id=$1 AND b.id=$2 AND i.status='active' AND i.deleted_at IS NULL
 AND b.revoked_at IS NULL AND b.expires_at>now() AND b.authenticated_at>now()-interval '5 minutes'
 AND (i.mfa_secret IS NULL OR b.mfa_at>now()-interval '5 minutes') FOR UPDATE OF i,b`, subject, sessionID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO github_accounts(github_id,identity_id,login) VALUES($1,$2,$3)`, githubID, subject, login)
	if err != nil {
		return classify(err)
	}
	if err = audit(ctx, tx, subject, "github.bind", subject); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Registration never merges an email match. The caller must obtain a verified
// primary email and, where configured, verify active organization membership.
// New production GitHub accounts are administrators; development accounts are members.
func (s *Store) RegisterGitHub(ctx context.Context, githubID int64, login, email, name string) (*Identity, error) {
	email, err := validEmail(email)
	if err != nil {
		return nil, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = login
	}
	if githubID <= 0 || login == "" || len(login) > 100 || name == "" || len(name) > 200 {
		return nil, ErrForbidden
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	role := "member"
	if !s.Dev {
		role = "admin"
	}
	u := &Identity{ID: RandomToken(), Email: email, Name: name, Role: role, Status: "active", EmailVerified: true}
	_, err = tx.Exec(ctx, `INSERT INTO identities(id,email,name,password_hash,email_verified,role) VALUES($1,$2,$3,'',true,$4)`, u.ID, u.Email, u.Name, u.Role)
	if err != nil {
		return nil, classify(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO github_accounts(github_id,identity_id,login) VALUES($1,$2,$3)`, githubID, u.ID, login)
	if err != nil {
		return nil, classify(err)
	}
	if err = audit(ctx, tx, u.ID, "github.register", u.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return u, nil
}

type GitHubAccount struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

func (s *Store) GitHubAccount(ctx context.Context, subject string) (*GitHubAccount, error) {
	var a GitHubAccount
	err := s.DB.QueryRow(ctx, `SELECT github_id,login FROM github_accounts WHERE identity_id=$1`, subject).Scan(&a.ID, &a.Login)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &a, err
}

// GitHubLoginState binds the upstream proof to the current local credentials.
func (s *Store) GitHubLoginState(ctx context.Context, subject string) (enabled bool, version int64, err error) {
	err = s.DB.QueryRow(ctx, `SELECT mfa_secret IS NOT NULL,authentication_version FROM identities WHERE id=$1 AND status='active' AND deleted_at IS NULL`, subject).Scan(&enabled, &version)
	err = classify(err)
	return
}
func (s *Store) NewGitHubSession(ctx context.Context, subject string, version int64, mfa bool) (token, csrf string, err error) {
	token, csrf = RandomToken(), RandomToken()
	// Hold a share lock until insertion commits so credential changes and account
	// disable operations cannot race the final proof check and session creation.
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM identities WHERE id=$1 AND status='active' AND deleted_at IS NULL
 AND authentication_version=$2 AND (mfa_secret IS NOT NULL)=$3
 AND EXISTS(SELECT 1 FROM github_accounts g WHERE g.identity_id=identities.id) FOR SHARE`, subject, version, mfa).Scan(&id)
	if err != nil {
		return "", "", classify(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO browser_sessions(id,token_hash,identity_id,csrf_hash,expires_at,mfa_at)
 VALUES($1,$2,$3,$4,now()+interval '8 hours',CASE WHEN $5 THEN now() ELSE NULL END)`, RandomToken(), Hash(token), subject, Hash(csrf), mfa)
	if err != nil {
		return "", "", err
	}
	err = tx.Commit(ctx)
	return
}
