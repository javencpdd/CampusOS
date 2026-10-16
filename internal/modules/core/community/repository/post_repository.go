package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

var ErrPostNotFound = errors.New("post not found")

type PostRepository interface {
	Create(ctx context.Context, post *domain.Post) error
	GetByID(ctx context.Context, id string) (*domain.Post, error)
	ListByThread(ctx context.Context, threadID string, page, pageSize int) ([]*domain.Post, int64, error)
	Update(ctx context.Context, post *domain.Post) error
	Delete(ctx context.Context, id string) error
}

type MemoryPostRepository struct {
	mu              sync.RWMutex
	posts           map[string]*domain.Post
	nextFloorNumber map[string]int
	versionCounter  int64
}

func NewMemoryPostRepository() *MemoryPostRepository {
	return &MemoryPostRepository{
		posts:           make(map[string]*domain.Post),
		nextFloorNumber: make(map[string]int),
	}
}

func (r *MemoryPostRepository) Create(_ context.Context, post *domain.Post) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if post.FloorNumber <= 0 {
		r.nextFloorNumber[post.ThreadID]++
		post.FloorNumber = r.nextFloorNumber[post.ThreadID]
	} else if post.FloorNumber > r.nextFloorNumber[post.ThreadID] {
		r.nextFloorNumber[post.ThreadID] = post.FloorNumber
	}
	version, err := r.nextPostFactsVersionLocked()
	if err != nil {
		return err
	}
	preparePost(post)
	if !validPostActorFacts(post) || !json.Valid(post.Metadata) {
		return ErrPostFactsUnavailable
	}
	post.FactsVersion = version
	r.posts[post.ID] = clonePost(post)
	return nil
}

func (r *MemoryPostRepository) GetByID(_ context.Context, id string) (*domain.Post, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.posts[id]
	if !ok || p.DeletedAt != nil {
		return nil, ErrPostNotFound
	}
	return clonePost(p), nil
}

func (r *MemoryPostRepository) Update(_ context.Context, post *domain.Post) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.posts[post.ID]
	if !ok || current.DeletedAt != nil {
		return ErrPostNotFound
	}
	candidate := clonePost(post)
	preparePost(candidate)
	if !validPostActorFacts(candidate) || !json.Valid(candidate.Metadata) {
		return ErrPostFactsUnavailable
	}
	candidate.FactsVersion = current.FactsVersion
	if !samePostFacts(current, candidate) {
		version, err := r.nextPostFactsVersionLocked()
		if err != nil {
			return err
		}
		candidate.FactsVersion = version
	}
	post.FactsVersion = candidate.FactsVersion
	r.posts[post.ID] = candidate
	return nil
}

func (r *MemoryPostRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.posts[id]
	if !ok || current.DeletedAt != nil {
		return ErrPostNotFound
	}
	version, err := r.nextPostFactsVersionLocked()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	candidate := clonePost(current)
	candidate.DeletedAt = &now
	candidate.FactsVersion = version
	r.posts[id] = candidate
	return nil
}

func (r *MemoryPostRepository) ListByThread(_ context.Context, threadID string, page, pageSize int) ([]*domain.Post, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var filtered []*domain.Post
	for _, p := range r.posts {
		if p.ThreadID == threadID && p.DeletedAt == nil {
			filtered = append(filtered, clonePost(p))
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].FloorNumber == filtered[j].FloorNumber {
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		}
		return filtered[i].FloorNumber < filtered[j].FloorNumber
	})
	total := int64(len(filtered))
	start := (page - 1) * pageSize
	if start >= len(filtered) {
		return []*domain.Post{}, total, nil
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[start:end], total, nil
}

func (r *MemoryPostRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := memoryPostState{Posts: map[string]memoryPostRow{}, Next: r.nextFloorNumber}
	for id, p := range r.posts {
		state.Posts[id] = memoryPostRow{Post: clonePost(p), Version: p.FactsVersion, Metadata: append(json.RawMessage(nil), p.Metadata...), DeletedAt: p.DeletedAt, ActorKind: p.DeletedActorKind, ActorID: p.DeletedActorID}
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return []byte(nil)
	}
	return raw
}
func (r *MemoryPostRepository) Restore(value any) {
	raw, ok := value.([]byte)
	if !ok || len(raw) == 0 {
		return
	}
	var state memoryPostState
	if json.Unmarshal(raw, &state) != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.posts = map[string]*domain.Post{}
	r.nextFloorNumber = state.Next
	if r.nextFloorNumber == nil {
		r.nextFloorNumber = map[string]int{}
	}
	for id, v := range state.Posts {
		if v.Post == nil {
			continue
		}
		p := clonePost(v.Post)
		p.FactsVersion = v.Version
		p.Metadata = append(json.RawMessage(nil), v.Metadata...)
		p.DeletedAt = v.DeletedAt
		p.DeletedActorKind = v.ActorKind
		p.DeletedActorID = v.ActorID
		r.posts[id] = p
		if p.FactsVersion > r.versionCounter {
			r.versionCounter = p.FactsVersion
		}
	}
}
