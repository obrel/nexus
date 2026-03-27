package domain

import "context"

// Context key types for request scoping.
type ctxKey string

const (
	// CtxKeyAppID is the context key for the tenant/application ID.
	CtxKeyAppID ctxKey = "app_id"
	// CtxKeyUserID is the context key for the authenticated user ID.
	CtxKeyUserID ctxKey = "user_id"
)

// WithAppID returns a new context with app_id set.
func WithAppID(ctx context.Context, appID string) context.Context {
	return context.WithValue(ctx, CtxKeyAppID, appID)
}

// AppIDFromContext extracts app_id from context.
func AppIDFromContext(ctx context.Context) string {
	v, ok := ctx.Value(CtxKeyAppID).(string)
	if !ok {
		return ""
	}
	return v
}

// WithUserID returns a new context with user_id set.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, CtxKeyUserID, userID)
}

// UserIDFromContext extracts user_id from context.
func UserIDFromContext(ctx context.Context) string {
	v, ok := ctx.Value(CtxKeyUserID).(string)
	if !ok {
		return ""
	}
	return v
}
