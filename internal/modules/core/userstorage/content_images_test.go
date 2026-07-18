package storage

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestContentImageStoreSavesUnderUserStorage(t *testing.T) {
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewContentImageStore(adapter, adapter, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	asset, err := store.Save("user-1", "preview.png", bytes.NewReader(testPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(asset.FileURL, ContentImageURLPrefix+"/user-1/") || asset.Width != 3 || asset.Height != 2 {
		t.Fatalf("unexpected asset: %#v", asset)
	}
	path, err := store.Path("user-1", asset.FileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	expected, err := adapter.Path("user-1", ImageDir, ContentImageDir, asset.FileName)
	if err != nil || path != expected {
		t.Fatalf("asset escaped content directory: path=%s expected=%s err=%v", path, expected, err)
	}
}

func TestContentImageStoreRejectsUnsupportedAndQuotaOverflow(t *testing.T) {
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 16)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewContentImageStore(adapter, adapter, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save("user-1", "note.txt", strings.NewReader("plain text")); !errors.Is(err, ErrImageUnsupported) {
		t.Fatalf("expected unsupported image, got %v", err)
	}
	if _, err := store.Save("user-1", "preview.png", bytes.NewReader(testPNG(t))); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected quota rejection, got %v", err)
	}
}

func TestContentImageHandlerRejectsOversizedBodyBeforeMultipartParsing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewContentImageStore(adapter, adapter, 64)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "oversized.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("x"), int(64+contentImageFormSlack+1))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/content/assets/images", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	context.Set("user_id", "user-1")
	NewHandler(store).UploadContentImage(context)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "74002") {
		t.Fatalf("expected stable upload error code, body=%s", recorder.Body.String())
	}
}
