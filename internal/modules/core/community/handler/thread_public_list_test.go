package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	"github.com/gin-gonic/gin"
)

type publicListHTTPRepository struct {
	*repository.MemoryThreadRepository
	calls      int
	failAt     int
	change     string
	threadID   string
	lastFilter domain.ThreadListFilter
}

func (r *publicListHTTPRepository) List(ctx context.Context, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	r.calls++
	r.lastFilter = filter
	if r.calls == r.failAt {
		return nil, 0, errors.New("query failed secret-diagnostic-body /private/database/path")
	}
	if r.calls%2 == 0 && r.change != "" {
		thread, err := r.MemoryThreadRepository.GetByID(ctx, r.threadID)
		if err != nil {
			return nil, 0, err
		}
		thread.Content = "secret-changed-body"
		if r.change == "private" {
			thread.PublicationStatus = domain.PublicationStatusPrivate
		} else {
			thread.Title += " changed"
		}
		if err := r.MemoryThreadRepository.Update(ctx, thread); err != nil {
			return nil, 0, err
		}
	}
	return r.MemoryThreadRepository.List(ctx, filter)
}

func publicListHTTPSeed(t *testing.T, repo *repository.MemoryThreadRepository, id, state string) {
	t.Helper()
	thread := &domain.Thread{ID: id, AuthorID: "99102", CategoryID: "99103", Title: "public-title", Content: "public-body",
		PublicationStatus: domain.PublicationStatus(state), ModerationStatus: domain.ModerationStatusClear, DeletionStatus: domain.DeletionStatusActive}
	if err := repo.Create(context.Background(), thread); err != nil {
		t.Fatal(err)
	}
}

func TestPublicThreadListHTTPErrorBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name   string
		status int
	}{
		{"public", 200}, {"read-fault", 500}, {"recheck-fault", 500}, {"busy", 409}, {"withdrawn", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &publicListHTTPRepository{MemoryThreadRepository: repository.NewMemoryThreadRepository(), threadID: "99101"}
			publicListHTTPSeed(t, repo.MemoryThreadRepository, repo.threadID, "published")
			switch test.name {
			case "read-fault":
				repo.failAt = 1
			case "recheck-fault":
				repo.failAt = 2
			case "busy":
				repo.change = "busy"
			case "withdrawn":
				repo.change = "private"
			}
			router := gin.New()
			router.GET("/threads", NewThreadHandler(service.NewThreadService(repo, nil)).ListThreads)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
				"/threads?status=all&publication_status=private&moderation_status=taken_down&deletion_status=trashed&include_trashed=true", nil))
			if rec.Code != test.status {
				t.Fatalf("status: got %d want %d: %s", rec.Code, test.status, rec.Body.String())
			}
			body := rec.Body.String()
			for _, hidden := range []string{"secret-changed-body", "secret-diagnostic-body", "/private/database/path", "facts_version"} {
				if strings.Contains(body, hidden) {
					t.Fatalf("response exposed %s: %s", hidden, body)
				}
			}
			if test.name != "public" && strings.Contains(body, "public-body") {
				t.Fatal("failed or withdrawn list contains content")
			}
			if test.name == "busy" && !strings.Contains(body, `"code":"resource.conflict"`) {
				t.Fatalf("missing stable conflict error: %s", body)
			}
			if test.name == "read-fault" || test.name == "recheck-fault" {
				if !strings.Contains(body, `"code":"internal.error"`) {
					t.Fatalf("missing safe internal error: %s", body)
				}
			}
			if repo.lastFilter.Status != "" || repo.lastFilter.PublicationStatus != "published" ||
				repo.lastFilter.ModerationStatus != "clear" || repo.lastFilter.DeletionStatus != "active" || repo.lastFilter.IncludeTrashed {
				t.Fatalf("public query expanded visibility: %+v", repo.lastFilter)
			}
			if test.name == "withdrawn" {
				var payload struct {
					Data struct {
						Items      []json.RawMessage `json:"items"`
						Pagination struct {
							Total int64 `json:"total"`
						} `json:"pagination"`
					} `json:"data"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
					t.Fatal(err)
				}
				if len(payload.Data.Items) != 0 || payload.Data.Pagination.Total != 0 {
					t.Fatalf("withdrawn list retained old count or item: %s", body)
				}
			}
		})
	}
}

func TestPublicThreadListHTTPEmptyPageKeepsPublicCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := repository.NewMemoryThreadRepository()
	publicListHTTPSeed(t, repo, "99101", "published")
	publicListHTTPSeed(t, repo, "99104", "published")
	publicListHTTPSeed(t, repo, "99105", "private")
	router := gin.New()
	router.GET("/threads", NewThreadHandler(service.NewThreadService(repo, nil)).ListThreads)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/threads?page=3&page_size=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data struct {
			Items      []json.RawMessage `json:"items"`
			Pagination struct {
				Page       int   `json:"page"`
				PageSize   int   `json:"page_size"`
				Total      int64 `json:"total"`
				TotalPages int   `json:"total_pages"`
			} `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	page := payload.Data.Pagination
	if payload.Data.Items == nil || len(payload.Data.Items) != 0 || page.Page != 3 || page.PageSize != 1 || page.Total != 2 || page.TotalPages != 2 {
		t.Fatalf("empty page lost its public count or array shape: %s", rec.Body.String())
	}
}
