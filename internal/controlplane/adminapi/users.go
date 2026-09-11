package adminapi

import (
	"errors"
	"net/http"
	"net/mail"

	"github.com/F31/liteAIG/internal/controlplane/rbac"
	"github.com/F31/liteAIG/internal/platform/webkit"
)

// UserIDGenerator mints local account identifiers.
type UserIDGenerator interface{ New() (string, error) }

// UserView is the user directory row returned to the Console.
type UserView struct {
	LocalUser
	IsSelf bool `json:"isSelf"`
}

// WithUserManagement wires the local user directory, password hashing, and ID
// generation so the /users endpoints are served. Without it they answer
// NOT_IMPLEMENTED.
func (s *Server) WithUserManagement(store LocalCredentialStore, passwords PasswordVerifier, ids UserIDGenerator) *Server {
	s.userStore = store
	s.userPasswords = passwords
	s.userIDs = ids
	return s
}

func (s *Server) usersReady() bool {
	return s.userStore != nil && s.userPasswords != nil && s.userIDs != nil
}

func (s *Server) listUsers(c *webkit.Context) error {
	if !s.usersReady() {
		return notImplemented()
	}
	users, err := s.userStore.ListLocalUsers(c.Request().Context())
	if err != nil {
		return internalError()
	}
	session := sessionFrom(c)
	result := make([]UserView, 0, len(users))
	for _, user := range users {
		result = append(result, UserView{LocalUser: user, IsSelf: user.ID == session.AdminID})
	}
	return c.JSON(http.StatusOK, result)
}

func (s *Server) createUser(c *webkit.Context) error {
	if !s.usersReady() {
		return notImplemented()
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
		Email    string `json:"email"`
	}
	if err := c.Bind(&input, 1<<20); err != nil ||
		input.Username == "" || len([]rune(input.Username)) > 64 ||
		len(input.Password) < minimumResetPasswordSize ||
		!validRole(input.Role) ||
		!validEmail(input.Email) {
		return invalidRequest()
	}
	password := []byte(input.Password)
	input.Password = ""
	defer clear(password)
	hash, err := s.userPasswords.Hash(password)
	if err != nil {
		return internalError()
	}
	id, err := s.userIDs.New()
	if err != nil {
		return internalError()
	}
	user, err := s.userStore.CreateLocalUser(c.Request().Context(), LocalUser{ID: id, Username: input.Username, Role: input.Role, PasswordHash: hash, Email: input.Email})
	if errors.Is(err, ErrUsernameTaken) {
		return webkit.NewAPIError(http.StatusConflict, "USERNAME_TAKEN", nil)
	}
	if err != nil {
		return internalError()
	}
	s.recordAudit(c, "user.create", "local_user", user.ID)
	return c.JSON(http.StatusCreated, UserView{LocalUser: user})
}

// updateUserEmail sets the address a password-reset code is delivered to.
// Admins may change any account; the /users/me/email route lets an account
// manage its own address without admin rights.
func (s *Server) updateUserEmail(c *webkit.Context) error {
	if !s.usersReady() {
		return notImplemented()
	}
	var input struct {
		Email string `json:"email"`
	}
	if err := c.Bind(&input, 1<<20); err != nil || !validEmail(input.Email) {
		return invalidRequest()
	}
	if _, err := s.findUserTarget(c); err != nil {
		return err
	}
	user, storeErr := s.userStore.SetUserEmail(c.Request().Context(), c.Param("id"), input.Email)
	if err := userStoreError(storeErr); err != nil {
		return err
	}
	// The address is the delivery target for password-reset codes, making it
	// an account-takeover vector; changing it voids the account's sessions.
	s.invalidateSessions(user.ID)
	s.recordAudit(c, "user.email_change", "local_user", user.ID)
	return c.JSON(http.StatusOK, UserView{LocalUser: user, IsSelf: user.ID == sessionFrom(c).AdminID})
}

func (s *Server) updateMyEmail(c *webkit.Context) error {
	if !s.usersReady() {
		return notImplemented()
	}
	var input struct {
		Email string `json:"email"`
	}
	if err := c.Bind(&input, 1<<20); err != nil || !validEmail(input.Email) {
		return invalidRequest()
	}
	session := sessionFrom(c)
	user, storeErr := s.userStore.SetUserEmail(c.Request().Context(), session.AdminID, input.Email)
	if err := userStoreError(storeErr); err != nil {
		return err
	}
	s.invalidateSessions(user.ID)
	s.recordAudit(c, "user.email_change_self", "local_user", user.ID)
	return c.JSON(http.StatusOK, UserView{LocalUser: user, IsSelf: true})
}

// validEmail accepts an empty address (unset) or a syntactically valid one.
func validEmail(email string) bool {
	if email == "" {
		return true
	}
	if len(email) > 254 {
		return false
	}
	_, err := mail.ParseAddress(email)
	return err == nil
}

