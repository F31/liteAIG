package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/controlplane/rbac"
)

type usersStore struct {
	users []LocalUser
}

func (s *usersStore) FindLocalCredential(context.Context, string) (LocalCredential, error) {
	return LocalCredential{}, nil
}
func (s *usersStore) FindLocalUserByID(_ context.Context, id string) (LocalCredential, error) {
	for _, user := range s.users {
		if user.ID == id {
			return LocalCredential{AdminID: user.ID, Username: user.Username, Role: user.Role, Status: user.Status}, nil
		}
	}
	return LocalCredential{}, ErrLocalUserNotFound
}
func (s *usersStore) ResetLocalPassword(_ context.Context, username, _ string) error {
	for i := range s.users {
		if s.users[i].Username == username {
			return nil
		}
	}
	return ErrLocalUserNotFound
}
func (s *usersStore) ListLocalUsers(context.Context) ([]LocalUser, error) {
	result := make([]LocalUser, len(s.users))
	copy(result, s.users)
	return result, nil
}
func (s *usersStore) CreateLocalUser(_ context.Context, user LocalUser) (LocalUser, error) {
	for _, existing := range s.users {
		if existing.Username == user.Username {
			return LocalUser{}, ErrUsernameTaken
		}
	}
	user.Status = "active"
	user.CreatedAt = time.Unix(200, 0)
	user.PasswordHash = ""
	s.users = append(s.users, user)
	return user, nil
}
func (s *usersStore) SetUserRole(_ context.Context, id, role string) (LocalUser, error) {
	for i := range s.users {
		if s.users[i].ID == id {
			s.users[i].Role = role
			return s.users[i], nil
		}
	}
	return LocalUser{}, ErrLocalUserNotFound
}
func (s *usersStore) SetUserStatus(_ context.Context, id, status string) (LocalUser, error) {
	for i := range s.users {
		if s.users[i].ID == id {
			s.users[i].Status = status
			return s.users[i], nil
		}
	}
	return LocalUser{}, ErrLocalUserNotFound
}
func (s *usersStore) SetUserEmail(_ context.Context, id, email string) (LocalUser, error) {
	for i := range s.users {
		if s.users[i].ID == id {
			s.users[i].Email = email
			return s.users[i], nil
		}
	}
	return LocalUser{}, ErrLocalUserNotFound
}
func (s *usersStore) DeleteLocalUser(_ context.Context, id string) error {
	for i := range s.users {
		if s.users[i].ID == id {
			s.users = append(s.users[:i], s.users[i+1:]...)
			return nil
		}
	}
	return ErrLocalUserNotFound
}
func (s *usersStore) CountActiveAdmins(context.Context) (int, error) {
	count := 0
	for _, user := range s.users {
		if user.Role == rbac.RoleTenantAdmin && user.Status == "active" {
			count++
		}
	}
	return count, nil
}

type testIDGenerator struct{ counter int }

func (g *testIDGenerator) New() (string, error) {
	g.counter++
	return "gen-" + string(rune('0'+g.counter)), nil
}

type userPasswords struct{}

func (userPasswords) Hash(password []byte) (string, error) { return "hash:" + string(password), nil }
func (userPasswords) Verify([]byte, string) (bool, error)  { return true, nil }

func newUsersServer(t *testing.T, session Session) (*Server, *usersStore) {
	t.Helper()
	store := &usersStore{users: []LocalUser{
		{ID: "admin-1", Username: "admin", Role: "tenant_admin", Status: "active", CreatedAt: time.Unix(100, 0)},
		{ID: "viewer-1", Username: "reader", Role: "viewer", Status: "active", CreatedAt: time.Unix(101, 0)},
	}}
	server := New(AllOf(&fakeBackend{}), authorizer{session: session})
	server.WithUserManagement(store, userPasswords{}, &testIDGenerator{})
	return server, store
}

func doUsersRequest(t *testing.T, server *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	} else {
		reader = bytes.NewReader(nil)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(method, path, reader))
	return response
}

func decodeUsers(t *testing.T, body []byte) []UserView {
	t.Helper()
	var result []UserView
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode users: %v body=%s", err, body)
	}
	return result
}

