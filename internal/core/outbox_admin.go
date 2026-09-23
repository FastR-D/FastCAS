package core

import (
	"context"
	"time"
)

// OutboxStatus intentionally excludes event payloads and signed tokens.
type OutboxStatus struct {
	ID          string     `json:"id"`
	ClientID    string     `json:"client_id"`
	Kind        string     `json:"kind"`
	Attempts    int        `json:"attempts"`
	LastStatus  *int       `json:"last_status"`
	CreatedAt   time.Time  `json:"created_at"`
	AvailableAt time.Time  `json:"available_at"`
	DeadAt      *time.Time `json:"dead_at"`
	DeliveredAt *time.Time `json:"delivered_at"`
}

// OutboxAttempt contains only delivery metadata, never payloads or token material.
type OutboxAttempt struct {
	ID         int64     `json:"id"`
	Attempt    int       `json:"attempt"`
	Outcome    string    `json:"outcome"`
	HTTPStatus *int      `json:"http_status"`
	DurationMS int       `json:"duration_ms"`
	FinishedAt time.Time `json:"finished_at"`
}

func (s *Store) OutboxAttempts(ctx context.Context, id string) ([]OutboxAttempt, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,attempt,outcome,http_status,duration_ms,finished_at FROM outbox_attempts WHERE event_id=$1 ORDER BY id DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []OutboxAttempt{}
	for rows.Next() {
		var attempt OutboxAttempt
		if err = rows.Scan(&attempt.ID, &attempt.Attempt, &attempt.Outcome, &attempt.HTTPStatus, &attempt.DurationMS, &attempt.FinishedAt); err != nil {
			return nil, err
		}
		result = append(result, attempt)
	}
	return result, rows.Err()
}

func (s *Store) OutboxStatuses(ctx context.Context, before string, deadOnly bool) ([]OutboxStatus, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,client_id,kind,attempts,last_status,created_at,available_at,dead_at,delivered_at FROM outbox WHERE ($1='' OR id<$1) AND (NOT $2 OR dead_at IS NOT NULL) ORDER BY id DESC LIMIT 100`, before, deadOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []OutboxStatus{}
	for rows.Next() {
		var e OutboxStatus
		if err = rows.Scan(&e.ID, &e.ClientID, &e.Kind, &e.Attempts, &e.LastStatus, &e.CreatedAt, &e.AvailableAt, &e.DeadAt, &e.DeliveredAt); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// RetryDeadEvent retains the original id/payload for receiver deduplication.
// It never steals a live worker lease or requeues a delivered event.
func (s *Store) RetryDeadEvent(ctx context.Context, actor, id string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE outbox SET dead_at=NULL,attempts=0,last_status=NULL,available_at=now(),lease_until=NULL,lease_token=NULL WHERE id=$1 AND dead_at IS NOT NULL AND delivered_at IS NULL AND (lease_until IS NULL OR lease_until<now())`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(actor,action,target) VALUES($1,'outbox.retry',$2)`, actor, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
