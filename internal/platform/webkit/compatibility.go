package webkit

import "net/http"

const (
	APIContractHeader        = "LiteAIG-API-Version"
	CurrentAPIContract       = "2026-09-09"
	DeprecatedAPIContract    = "2026-09-01"
	DeprecatedAPIContractEnd = "Wed, 09 Dec 2026 00:00:00 GMT"
)

// APICompatibility pins the public HTTP contract. Empty requests use the
// current contract; deprecated requests are served with standard deprecation
// headers; unknown future/past versions fail before reaching business logic.
func APICompatibility() Middleware {
	return func(next Handler) Handler {
		return func(c *Context) error {
			requested := c.Request().Header.Get(APIContractHeader)
			c.Header().Set(APIContractHeader, CurrentAPIContract)
			switch requested {
			case "", CurrentAPIContract:
				return next(c)
			case DeprecatedAPIContract:
				c.Header().Set("Deprecation", "true")
				c.Header().Set("Sunset", DeprecatedAPIContractEnd)
				return next(c)
			default:
				return NewAPIError(http.StatusBadRequest, "UNSUPPORTED_API_VERSION", map[string]any{
					"currentVersion":     CurrentAPIContract,
					"supportedVersions":  []string{CurrentAPIContract, DeprecatedAPIContract},
					"deprecatedVersions": []string{DeprecatedAPIContract},
				})
			}
		}
	}
}