func TestListUsersMarksSelf(t *testing.T) {
	server, _ := newUsersServer(t, Session{AdminID: "admin-1", TenantID: "t", Username: "admin", Role: rbac.RoleTenantAdmin})
	response := doUsersRequest(t, server, http.MethodGet, "/api/admin/users", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	users := decodeUsers(t, response.Body.Bytes())
	if len(users) != 2 {
		t.Fatalf("len(users)=%d", len(users))
	}
	for _, user := range users {
		want := user.ID == "admin-1"
		if user.IsSelf != want {
			t.Fatalf("user %s isSelf=%v want %v", user.ID, user.IsSelf, want)
		}
	}
}

func TestUsersEndpointsRejectNonAdmin(t *testing.T) {
	server, _ := newUsersServer(t, Session{AdminID: "viewer-1", TenantID: "t", Username: "reader", Role: rbac.RoleViewer})
	response := doUsersRequest(t, server, http.MethodGet, "/api/admin/users", "")
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "ROLE_FORBIDDEN") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCreateUserValidatesAndCreates(t *testing.T) {
	server, store := newUsersServer(t, Session{AdminID: "admin-1", TenantID: "t", Username: "admin", Role: rbac.RoleTenantAdmin})

	badRole := doUsersRequest(t, server, http.MethodPost, "/api/admin/users", `{"username":"bob","password":"long-enough","role":"superuser"}`)
	if badRole.Code != http.StatusBadRequest {
		t.Fatalf("bad role status=%d", badRole.Code)
	}
	systemRole := doUsersRequest(t, server, http.MethodPost, "/api/admin/users", `{"username":"root","password":"long-enough","role":"system_admin"}`)
	if systemRole.Code != http.StatusBadRequest {
		t.Fatalf("system role status=%d", systemRole.Code)
	}
	shortPassword := doUsersRequest(t, server, http.MethodPost, "/api/admin/users", `{"username":"bob","password":"short","role":"viewer"}`)
	if shortPassword.Code != http.StatusBadRequest {
		t.Fatalf("short password status=%d", shortPassword.Code)
	}
	duplicate := doUsersRequest(t, server, http.MethodPost, "/api/admin/users", `{"username":"admin","password":"long-enough","role":"viewer"}`)
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), "USERNAME_TAKEN") {
		t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}

	created := doUsersRequest(t, server, http.MethodPost, "/api/admin/users", `{"username":"bob","password":"long-enough","role":"tenant_operator"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var createdUser UserView
	if json.Unmarshal(created.Body.Bytes(), &createdUser) != nil || createdUser.Username != "bob" || createdUser.Role != "tenant_operator" {
		t.Fatalf("created=%+v body=%s", createdUser, created.Body.String())
	}
	if len(store.users) != 3 {
		t.Fatalf("store.users=%d", len(store.users))
	}
}

func TestUserRoleChangeProtectsSelfAndAllowsWithBackupAdmin(t *testing.T) {
	server, _ := newUsersServer(t, Session{AdminID: "admin-1", TenantID: "t", Username: "admin", Role: rbac.RoleTenantAdmin})

	self := doUsersRequest(t, server, http.MethodPatch, "/api/admin/users/admin-1/role", `{"role":"viewer"}`)
	if self.Code != http.StatusBadRequest {
		t.Fatalf("self role change status=%d", self.Code)
	}
	ok := doUsersRequest(t, server, http.MethodPatch, "/api/admin/users/viewer-1/role", `{"role":"tenant_operator"}`)
	if ok.Code != http.StatusOK {
		t.Fatalf("operator promotion status=%d body=%s", ok.Code, ok.Body.String())
	}
	systemRole := doUsersRequest(t, server, http.MethodPatch, "/api/admin/users/viewer-1/role", `{"role":"system_admin"}`)
	if systemRole.Code != http.StatusBadRequest {
		t.Fatalf("system role change status=%d body=%s", systemRole.Code, systemRole.Body.String())
	}
}

func TestUserStatusAndDeleteProtectSelf(t *testing.T) {
	server, store := newUsersServer(t, Session{AdminID: "admin-1", TenantID: "t", Username: "admin", Role: rbac.RoleTenantAdmin})

	selfDisable := doUsersRequest(t, server, http.MethodPost, "/api/admin/users/admin-1/status", `{"status":"disabled"}`)
	if selfDisable.Code != http.StatusBadRequest {
		t.Fatalf("self disable status=%d", selfDisable.Code)
	}
	selfDelete := doUsersRequest(t, server, http.MethodDelete, "/api/admin/users/admin-1", "")
	if selfDelete.Code != http.StatusBadRequest {
		t.Fatalf("self delete status=%d", selfDelete.Code)
	}
	deleted := doUsersRequest(t, server, http.MethodDelete, "/api/admin/users/viewer-1", "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete viewer status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	if len(store.users) != 1 {
		t.Fatalf("store.users=%d after delete", len(store.users))
	}
}

func TestLastActiveAdminCannotBeRemovedByOtherSession(t *testing.T) {
	// The actor's own account row is disabled (a stale in-memory session), so
	// the only active tenant_admin is the target: removal must be refused.
	store := &usersStore{users: []LocalUser{
		{ID: "actor", Username: "actor", Role: rbac.RoleTenantAdmin, Status: "disabled", CreatedAt: time.Unix(100, 0)},
		{ID: "last-admin", Username: "last", Role: rbac.RoleTenantAdmin, Status: "active", CreatedAt: time.Unix(101, 0)},
	}}
	server := New(AllOf(&fakeBackend{}), authorizer{session: Session{AdminID: "actor", TenantID: "t", Username: "actor", Role: rbac.RoleTenantAdmin}})
	server.WithUserManagement(store, userPasswords{}, &testIDGenerator{})

	disable := doUsersRequest(t, server, http.MethodPost, "/api/admin/users/last-admin/status", `{"status":"disabled"}`)
	if disable.Code != http.StatusConflict || !strings.Contains(disable.Body.String(), "LAST_ADMIN_PROTECTED") {
		t.Fatalf("disable last admin status=%d body=%s", disable.Code, disable.Body.String())
	}
	demote := doUsersRequest(t, server, http.MethodPatch, "/api/admin/users/last-admin/role", `{"role":"viewer"}`)
	if demote.Code != http.StatusConflict || !strings.Contains(demote.Body.String(), "LAST_ADMIN_PROTECTED") {
		t.Fatalf("demote last admin status=%d body=%s", demote.Code, demote.Body.String())
	}
	deleteLast := doUsersRequest(t, server, http.MethodDelete, "/api/admin/users/last-admin", "")
	if deleteLast.Code != http.StatusConflict || !strings.Contains(deleteLast.Body.String(), "LAST_ADMIN_PROTECTED") {
		t.Fatalf("delete last admin status=%d body=%s", deleteLast.Code, deleteLast.Body.String())
	}
	if len(store.users) != 2 {
		t.Fatalf("store.users=%d, last admin must remain", len(store.users))
	}
}

func TestResetUserPassword(t *testing.T) {
	server, _ := newUsersServer(t, Session{AdminID: "admin-1", TenantID: "t", Username: "admin", Role: rbac.RoleTenantAdmin})
	short := doUsersRequest(t, server, http.MethodPost, "/api/admin/users/viewer-1/reset-password", `{"password":"short"}`)
	if short.Code != http.StatusBadRequest {
		t.Fatalf("short reset status=%d", short.Code)
	}
	reset := doUsersRequest(t, server, http.MethodPost, "/api/admin/users/viewer-1/reset-password", `{"password":"new-password-1"}`)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset status=%d body=%s", reset.Code, reset.Body.String())
	}
	missing := doUsersRequest(t, server, http.MethodPost, "/api/admin/users/ghost/reset-password", `{"password":"new-password-1"}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing reset status=%d", missing.Code)
	}
}

