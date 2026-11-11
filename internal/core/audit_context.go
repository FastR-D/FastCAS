package core

import "context"

type auditRequestKey struct{}

// WithAuditRequestID correlates committed security operations with a server-
// generated request ID. CLI and background operations may leave the ID empty.
func WithAuditRequestID(ctx context.Context, id string) context.Context {
	if len(id) > 128 {
		id = ""
	}
	return context.WithValue(ctx, auditRequestKey{}, id)
}
func auditRequestID(ctx context.Context) string {
	id, _ := ctx.Value(auditRequestKey{}).(string)
	return id
}
