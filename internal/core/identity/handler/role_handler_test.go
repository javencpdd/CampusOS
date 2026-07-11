package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/internal/core/identity/repository"
	"github.com/campusos/CampusOS/internal/core/identity/service"
	"github.com/gin-gonic/gin"
)

func TestRoleHandlerReturnsActionableRoleAssignmentErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userRepo := repository.NewMemoryUserRepository()
	if err := userRepo.Create(context.Background(), &domain.User{
		ID:        "1001",
		Username:  "role_handler_user",
		Nickname:  "Role Handler User",
		Email:     "role-handler@example.test",
		Status:    domain.UserStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	handler := NewRoleHandler(service.NewPermissionService(repository.NewMemoryRoleRepository(), userRepo))
	router := gin.New()
	router.POST("/users/:id/roles", handler.AssignRole)
	router.DELETE("/users/:id/roles", func(c *gin.Context) {
		c.Set("user_id", "1001")
		handler.RevokeRole(c)
	})

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

	selfRevoke := performRoleRequest(t, router, http.MethodDelete, "/users/1001/roles", `{"role_id":1}`)
	if selfRevoke.Code != http.StatusForbidden || selfRevoke.Payload.Code != 20004 {
		t.Fatalf("expected self revoke rejection, got status=%d payload=%#v", selfRevoke.Code, selfRevoke.Payload)
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