func TestSecurityMutationsInvalidateTargetSessionsAndAudit(t *testing.T) {
	store := &usersStore{users: []LocalUser{
		{ID: "admin-1", Username: "admin", Role: "tenant_admin", Status: "active", CreatedAt: time.Unix(100, 0)},
		{ID: "viewer-1", Username: "reader", Role: "viewer", Status: "active", CreatedAt: time.Unix(101, 0)},
	}}
	server := New(AllOf(&fakeBackend{}), authorizer{session: Session{AdminID: "admin-1", TenantID: "t", Username: "admin", Role: rbac.RoleTenantAdmin}})
	server.WithUserManagement(store, userPasswords{}, &testIDGenerator{})
	var invalidated []string
	server.WithSessionInvalidation(func(adminID string) { invalidated = append(invalidated, adminID) })
	type event struct {
		actor, action, resourceType, resourceID string
	}
	var events []event
	server.WithAudit(func(_ context.Context, tenantID, actorID, action, resourceType, resourceID string) error {
		events = append(events, event{actorID, action, resourceType, resourceID})
		return nil
	})

	disable := doUsersRequest(t, server, http.MethodPost, "/api/admin/users/viewer-1/status", `{"status":"disabled"}`)
	if disable.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disable.Code, disable.Body.String())
	}
	reset := doUsersRequest(t, server, http.MethodPost, "/api/admin/users/viewer-1/reset-password", `{"password":"new-password-1"}`)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset status=%d body=%s", reset.Code, reset.Body.String())
	}
	deleteReq := doUsersRequest(t, server, http.MethodDelete, "/api/admin/users/viewer-1", "")
	if deleteReq.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleteReq.Code, deleteReq.Body.String())
	}

	wantInvalidated := []string{"viewer-1", "viewer-1", "viewer-1"}
	if strings.Join(invalidated, ",") != strings.Join(wantInvalidated, ",") {
		t.Fatalf("invalidated=%v want %v", invalidated, wantInvalidated)
	}
	wantActions := []string{"user.disable", "user.password_reset_admin", "user.delete"}
	if len(events) != len(wantActions) {
		t.Fatalf("audit events=%d want %d: %+v", len(events), len(wantActions), events)
	}
	for i, action := range wantActions {
		if events[i].action != action || events[i].actor != "admin-1" || events[i].resourceID != "viewer-1" {
			t.Fatalf("event[%d]=%+v want action=%s", i, events[i], action)
		}
	}
}

