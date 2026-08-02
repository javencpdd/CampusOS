package storage

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
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

func TestOptimizeImageDownsizesAndCompressesJPEG(t *testing.T) {
	var input bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 3000, 1000))
	for y := 0; y < 1000; y++ {
		for x := 0; x < 3000; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 120, A: 255})
		}
	}
	if err := jpeg.Encode(&input, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	optimized, err := OptimizeImage(input.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if optimized.Width != 1920 || optimized.Height != 640 || optimized.MimeType != "image/jpeg" {
		t.Fatalf("unexpected optimized image: %#v", optimized)
	}
	if len(optimized.Data) >= input.Len() {
		t.Fatalf("expected compressed output smaller than input: input=%d output=%d", input.Len(), len(optimized.Data))
	}
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
