package storage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
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

func TestContentImageStoreListsOnlyTheCurrentOwnersImages(t *testing.T) {
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewContentImageStore(adapter, adapter, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Save("user-1", "first.png", bytes.NewReader(testPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save("user-2", "other.png", bytes.NewReader(testPNG(t))); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListOwned("user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].FileName != first.FileName || !strings.Contains(items[0].FileURL, "/user-1/") {
		t.Fatalf("owner media inventory leaked or omitted an image: %#v", items)
	}
	if items[0].CreatedAt.IsZero() {
		t.Fatalf("listed media must include a safe upload timestamp: %#v", items[0])
	}
}

func TestOptimizeImageReportsSourceDimensionsForSafeUserGuidance(t *testing.T) {
	data := pngHeader(8000, 6000) // 48 MP; DecodeConfig succeeds without decoding a bitmap.
	_, err := OptimizeImage(data)
	if !errors.Is(err, ErrImageDimensions) {
		t.Fatalf("expected decoded-pixel error, got %v", err)
	}
	var dimensions *ImageDimensionError
	if !errors.As(err, &dimensions) || dimensions.Width != 8000 || dimensions.Height != 6000 || dimensions.MaxPixels != MaxDecodedImagePixels {
		t.Fatalf("expected safe source dimensions, got %#v", dimensions)
	}
}

func TestOptimizeImageRejectsOversizedWebPUsingHeaderDimensions(t *testing.T) {
	data := webpVP8XHeader(8000, 6000) // 48 MP; WebP is never rasterized for this check.
	_, err := OptimizeImage(data)
	if !errors.Is(err, ErrImageDimensions) {
		t.Fatalf("expected decoded-pixel error, got %v", err)
	}
	var dimensions *ImageDimensionError
	if !errors.As(err, &dimensions) || dimensions.Width != 8000 || dimensions.Height != 6000 {
		t.Fatalf("expected WebP dimensions in the error, got %#v", dimensions)
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
	if !strings.Contains(recorder.Body.String(), "正文图片文件过大") || !strings.Contains(recorder.Body.String(), "max_bytes") {
		t.Fatalf("expected actionable Chinese image-limit details, body=%s", recorder.Body.String())
	}
}

func pngHeader(width, height uint32) []byte {
	data := make([]byte, 0, 33)
	data = append(data, 0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a)
	data = append(data, 0, 0, 0, 13, 'I', 'H', 'D', 'R')
	chunk := make([]byte, 13)
	binary.BigEndian.PutUint32(chunk[0:4], width)
	binary.BigEndian.PutUint32(chunk[4:8], height)
	chunk[8] = 8
	chunk[9] = 6
	data = append(data, chunk...)
	crc := make([]byte, 4)
	binary.BigEndian.PutUint32(crc, crc32.ChecksumIEEE(append([]byte("IHDR"), chunk...)))
	return append(data, crc...)
}

func webpVP8XHeader(width, height uint32) []byte {
	if width == 0 || height == 0 || width > 1<<24 || height > 1<<24 {
		panic("test WebP dimensions must fit the VP8X 24-bit canvas fields")
	}
	data := make([]byte, 30)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], 22) // WEBP + one 10-byte VP8X chunk.
	copy(data[8:12], "WEBP")
	copy(data[12:16], "VP8X")
	binary.LittleEndian.PutUint32(data[16:20], 10)
	canvasWidth := width - 1
	canvasHeight := height - 1
	data[24], data[25], data[26] = byte(canvasWidth), byte(canvasWidth>>8), byte(canvasWidth>>16)
	data[27], data[28], data[29] = byte(canvasHeight), byte(canvasHeight>>8), byte(canvasHeight>>16)
	return data
}
