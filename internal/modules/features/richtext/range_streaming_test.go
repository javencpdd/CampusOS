package richtext

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/gin-gonic/gin"
)

// Generates a 20 MiB object without allocating it; the test fails if a 64 KiB
// Range request reads the whole object instead of streaming the selected bytes.
type measuredPDF struct {
	position, read int64
	closed         bool
}

func (r *measuredPDF) Read(p []byte) (int, error) {
	n := len(p)
	if int64(n) > MaxArticleAttachmentBytes-r.position {
		n = int(MaxArticleAttachmentBytes - r.position)
	}
	if n <= 0 {
		return 0, io.EOF
	}
	clear(p[:n])
	r.position += int64(n)
	r.read += int64(n)
	return n, nil
}
func (r *measuredPDF) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		r.position = offset
	case io.SeekCurrent:
		r.position += offset
	case io.SeekEnd:
		r.position = MaxArticleAttachmentBytes + offset
	}
	return r.position, nil
}
func (r *measuredPDF) Close() error { r.closed = true; return nil }

type cancelledWriter struct{ *httptest.ResponseRecorder }

func (w cancelledWriter) Write([]byte) (int, error) { return 0, context.Canceled }

func TestTwentyRangesStreamOnlyRequestedBytesAndReleaseOnCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("user_id", "user")
			c.Request = httptest.NewRequest(http.MethodGet, "/content", nil)
			c.Request.Header.Set("Range", "bytes=1048576-1114111")
			reader := &measuredPDF{}
			handler.serveOpenedAttachment(c, AttachmentOpen{Attachment: ArticleAttachment{DisplayName: "fixture.pdf", Asset: UserAsset{MimeType: "application/pdf"}}, Object: corestorage.Object{UpdatedAt: time.Now(), SHA256: "fixture"}, Reader: reader}, true)
			if recorder.Code != 206 || reader.read != 65536 || !reader.closed {
				t.Errorf("range response status=%d bytes=%d closed=%v", recorder.Code, reader.read, reader.closed)
			}
		}()
	}
	wg.Wait()
	c, _ := gin.CreateTestContext(cancelledWriter{httptest.NewRecorder()})
	c.Set("user_id", "user")
	c.Request = httptest.NewRequest(http.MethodGet, "/content", nil)
	reader := &measuredPDF{}
	handler.serveOpenedAttachment(c, AttachmentOpen{Attachment: ArticleAttachment{Asset: UserAsset{MimeType: "application/pdf"}}, Reader: reader}, true)
	if !reader.closed || handler.contentAdmission.active != 0 {
		t.Fatal("cancelled writer leaked resources")
	}
}