func TestMeExposesRoleUsernameAndScopes(t *testing.T) {
	server, _ := newUsersServer(t, Session{AdminID: "admin-1", TenantID: "t", Username: "admin", Role: rbac.RoleTenantOperator})
	response := doUsersRequest(t, server, http.MethodGet, "/api/admin/me", "")
	if response.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", response.Code, response.Body.String())
	}
	var result map[string]any
	if json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatalf("decode me: %s", response.Body.String())
	}
	if result["role"] != rbac.RoleTenantOperator || result["username"] != "admin" {
		t.Fatalf("me=%v", result)
	}
	scopes, _ := result["scopes"].([]any)
	joined := ""
	for _, scope := range scopes {
		joined += scope.(string) + ","
	}
	if !strings.Contains(joined, rbac.PermApprovalDecide) || strings.Contains(joined, rbac.PermDelegationGrant) {
		t.Fatalf("operator scopes=%v", joined)
	}
}

func TestWriteEndpointsRejectViewer(t *testing.T) {
	server, _ := newUsersServer(t, Session{AdminID: "viewer-1", TenantID: "t", Username: "reader", Role: rbac.RoleViewer})
	for _, endpoint := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/admin/keys"},
		{http.MethodPost, "/api/admin/config/drafts"},
		{http.MethodPost, "/api/admin/projects"},
		{http.MethodPost, "/api/admin/alerts/rules"},
		{http.MethodPost, "/api/admin/alerts/rules/import-defaults"},
		{http.MethodPut, "/api/admin/alerts/notifications"},
		{http.MethodDelete, "/api/admin/alerts/notifications"},
	} {
		response := doUsersRequest(t, server, endpoint.method, endpoint.path, `{}`)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d want 403", endpoint.method, endpoint.path, response.Code)
		}
	}
}

func TestApprovalsAndFederationAllowOperator(t *testing.T) {
	operator, _ := newUsersServer(t, Session{AdminID: "op-1", TenantID: "t", Username: "op", Role: rbac.RoleTenantOperator})
	viewer, _ := newUsersServer(t, Session{AdminID: "viewer-1", TenantID: "t", Username: "reader", Role: rbac.RoleViewer})

	opApprove := doUsersRequest(t, operator, http.MethodPost, "/api/admin/approvals/req-1/action", `{"decision":"approve"}`)
	if opApprove.Code != http.StatusOK {
		t.Fatalf("operator approve status=%d body=%s", opApprove.Code, opApprove.Body.String())
	}
	viewerApprove := doUsersRequest(t, viewer, http.MethodPost, "/api/admin/approvals/req-1/action", `{"decision":"approve"}`)
	if viewerApprove.Code != http.StatusForbidden {
		t.Fatalf("viewer approve status=%d want 403", viewerApprove.Code)
	}
	opSuspend := doUsersRequest(t, operator, http.MethodPost, "/api/admin/federation/rel-1/suspend", `{}`)
	if opSuspend.Code != http.StatusOK {
		t.Fatalf("operator suspend status=%d", opSuspend.Code)
	}
}
