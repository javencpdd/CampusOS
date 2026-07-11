package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/internal/core/identity/repository"
	"github.com/campusos/CampusOS/internal/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestUpdateUserAuthorizationAndFieldFiltering(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := repository.NewMemoryUserRepository()
	now := time.Now().UTC()
	for _, user := range []*domain.User{
		{ID: "1001", Username: "alice", Nickname: "Alice", Email: "alice@example.com", Status: domain.UserStatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: "1002", Username: "bob", Nickname: "Bob", Email: "bob@example.com", Status: domain.UserStatusActive, CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.Create(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	jwtMgr := auth.NewJWTManager(auth.JWTConfig{Secret: "test-secret", AccessTTL: time.Hour, Issuer: "test"})
	token, err := jwtMgr.GenerateAccessToken("1001", "alice")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewUserHandler(service.NewUserService(repo, jwtMgr, nil, nil))
	router := gin.New()
	router.Use(middleware.TraceID())
	router.PUT("/users/:id", middleware.JWTAuth(jwtMgr), handler.UpdateUser)
	router.GET("/users/:id", handler.GetUser)

	request := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if authenticated {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	if recorder := request(http.MethodPut, "/users/1002", `{"nickname":"Taken over"}`, true); recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-user update status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := request(http.MethodPut, "/users/1001", `{"nickname":"Alice 2","status":"suspended"}`, true); recorder.Code != http.StatusBadRequest {
		t.Fatalf("system field update status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := request(http.MethodPut, "/users/1001", `{"nickname":"Alice 2"}`, true); recorder.Code != http.StatusOK {
		t.Fatalf("self update status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := request(http.MethodPut, "/users/1001", `{"nickname":"Alice 3"}`, false); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous update status = %d: %s", recorder.Code, recorder.Body.String())
	}

	public := request(http.MethodGet, "/users/1001", "", false)
	if public.Code != http.StatusOK {
		t.Fatalf("public profile status = %d: %s", public.Code, public.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(public.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(public.Body.String(), "alice@example.com") {
		t.Fatalf("public profile leaked email: %s", public.Body.String())
	}
	if payload["request_id"] == "" {
		t.Fatalf("public response lacks request_id: %s", public.Body.String())
	}
}
