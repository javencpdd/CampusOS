package handler

import (
	"context"
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

type publicDetailHTTPRepository struct {
	*repository.MemoryThreadRepository
	readError  error
	finalError error
	change     string
}

func (r *publicDetailHTTPRepository) GetByID(ctx context.Context, id string) (*domain.Thread, error) {
	if r.readError != nil {
		return nil, r.readError
	}
	return r.MemoryThreadRepository.GetByID(ctx, id)
}

func (r *publicDetailHTTPRepository) RecheckPublicThread(ctx context.Context, id string, version int64, view bool) (*domain.Thread, error) {
	if r.finalError != nil {
		return nil, r.finalError
	}
	if r.change != "" {
		current, err := r.MemoryThreadRepository.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if r.change == "private" {
			current.PublicationStatus = "private"
		}
		current.Content = "secret-changed-body"
		if err = r.MemoryThreadRepository.Update(ctx, current); err != nil {
			return nil, err
		}
	}
	return r.MemoryThreadRepository.RecheckPublicThread(ctx, id, version, view)
}

func TestPublicThreadDetailHTTPErrorBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name   string
		status int
	}{
		{"public", 200}, {"private", 404}, {"read-fault", 500}, {"recheck-fault", 500}, {"busy", 409},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &publicDetailHTTPRepository{MemoryThreadRepository: repository.NewMemoryThreadRepository()}
			thread := &domain.Thread{ID: "99101", AuthorID: "99102", CategoryID: "99103", Title: "public-title", Content: "public-body",
				PublicationStatus: "published", ModerationStatus: "clear", DeletionStatus: "active"}
			if err := repo.Create(context.Background(), thread); err != nil {
				t.Fatal(err)
			}
			switch test.name {
			case "private":
				repo.change = "private"
			case "read-fault":
				repo.readError = errors.New("database failure secret-diagnostic-body")
			case "recheck-fault":
				repo.finalError = errors.New("query failure secret-diagnostic-body")
			case "busy":
				repo.change = "busy"
			}
			router := gin.New()
			router.GET("/threads/:id", NewThreadHandler(service.NewThreadService(repo, nil)).GetThread)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/threads/99101?author_id=99102&publication_status=published&facts_version=1", nil))
			if rec.Code != test.status {
				t.Fatalf("status: got %d want %d: %s", rec.Code, test.status, rec.Body.String())
			}
			body := rec.Body.String()
			for _, hidden := range []string{"secret-changed-body", "secret-diagnostic-body", "facts_version"} {
				if strings.Contains(body, hidden) {
					t.Fatalf("response exposed %s: %s", hidden, body)
				}
			}
			if test.status != 200 && strings.Contains(body, "public-body") {
				t.Fatal("denied response contains content")
			}
			stored, err := repo.MemoryThreadRepository.GetByID(context.Background(), thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantViews := int64(0)
			if test.status == 200 {
				wantViews = 1
			}
			if stored.ViewCount != wantViews {
				t.Fatalf("view count got %d want %d", stored.ViewCount, wantViews)
			}
		})
	}
}
