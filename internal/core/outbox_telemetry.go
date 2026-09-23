package core

import (
	"context"
	"time"
)

// OutboxTelemetry contains only aggregate delivery health, with no event IDs,
// recipients, payloads or signed tokens.
type OutboxTelemetry struct {
	Pending                 int64   `json:"pending"`
	Due                     int64   `json:"due"`
	Dead                    int64   `json:"dead"`
	OldestPendingAgeSeconds float64 `json:"oldest_pending_age_seconds"`
	OldestDueAgeSeconds     float64 `json:"oldest_due_age_seconds"`
	RecentDelivered         int64   `json:"recent_delivered"`
	P95DeliverySeconds      float64 `json:"p95_delivery_seconds"`
}

func (s *Store) OutboxTelemetry(ctx context.Context, window time.Duration) (OutboxTelemetry, error) {
	var result OutboxTelemetry
	err := s.DB.QueryRow(ctx, `SELECT
 count(*) FILTER (WHERE delivered_at IS NULL AND dead_at IS NULL),
 count(*) FILTER (WHERE delivered_at IS NULL AND dead_at IS NULL AND available_at<=now()),
 count(*) FILTER (WHERE dead_at IS NOT NULL AND delivered_at IS NULL),
 COALESCE(max(EXTRACT(EPOCH FROM now()-created_at)::double precision) FILTER (WHERE delivered_at IS NULL AND dead_at IS NULL),0),
 COALESCE(max(EXTRACT(EPOCH FROM now()-available_at)::double precision) FILTER (WHERE delivered_at IS NULL AND dead_at IS NULL AND available_at<=now()),0),
 count(*) FILTER (WHERE delivered_at>=now()-$1::double precision*interval '1 second'),
 COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM delivered_at-created_at)::double precision) FILTER (WHERE delivered_at>=now()-$1::double precision*interval '1 second'),0)
 FROM outbox`, window.Seconds()).Scan(&result.Pending, &result.Due, &result.Dead, &result.OldestPendingAgeSeconds, &result.OldestDueAgeSeconds, &result.RecentDelivered, &result.P95DeliverySeconds)
	return result, err
}