func (s *Server) updateUserRole(c *webkit.Context) error {
	if !s.usersReady() {
		return notImplemented()
	}
	var input struct {
		Role string `json:"role"`
	}
	if err := c.Bind(&input, 1<<20); err != nil || !validRole(input.Role) {
		return invalidRequest()
	}
	target, err := s.findUserTarget(c)
	if err != nil {
		return err
	}
	if target.AdminID == sessionFrom(c).AdminID {
		return invalidRequest()
	}
	if isLastActiveAdmin(target) {
		if err := s.lastAdminAllows(c); err != nil {
			return err
		}
	}
	user, storeErr := s.userStore.SetUserRole(c.Request().Context(), c.Param("id"), input.Role)
	if err := userStoreError(storeErr); err != nil {
		return err
	}
	// Sessions snapshot their role at login; a demotion must not keep the old
	// privilege alive for the session TTL.
	s.invalidateSessions(user.ID)
	s.recordAudit(c, "user.role_change", "local_user", user.ID)
	return c.JSON(http.StatusOK, UserView{LocalUser: user, IsSelf: user.ID == sessionFrom(c).AdminID})
}

func (s *Server) updateUserStatus(c *webkit.Context) error {
	if !s.usersReady() {
		return notImplemented()
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := c.Bind(&input, 1<<20); err != nil ||
		(input.Status != "active" && input.Status != "disabled") {
		return invalidRequest()
	}
	target, err := s.findUserTarget(c)
	if err != nil {
		return err
	}
	if target.AdminID == sessionFrom(c).AdminID {
		return invalidRequest()
	}
	if input.Status == "disabled" && isLastActiveAdmin(target) {
		if err := s.lastAdminAllows(c); err != nil {
			return err
		}
	}
	user, storeErr := s.userStore.SetUserStatus(c.Request().Context(), c.Param("id"), input.Status)
	if err := userStoreError(storeErr); err != nil {
		return err
	}
	// A disabled account's existing sessions must die now, not after the
	// 12-hour session TTL.
	s.invalidateSessions(user.ID)
	action := "user.disable"
	if input.Status == "active" {
		action = "user.enable"
	}
	s.recordAudit(c, action, "local_user", user.ID)
	return c.JSON(http.StatusOK, UserView{LocalUser: user, IsSelf: user.ID == sessionFrom(c).AdminID})
}

func (s *Server) resetUserPassword(c *webkit.Context) error {
	if !s.usersReady() {
		return notImplemented()
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := c.Bind(&input, 1<<20); err != nil || len(input.Password) < minimumResetPasswordSize {
		return invalidRequest()
	}
	target, err := s.findUserTarget(c)
	if err != nil {
		return err
	}
	password := []byte(input.Password)
	input.Password = ""
	defer clear(password)
	hash, err := s.userPasswords.Hash(password)
	if err != nil {
		return internalError()
	}
	if err := s.userStore.ResetLocalPassword(c.Request().Context(), target.Username, hash); err != nil {
		return userStoreError(err)
	}
	// A password reset by an admin must void the target's existing sessions.
	s.invalidateSessions(target.AdminID)
	s.recordAudit(c, "user.password_reset_admin", "local_user", target.AdminID)
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) deleteUser(c *webkit.Context) error {
	if !s.usersReady() {
		return notImplemented()
	}
	target, err := s.findUserTarget(c)
	if err != nil {
		return err
	}
	if target.AdminID == sessionFrom(c).AdminID {
		return invalidRequest()
	}
	if isLastActiveAdmin(target) {
		if err := s.lastAdminAllows(c); err != nil {
			return err
		}
	}
	if err := s.userStore.DeleteLocalUser(c.Request().Context(), c.Param("id")); err != nil {
		return userStoreError(err)
	}
	s.invalidateSessions(target.AdminID)
	s.recordAudit(c, "user.delete", "local_user", target.AdminID)
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) findUserTarget(c *webkit.Context) (LocalCredential, error) {
	target, err := s.userStore.FindLocalUserByID(c.Request().Context(), c.Param("id"))
	if errors.Is(err, ErrLocalUserNotFound) {
		return LocalCredential{}, webkit.NewAPIError(http.StatusNotFound, "NOT_FOUND", nil)
	}
	if err != nil {
		return LocalCredential{}, internalError()
	}
	return target, nil
}

// lastAdminAllows refuses mutations that would leave the tenant without any
// active tenant_admin.
func (s *Server) lastAdminAllows(c *webkit.Context) error {
	count, err := s.userStore.CountActiveAdmins(c.Request().Context())
	if err != nil || count <= 1 {
		return webkit.NewAPIError(http.StatusConflict, "LAST_ADMIN_PROTECTED", nil)
	}
	return nil
}

func isLastActiveAdmin(user LocalCredential) bool {
	return user.Role == rbac.RoleTenantAdmin && user.Status == "active"
}

func userStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrLocalUserNotFound) || errors.Is(err, ErrUsernameTaken) {
		code := "NOT_FOUND"
		status := http.StatusNotFound
		if errors.Is(err, ErrUsernameTaken) {
			code = "USERNAME_TAKEN"
			status = http.StatusConflict
		}
		return webkit.NewAPIError(status, code, nil)
	}
	return internalError()
}

func validRole(role string) bool {
	for _, candidate := range rbac.TenantRoles() {
		if role == candidate {
			return true
		}
	}
	return false
}
