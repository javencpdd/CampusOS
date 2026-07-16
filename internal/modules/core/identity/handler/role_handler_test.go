package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/gin-gonic/gin"
)

func TestRoleHandlerReturnsActionableRoleAssignmentErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	userRepo := repository.NewMemoryUserRepository()
	for _, user := range []*domain.User{
		{ID: "1001", Username: "role_handler_user", Nickname: "Role Handler User", Email: "role-handler@example.test", Status: domain.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "9001", Username: "role_handler_admin", Nickname: "Role Handler Admin", Email: "role-handler-admin@example.test", Status: domain.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	} {
		if err := userRepo.Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}

	permissionService := service.NewPermissionService(repository.NewMemoryRoleRepository(), userRepo)
	if _, err := permissionService.AssignRole(ctx, "9001", 1); err != nil {
		t.Fatalf("seed administrator: %v", err)
	}
	handler := NewRoleHandler(permissionService)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("user_id", "9001") })
	router.POST("/users/:id/roles", handler.AssignRole)
	router.DELETE("/users/:id/roles", handler.RevokeRole)

	assignment := performRoleRequest(t, router, http.MethodPost, "/users/1001/roles", `{"role_id":1}`)
	if assignment.Code != http.StatusOK || assignment.Payload.Code != 0 || !assignment.Payload.Data.Assigned {
		t.Fatalf("unexpected role assignment response: status=%d payload=%#v", assignment.Code, assignment.Payload)
	}

	protected := performRoleRequest(t, router, http.MethodPost, "/users/1001/roles", `{"role_id":3}`)
	if protected.Code != http.StatusForbidden || protected.Payload.Code != 70012 {
		t.Fatalf("expected protected member role error, got status=%d payload=%#v", protected.Code, protected.Payload)
	}

	moderatorWithoutScope := performRoleRequest(t, router, http.MethodPost, "/users/1001/roles", `{"role_id":2}`)
	if moderatorWithoutScope.Code != http.StatusBadRequest || moderatorWithoutScope.Payload.Code != 70013 {
		t.Fatalf("expected scoped moderator error, got status=%d payload=%#v", moderatorWithoutScope.Code, moderatorWithoutScope.Payload)
	}

	selfRevoke := performRoleRequest(t, router, http.MethodDelete, "/users/9001/roles", `{"role_id":1}`)
	if selfRevoke.Code != http.StatusForbidden || selfRevoke.Payload.Code != 20004 {
		t.Fatalf("expected self revoke rejection, got status=%d payload=%#v", selfRevoke.Code, selfRevoke.Payload)
	}

	unauthenticated := gin.New()
	unauthenticated.POST("/users/:id/roles", handler.AssignRole)
	missingActor := performRoleRequest(t, unauthenticated, http.MethodPost, "/users/1001/roles", `{"role_id":1}`)
	if missingActor.Code != http.StatusUnauthorized || missingActor.Payload.Code != 20001 {
		t.Fatalf("missing actor context must fail closed, got status=%d payload=%#v", missingActor.Code, missingActor.Payload)
	}
}

type roleHandlerResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Assigned bool `json:"assigned"`
	} `json:"data"`
}

type recordedRoleResponse struct {
	Code    int
	Payload roleHandlerResponse
}

func performRoleRequest(t *testing.T, router http.Handler, method, path, body string) recordedRoleResponse {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var payload roleHandlerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return recordedRoleResponse{Code: rec.Code, Payload: payload}
}
