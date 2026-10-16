package space

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestSpaceOwnerHTTPRealCurrentSessionStrictDTOAndSafeAck(t *testing.T) {
	runSpaceOwnerHTTPCurrentSession(t)
}

func TestUploadAvatarReturnsChineseActionableSizeLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	jwtMgr := auth.NewJWTManager(auth.JWTConfig{Secret: "test-secret", AccessTTL: time.Hour, Issuer: "campusos-test"})
	token, err := jwtMgr.GenerateAccessToken("1001", "alice")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID: "1001", Username: "alice", Nickname: "Alice",
	}))
	store, err := NewLocalFileStore(FileStorageConfig{
		RootDir: filepath.Join(t.TempDir(), "space-files"), MaxAvatarBytes: 32,
	})
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	svc.SetFileStore(store)
	handler := NewHandler(svc)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "avatar.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(make([]byte, 33)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close form: %v", err)
	}

	router := gin.New()
	router.POST("/spaces/me/avatar", middleware.JWTAuth(jwtMgr), handler.UploadAvatar)
	req := httptest.NewRequest(http.MethodPost, "/spaces/me/avatar", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d: %s", http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	}
	var payload struct {
		Msg   string `json:"msg"`
		Error struct {
			Details struct {
				MaxBytes int64 `json:"max_bytes"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Details.MaxBytes != 32 || !strings.Contains(payload.Msg, "单个文件最大 32 B") {
		t.Fatalf("expected actionable Chinese size limit, got %#v", payload)
	}
}
