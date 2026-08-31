package richtext

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/gin-gonic/gin"
)

func TestWriteRichTextAssetUploadErrorExplainsSourceDimensions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	writeRichTextAssetUploadError(context, &corestorage.ImageDimensionError{
		Width: 8000, Height: 6000, MaxPixels: corestorage.MaxDecodedImagePixels,
	}, 5*1024*1024, 1234)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Msg   string `json:"msg"`
		Error struct {
			Details struct {
				Width            int   `json:"width"`
				Height           int   `json:"height"`
				MaxDecodedPixels int64 `json:"max_decoded_pixels"`
				MaxBytes         int64 `json:"max_bytes"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(payload.Msg, "8000 × 6000") || !strings.Contains(payload.Msg, "4000 万像素") {
		t.Fatalf("expected actionable Chinese dimension message, got %#v", payload)
	}
	if payload.Error.Details.Width != 8000 || payload.Error.Details.Height != 6000 ||
		payload.Error.Details.MaxDecodedPixels != corestorage.MaxDecodedImagePixels ||
		payload.Error.Details.MaxBytes != 5*1024*1024 {
		t.Fatalf("expected dimension details, got %#v", payload.Error.Details)
	}
}
