package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/guardrail/benchmark"
	"github.com/F31/liteAIG/internal/tenancy"
)

func (b *fakeBackend) Evidence(context.Context, tenancy.TenantScope, time.Time, time.Time) (benchmark.Archive, error) {
	return benchmark.Archive{}, nil
}

type evidenceServiceFunc func(context.Context, tenancy.TenantScope, time.Time, time.Time) (benchmark.Archive, error)

func (f evidenceServiceFunc) Evidence(ctx context.Context, scope tenancy.TenantScope, from, to time.Time) (benchmark.Archive, error) {
	return f(ctx, scope, from, to)
}

func TestEvidenceEndpoint(t *testing.T) {
	const start = "2026-01-01T00:00:00Z"
	const end = "2026-01-02T00:00:00Z"
	for _, tc := range []struct {
		name, role, from, to string
		unauth, unavailable  bool
		status               int
		code                 string
	}{
		{name: "admin", role: "tenant_admin", status: 200},
		{name: "bounded", role: "tenant_admin", from: start, to: end, status: 200},
		{name: "from only", role: "tenant_admin", from: start, status: 200},
		{name: "to only", role: "tenant_admin", to: end, status: 200},
		{name: "zero from", role: "tenant_admin", from: "0001-01-01T00:00:00Z", to: end, status: 200},
		{name: "zero to", role: "tenant_admin", from: start, to: "0001-01-01T00:00:00Z", status: 200},
		{name: "operator", role: "operator", status: 403},
		{name: "viewer", role: "viewer", status: 403},
		{name: "unauthenticated", unauth: true, status: 401},
		{name: "invalid from", role: "tenant_admin", from: "yesterday", status: 400, code: "INVALID_TIME"},
		{name: "invalid to", role: "tenant_admin", to: "2026-01-02", status: 400, code: "INVALID_TIME"},
		{name: "reversed", role: "tenant_admin", from: end, to: start, status: 400, code: "INVALID_TIME_RANGE"},
		{name: "equal", role: "tenant_admin", from: start, to: start, status: 400, code: "INVALID_TIME_RANGE"},
		{name: "equal offset", role: "tenant_admin", from: start, to: "2026-01-01T01:00:00+01:00", status: 400, code: "INVALID_TIME_RANGE"},
		{name: "unavailable", role: "tenant_admin", unavailable: true, status: 503, code: "EVIDENCE_UNAVAILABLE"},
		{name: "unavailable operator", role: "operator", unavailable: true, status: 403},
		{name: "unavailable unauthenticated", unauth: true, unavailable: true, status: 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var svc EvidenceService = evidenceServiceFunc(func(_ context.Context, scope tenancy.TenantScope, from, to time.Time) (benchmark.Archive, error) {
				calls++
				wantFrom, _ := time.Parse(time.RFC3339, tc.from)
				wantTo, _ := time.Parse(time.RFC3339, tc.to)
				if scope.TenantID != "trusted" || !from.Equal(wantFrom) || !to.Equal(wantTo) {
					t.Fatalf("scope=%+v from=%v to=%v", scope, from, to)
				}
				return benchmark.NewExporter(nil).Export(scope.TenantID, from, to, nil), nil
			})
			if tc.unavailable {
				svc = nil
			}
			auth := authorizer{session: Session{AdminID: "admin", TenantID: "trusted", Role: tc.role}}
			if tc.unauth {
				auth.err = errors.New("no session")
			}
			server := New(Services{Evidence: svc}, auth)
			query := url.Values{"from": {tc.from}, "to": {tc.to}, "tenantId": {"attacker"}}
			request := httptest.NewRequest(http.MethodGet, "/api/admin/evidence?"+query.Encode(), nil)
			request.Header.Set("X-Tenant-ID", "attacker")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != tc.status || !json.Valid(response.Body.Bytes()) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
			if tc.code != "" && !strings.Contains(response.Body.String(), `"`+tc.code+`"`) {
				t.Fatalf("missing error code %s: %s", tc.code, response.Body)
			}
			if tc.status == 200 {
				var archive benchmark.Archive
				if err := json.Unmarshal(response.Body.Bytes(), &archive); err != nil {
					t.Fatal(err)
				}
				if calls != 1 || archive.TenantID != "trusted" || archive.ExportedAt.IsZero() || archive.Records == nil {
					t.Fatalf("calls=%d archive=%+v", calls, archive)
				}
			} else if calls != 0 {
				t.Fatalf("service called %d times on rejected request", calls)
			}
		})
	}
}
