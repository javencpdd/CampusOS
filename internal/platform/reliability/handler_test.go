package reliability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/gin-gonic/gin"
)

func TestListEventsPaginatesAndDoesNotExposePayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMemoryStore()
	service := NewService(transaction.NewMemory(), store)
	base := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	for index := 0; index < 3; index++ {
		event := Event{
			ID:             string(rune('a' + index)),
			Type:           "audit.safe",
			Payload:        json.RawMessage(`{"secret":"must-not-leak"}`),
			Headers:        json.RawMessage(`{"Authorization":"must-not-leak"}`),
			IdempotencyKey: "must-not-leak-" + string(rune('a'+index)),
			CreatedAt:      base,
			UpdatedAt:      base,
		}
		if _, err := service.Enqueue(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}

	handler := NewHandler(service)
	router := gin.New()
	router.GET("/events", func(c *gin.Context) {
		c.Set("user_id", "admin")
		handler.ListEvents(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/events?page=2&page_size=2", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var envelope struct {
		Data struct {
			Items      []map[string]any `json:"items"`
			Total      int64            `json:"total"`
			Pagination struct {
				Page       int   `json:"page"`
				PageSize   int   `json:"page_size"`
				Total      int64 `json:"total"`
				TotalPages int   `json:"total_pages"`
			} `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Total != 3 || envelope.Data.Pagination.Page != 2 || envelope.Data.Pagination.TotalPages != 2 || len(envelope.Data.Items) != 1 {
		t.Fatalf("unexpected pagination: %+v", envelope.Data)
	}
	if envelope.Data.Items[0]["id"] != "a" {
		t.Fatalf("pagination order is not deterministic: %+v", envelope.Data.Items)
	}
	for _, forbidden := range []string{"payload", "headers", "idempotency_key"} {
		if _, exists := envelope.Data.Items[0][forbidden]; exists {
			t.Fatalf("event response exposed %s", forbidden)
		}
	}
}

func TestQueryLimiterBoundsIdentityWindows(t *testing.T) {
	limiter := newQueryLimiter(1, time.Minute)
	limiter.capacity = 3
	for _, key := range []string{"user:d", "user:c", "user:b", "user:a"} {
		if allowed, _ := limiter.Allow(key); !allowed {
			t.Fatalf("first query for %s was rejected", key)
		}
	}
	if got := len(limiter.windows); got != limiter.capacity {
		t.Fatalf("identity windows = %d, want %d", got, limiter.capacity)
	}
	if _, exists := limiter.windows["user:d"]; exists {
		t.Fatal("oldest deterministic identity window was not evicted")
	}
}

func TestReliabilityQueryLimiterReturnsRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(NewService(transaction.NewMemory(), NewMemoryStore()))
	handler.queryLimiter = newQueryLimiter(1, time.Minute)
	router := gin.New()
	router.GET("/summary", func(c *gin.Context) {
		c.Set("user_id", "admin")
		handler.Summary(c)
	})

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/summary", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/summary", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, body = %s", second.Code, second.Body.String())
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header is missing")
	}
}
