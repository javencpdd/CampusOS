package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

func TestJWTAndPermissionNegativePathsUseStructuredErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jwtMgr := auth.NewJWTManager(auth.JWTConfig{Secret: "test-secret", AccessTTL: time.Hour, Issuer: "test"})
	userRepo := repository.NewMemoryUserRepository()
	if err := userRepo.Create(context.Background(), &domain.User{
		ID: "1001", Username: "alice", Nickname: "Alice", Email: "alice@example.test",
		Status: domain.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	permissionService := service.NewPermissionService(repository.NewMemoryRoleRepository(), userRepo)
	token, err := jwtMgr.GenerateAccessToken("1001", "alice")
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(TraceID())
	router.GET("/sensitive", JWTAuth(jwtMgr), RequirePermission(permissionService, "user", "suspend"), func(c *gin.Context) {
		response.Success(c, gin.H{"allowed": true})
	})

	for _, test := range []struct {
		name       string
		authority  string
		wantStatus int
		wantCode   string
	}{
		{name: "anonymous", wantStatus: http.StatusUnauthorized, wantCode: "auth.required"},
		{name: "malformed", authority: "Token invalid", wantStatus: http.StatusUnauthorized, wantCode: "auth.required"},
		{name: "member denied", authority: "Bearer " + token, wantStatus: http.StatusForbidden, wantCode: "permission.denied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/sensitive", nil)
			if test.authority != "" {
				req.Header.Set("Authorization", test.authority)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			var payload response.Response
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Error == nil || payload.Error.Code != test.wantCode || payload.Error.RequestID == "" {
				t.Fatalf("unexpected error contract: %#v", payload)
			}
		})
	}
}
