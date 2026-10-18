package tenancy

import "context"

type tenantContextKey struct{}

// WithTenant attaches the tenant scope to a context so storage adapters can
// select a tenant-scoped Postgres role/pool. It is not an authorization
// decision; repositories still validate TenantScope arguments.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		return ctx
	}
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

// TenantFromContext returns the tenant attached by WithTenant, if any.
func TenantFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(tenantContextKey{}).(string)
	return value
}
