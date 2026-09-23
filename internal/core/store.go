package core

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

var (
	ErrUnauthorized = errors.New("authentication required")
	ErrConflict     = errors.New("account or binding conflict")
	ErrExpired      = errors.New("transaction expired or already consumed")
	ErrForbidden    = errors.New("operation not permitted")
)

type Store struct {
	DB   *pgxpool.Pool
	Keys *KeyRing
	Dev  bool
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("database unavailable")
	}
	return &Store{DB: pool}, nil
}
func (s *Store) Close()                           { s.DB.Close() }
func (s *Store) Health(ctx context.Context) error { return s.DB.Ping(ctx) }

func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(89004321)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	for _, file := range files {
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			return err
		}
		var checksum string
		err = tx.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=$1`, file.Name()).Scan(&checksum)
		if err == nil {
			if checksum != Hash(string(body)) {
				return fmt.Errorf("migration checksum changed: %s", file.Name())
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err = tx.Exec(ctx, string(body)); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)`, file.Name(), Hash(string(body))); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func marshal(value any) ([]byte, error) { return json.Marshal(value) }
func classify(err error) error {
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23505" {
		return ErrConflict
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorized
	}
	return err
}

type Identity struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Role          string `json:"role"`
	Status        string `json:"status"`
	EmailVerified bool   `json:"email_verified"`
}

func validEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 254 {
		return "", errors.New("invalid email")
	}
	return email, nil
}

func (s *Store) CreateIdentity(ctx context.Context, email, name, password, role string) (*Identity, error) {
	email, err := validEmail(email)
	if err != nil {
		return nil, err
	}
	if role != "member" && role != "admin" {
		return nil, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return nil, errors.New("name required (maximum 200 bytes)")
	}
	encoded, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	user := &Identity{ID: RandomToken(), Email: email, Name: name, Role: role, Status: "active"}
	_, err = s.DB.Exec(ctx, `INSERT INTO identities(id,email,name,password_hash,role) VALUES($1,$2,$3,$4,$5)`, user.ID, email, name, encoded, role)
	return user, classify(err)
}

func (s *Store) Identity(ctx context.Context, id string) (*Identity, error) {
	var u Identity
	err := s.DB.QueryRow(ctx, `SELECT id,email,name,role,status,email_verified FROM identities WHERE id=$1 AND status='active'`, id).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Status, &u.EmailVerified)
	return &u, classify(err)
}

// Allow uses database time and a single UPSERT, so limits also apply across replicas.
func (s *Store) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	var count int
	err := s.DB.QueryRow(ctx, `INSERT INTO rate_limits(key,count,expires_at) VALUES($1,1,now()+$2*interval '1 second')
 ON CONFLICT(key) DO UPDATE SET count=CASE WHEN rate_limits.expires_at<now() THEN 1 ELSE rate_limits.count+1 END,
 expires_at=CASE WHEN rate_limits.expires_at<now() THEN excluded.expires_at ELSE rate_limits.expires_at END RETURNING count`, Hash(key), window.Seconds()).Scan(&count)
	return count <= limit, err
}

func (s *Store) Authenticate(ctx context.Context, email, password, peer string) (*Identity, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, key := range []string{"login-account:" + email, "login-peer:" + peer} {
		allowed, err := s.Allow(ctx, key, 12, 15*time.Minute)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrUnauthorized
		}
	}
	var id, encoded string
	err := s.DB.QueryRow(ctx, `SELECT id,password_hash FROM identities WHERE email=$1 AND status='active'`, email).Scan(&id, &encoded)
	if err != nil {
		// Match the normal password verification work without persisting a dummy user.
		PasswordMatches(password, "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		return nil, ErrUnauthorized
	}
	if !PasswordMatches(password, encoded) {
		return nil, ErrUnauthorized
	}
	return s.Identity(ctx, id)
}

type BrowserSession struct {
	ID              string    `json:"id"`
	IdentityID      string    `json:"identity_id"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}

func (s *Store) NewSession(ctx context.Context, subject string) (token, csrf string, err error) {
	token, csrf = RandomToken(), RandomToken()
	var result pgconn.CommandTag
	result, err = s.DB.Exec(ctx, `INSERT INTO browser_sessions(id,token_hash,identity_id,csrf_hash,expires_at) SELECT $1,$2,id,$4,now()+interval '8 hours' FROM identities WHERE id=$3 AND status='active'`, RandomToken(), Hash(token), subject, Hash(csrf))
	if err == nil && result.RowsAffected() != 1 {
		err = ErrUnauthorized
	}
	return
}
func (s *Store) Session(ctx context.Context, token string) (*BrowserSession, error) {
	var session BrowserSession
	err := s.DB.QueryRow(ctx, `SELECT s.id,s.identity_id,s.authenticated_at,s.expires_at FROM browser_sessions s JOIN identities i ON i.id=s.identity_id WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>now() AND i.status='active'`, Hash(token)).Scan(&session.ID, &session.IdentityID, &session.AuthenticatedAt, &session.ExpiresAt)
	return &session, classify(err)
}
func (s *Store) CheckCSRF(ctx context.Context, token, csrf string) error {
	var expected string
	err := s.DB.QueryRow(ctx, `SELECT csrf_hash FROM browser_sessions WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>now()`, Hash(token)).Scan(&expected)
	if err != nil || !equalHash(csrf, expected) {
		return ErrForbidden
	}
	return nil
}

func audit(ctx context.Context, tx pgx.Tx, actor, action, target string) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(actor,action,target) VALUES($1,$2,$3)`, actor, action, target)
	return err
}
