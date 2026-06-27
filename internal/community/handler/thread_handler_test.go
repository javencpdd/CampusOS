package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/community/repository"
	"github.com/campusos/CampusOS/internal/community/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestCreateThreadUsesJWTContext(t *testing.T) {
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

	threadRepo := repository.NewMemoryThreadRepository()
	threadSvc := service.NewThreadService(threadRepo, nil)
	threadHandler := NewThreadHandler(threadSvc)

	router := gin.New()
	router.POST("/threads", middleware.JWTAuth(jwtMgr), threadHandler.CreateThread)

	body := bytes.NewBufferString(`{"title":"hello","content":"world","category_id":"1"}`)
	req := httptest.NewRequest(http.MethodPost, "/threads", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var payload struct {
		Code int `json:"code"`
		Data struct {
			AuthorID   string `json:"author_id"`
			AuthorName string `json:"author_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != 0 {
		t.Fatalf("expected code 0, got %d", payload.Code)
	}
	if payload.Data.AuthorID != "1001" {
		t.Fatalf("expected author_id from token, got %q", payload.Data.AuthorID)
	}
	if payload.Data.AuthorName != "alice" {
		t.Fatalf("expected author_name from token, got %q", payload.Data.AuthorName)
	}
}
