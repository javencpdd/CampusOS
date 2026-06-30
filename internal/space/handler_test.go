package space

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestUpdateMeUsesJWTUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	jwtMgr := auth.NewJWTManager(auth.JWTConfig{
		Secret:    "test-secret",
		AccessTTL: time.Hour,
		Issuer:    "campusos-test",
	})
	token, err := jwtMgr.GenerateAccessToken("1001", "alice")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	handler := NewHandler(svc)

	router := gin.New()
	router.PUT("/spaces/me", middleware.JWTAuth(jwtMgr), handler.UpdateMe)

	req := httptest.NewRequest(http.MethodPut, "/spaces/me", bytes.NewBufferString(`{"title":"Alice Lab"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload struct {
		Code int `json:"code"`
		Data struct {
			Owner Owner  `json:"owner"`
			Space *Space `json:"space"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != 0 {
		t.Fatalf("expected code 0, got %d", payload.Code)
	}
	if payload.Data.Owner.ID != "1001" {
		t.Fatalf("expected owner id from token, got %q", payload.Data.Owner.ID)
	}
	if payload.Data.Space.Title != "Alice Lab" {
		t.Fatalf("expected updated title, got %q", payload.Data.Space.Title)
	}
}
