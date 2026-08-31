package schedule

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestImportMeOversizedFileReturnsActionableChineseDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, err := NewService(Config{RootDir: t.TempDir(), MaxImportBytes: 10})
	if err != nil {
		t.Fatalf("new schedule service: %v", err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("user_id", "1001") })
	router.POST("/schedule/me/import", NewHandler(service).ImportMe)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("term_year", "2026"); err != nil {
		t.Fatalf("write year: %v", err)
	}
	if err := writer.WriteField("semester", "fall"); err != nil {
		t.Fatalf("write semester: %v", err)
	}
	file, err := writer.CreateFormFile("file", "schedule.csv")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := file.Write(bytes.Repeat([]byte("x"), 11)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/schedule/me/import", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "academic_term.invalid" {
		t.Fatalf("machine code = %q, body = %s", payload.Error.Code, recorder.Body.String())
	}
	if payload.Error.Details["reason"] != "课表导入文件超过大小限制" {
		t.Fatalf("reason = %#v", payload.Error.Details["reason"])
	}
	if payload.Error.Details["max_bytes"] != float64(10) || payload.Error.Details["actual_bytes"] != float64(11) {
		t.Fatalf("size details = %#v", payload.Error.Details)
	}
}
