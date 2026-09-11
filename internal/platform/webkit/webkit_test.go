package webkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func doRequest(t *testing.T, h http.Handler, method, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON body %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestRoutingParamsAndMethods(t *testing.T) {
	e := New()
	e.Handle("GET /items/{id}", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"id": c.Param("id")})
	})
	e.Handle("POST /items", func(c *Context) error {
		return c.Text(http.StatusNoContent, "created")
	})

	rec := doRequest(t, e, http.MethodGet, "/items/42", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %q", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["id"] != "42" {
		t.Fatalf("id = %v, want 42", body["id"])
	}

	// /items exists for POST (and /items/{id} for GET), so bare GET /items
	// is a method-not-allowed, not a 404 — standard ServeMux semantics.
	rec = doRequest(t, e, http.MethodGet, "/items", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}

	rec = doRequest(t, e, http.MethodGet, "/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSpecificityMatch(t *testing.T) {
	e := New()
	e.Handle("GET /alerts", func(c *Context) error { return c.Text(200, "list") })
	e.Handle("GET /alerts/rules", func(c *Context) error { return c.Text(200, "rules") })

	if rec := doRequest(t, e, http.MethodGet, "/alerts/rules", ""); rec.Code != 200 || rec.Body.String() != "rules" {
		t.Fatalf("got %d %q, want rules route", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, e, http.MethodGet, "/alerts", ""); rec.Body.String() != "list" {
		t.Fatalf("got %q, want list route", rec.Body.String())
	}
}

func TestMiddlewareOrdering(t *testing.T) {
	var order []string
	record := func(step string) Middleware {
		return func(next Handler) Handler {
			return func(c *Context) error {
				order = append(order, step)
				err := next(c)
				order = append(order, step+"-out")
				return err
			}
		}
	}
	e := New()
	e.Use(record("global"))
	group := e.Group("/api", record("group"))
	group.Handle("GET /x", func(c *Context) error {
		order = append(order, "handler")
		return c.Text(200, "ok")
	}, record("route"))

	doRequest(t, e, http.MethodGet, "/api/x", "")
	want := []string{"global", "group", "route", "handler", "route-out", "group-out", "global-out"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestMiddlewareShortCircuit(t *testing.T) {
	reached := false
	e := New()
	e.Use(func(next Handler) Handler {
		return func(c *Context) error {
			return NewAPIError(http.StatusUnauthorized, "UNAUTHORIZED", nil)
		}
	})
	e.Handle("GET /x", func(c *Context) error {
		reached = true
		return c.Text(200, "ok")
	})

	rec := doRequest(t, e, http.MethodGet, "/x", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if reached {
		t.Fatal("handler ran despite middleware short-circuit")
	}
	body := decodeBody(t, rec)
	inner, ok := body["error"].(map[string]any)
	if !ok || inner["code"] != "UNAUTHORIZED" {
		t.Fatalf("body = %v, want error.code UNAUTHORIZED", body)
	}
}

func TestGroupMiddlewareScope(t *testing.T) {
	var groupHits int
	e := New()
	g := e.Group("/admin", func(next Handler) Handler {
		return func(c *Context) error {
			groupHits++
			return next(c)
		}
	})
	g.Handle("GET /a", func(c *Context) error { return c.Text(200, "a") })
	e.Handle("GET /public", func(c *Context) error { return c.Text(200, "p") })

	doRequest(t, e, http.MethodGet, "/admin/a", "")
	doRequest(t, e, http.MethodGet, "/public", "")
	if groupHits != 1 {
		t.Fatalf("group middleware ran %d times, want 1", groupHits)
	}
}

func TestAPIErrorRenderingWithParams(t *testing.T) {
	e := New()
	e.Handle("GET /x", func(c *Context) error {
		return NewAPIError(http.StatusUnprocessableEntity, "CONFIG_INVALID", map[string]any{"diagnostics": []any{"bad rule"}})
	})
	rec := doRequest(t, e, http.MethodGet, "/x", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	body := decodeBody(t, rec)
	inner, ok := body["error"].(map[string]any)
	if !ok || inner["code"] != "CONFIG_INVALID" {
		t.Fatalf("body = %v, want error.code CONFIG_INVALID", body)
	}
	params, ok := inner["params"].(map[string]any)
	if !ok || params["diagnostics"] == nil {
		t.Fatalf("params = %v, want diagnostics", inner["params"])
	}
}

func TestCustomErrorHandler(t *testing.T) {
	e := New()
	called := false
	e.ErrorHandler(func(c *Context, err error) {
		called = true
		var api *APIError
		if errors.As(err, &api) {
			_ = c.JSON(api.Status, map[string]string{"code": api.Code})
			return
		}
		_ = c.JSON(http.StatusInternalServerError, map[string]string{"code": "BOOM"})
	})
	e.Handle("GET /a", func(c *Context) error { return NewAPIError(404, "NOT_FOUND", nil) })
	e.Handle("GET /b", func(c *Context) error { return errors.New("mystery") })

	rec := doRequest(t, e, http.MethodGet, "/a", "")
	if !called || rec.Code != 404 {
		t.Fatalf("a: called=%v code=%d", called, rec.Code)
	}
	if body := decodeBody(t, rec); body["code"] != "NOT_FOUND" {
		t.Fatalf("a body = %v", body)
	}
	rec = doRequest(t, e, http.MethodGet, "/b", "")
	if rec.Code != 500 || decodeBody(t, rec)["code"] != "BOOM" {
		t.Fatalf("b: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestUnwrappedErrorIs500(t *testing.T) {
	e := New()
	e.Handle("GET /x", func(c *Context) error { return errors.New("boom") })
	rec := doRequest(t, e, http.MethodGet, "/x", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := decodeBody(t, rec)
	inner := body["error"].(map[string]any)
	if inner["code"] != "INTERNAL_ERROR" {
		t.Fatalf("body = %v", body)
	}
}

func TestBindJSON(t *testing.T) {
	e := New()
	e.Handle("POST /x", func(c *Context) error {
		var in struct {
			Name string `json:"name"`
		}
		if err := c.Bind(&in, 1<<20); err != nil {
			return NewAPIError(400, "INVALID_REQUEST", nil)
		}
		return c.JSON(200, map[string]string{"name": in.Name})
	})

	rec := doRequest(t, e, http.MethodPost, "/x", `{"name":"ops"}`)
	if rec.Code != 200 || decodeBody(t, rec)["name"] != "ops" {
		t.Fatalf("valid body: code=%d body=%q", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, e, http.MethodPost, "/x", `{not json`)
	if rec.Code != 400 {
		t.Fatalf("malformed body: code=%d, want 400", rec.Code)
	}

	big := strings.Repeat("a", 1<<20+1)
	rec = doRequest(t, e, http.MethodPost, "/x", `{"name":"`+big+`"}`)
	if rec.Code != 400 {
		t.Fatalf("oversized body: code=%d, want 400", rec.Code)
	}
}

func TestPostWriteErrorNotDoubleWritten(t *testing.T) {
	e := New()
	e.Handle("GET /x", func(c *Context) error {
		c.Status(200)
		if _, err := io.WriteString(c.Response(), "partial"); err != nil {
			return err
		}
		return errors.New("late failure")
	})
	rec := doRequest(t, e, http.MethodGet, "/x", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "partial" {
		t.Fatalf("body = %q, want only the original bytes", got)
	}
}

func TestRecoverPanicBeforeWrite(t *testing.T) {
	e := New()
	e.Use(Recover())
	e.Handle("GET /x", func(c *Context) error {
		panic("kaboom")
	})
	rec := doRequest(t, e, http.MethodGet, "/x", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := decodeBody(t, rec)
	inner := body["error"].(map[string]any)
	if inner["code"] != "INTERNAL_ERROR" {
		t.Fatalf("body = %v", body)
	}
}

func TestSecurityHeadersAndOverrides(t *testing.T) {
	const csp = "default-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'"
	e := New()
	e.Use(SecurityHeaders([2]string{"Content-Security-Policy", csp}))
	e.Handle("GET /json", func(c *Context) error { return c.JSON(200, map[string]string{"ok": "1"}) })
	e.Handle("GET /sse", func(c *Context) error {
		c.Header().Set("Content-Type", "text/event-stream")
		c.Header().Set("Cache-Control", "no-cache")
		c.Status(200)
		_, err := io.WriteString(c.Response(), "retry: 3000\n\n")
		return err
	})

	rec := doRequest(t, e, http.MethodGet, "/json", "")
	h := rec.Header()
	if h.Get("Content-Security-Policy") != csp {
		t.Fatalf("CSP = %q", h.Get("Content-Security-Policy"))
	}
	if h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Referrer-Policy") != "no-referrer" ||
		h.Get("Permissions-Policy") != "camera=(), microphone=(), geolocation=()" ||
		h.Get("Cross-Origin-Opener-Policy") != "same-origin" ||
		h.Get("Cache-Control") != "no-store" {
		t.Fatalf("baseline headers missing: %v", h)
	}
	if h.Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", h.Get("Content-Type"))
	}

	rec = doRequest(t, e, http.MethodGet, "/sse", "")
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("sse Cache-Control = %q, want handler override no-cache", rec.Header().Get("Cache-Control"))
	}
}

func TestAPICompatibilityHeadersAndVersionGate(t *testing.T) {
	reached := 0
	e := New().Use(APICompatibility())
	e.Handle("GET /x", func(c *Context) error {
		reached++
		return c.JSON(http.StatusOK, map[string]string{"ok": "1"})
	})

	rec := doRequest(t, e, http.MethodGet, "/x", "")
	if rec.Code != http.StatusOK || rec.Header().Get(APIContractHeader) != CurrentAPIContract {
		t.Fatalf("current response code=%d headers=%v", rec.Code, rec.Header())
	}

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(APIContractHeader, DeprecatedAPIContract)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Deprecation") != "true" || rec.Header().Get("Sunset") != DeprecatedAPIContractEnd {
		t.Fatalf("deprecated response code=%d headers=%v", rec.Code, rec.Header())
	}

	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(APIContractHeader, "1999-01-01")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "UNSUPPORTED_API_VERSION") {
		t.Fatalf("unsupported response code=%d body=%s", rec.Code, rec.Body.String())
	}
	if reached != 2 {
		t.Fatalf("handler reached %d times, want 2", reached)
	}
}

func TestHandleHTTPMountsForeignHandler(t *testing.T) {
	foreign := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Foreign", "yes")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("foreign body"))
	})
	e := New()
	e.Use(func(next Handler) Handler {
		return func(c *Context) error {
			c.Header().Set("X-Middleware", "ran")
			return next(c)
		}
	})
	e.HandleHTTP("/foreign/", foreign)

	rec := doRequest(t, e, http.MethodGet, "/foreign/x", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if rec.Body.String() != "foreign body" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if rec.Header().Get("X-Foreign") != "yes" || rec.Header().Get("X-Middleware") != "ran" {
		t.Fatalf("headers = %v", rec.Header())
	}
}

func TestContextValues(t *testing.T) {
	e := New()
	e.Use(func(next Handler) Handler {
		return func(c *Context) error {
			c.Set("session", "admin-1")
			return next(c)
		}
	})
	e.Handle("GET /x", func(c *Context) error {
		value, ok := c.Get("session")
		if !ok || value != "admin-1" {
			return fmt.Errorf("session value = %v, %v", value, ok)
		}
		return c.Text(200, "ok")
	})
	if rec := doRequest(t, e, http.MethodGet, "/x", ""); rec.Code != 200 {
		t.Fatalf("code = %d body = %q", rec.Code, rec.Body.String())
	}
}
