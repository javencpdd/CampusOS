package repository

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
)

var ErrSessionNotFound = errors.New("identity session not found")

// SessionRepository owns only server-side session state. It never receives a
// raw refresh token, which keeps accidental logging and persistence leaks out
// of the repository boundary.
type SessionRepository interface {
	Create(context.Context, *domain.Session) error
	GetByID(context.Context, string) (*domain.Session, error)
	GetByIDForUpdate(context.Context, string) (*domain.Session, error)
	GetByRefreshDigestForUpdate(context.Context, string) (*domain.Session, error)
	Update(context.Context, *domain.Session) error
	ListByUser(context.Context, string) ([]*domain.Session, error)
	RevokeFamily(context.Context, string, string, time.Time) (int64, error)
	RevokeByUser(context.Context, string, string, time.Time) (int64, error)
}

type MemorySessionRepository struct {
	mu       sync.RWMutex
	sessions map[string]*domain.Session
	digests  map[string]string
}

func NewMemorySessionRepository() *MemorySessionRepository {
	return &MemorySessionRepository{sessions: make(map[string]*domain.Session), digests: make(map[string]string)}
}

func (r *MemorySessionRepository) Create(_ context.Context, session *domain.Session) error {
	if session == nil || session.ID == "" || session.UserID == "" || session.RefreshTokenDigest == "" || session.TokenFamilyID == "" {
		return errors.New("identity session requires id, user, refresh digest and family")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sessions[session.ID]; exists {
		return errors.New("identity session id already exists")
	}
	if _, exists := r.digests[session.RefreshTokenDigest]; exists {
		return errors.New("identity session refresh digest already exists")
	}
	r.sessions[session.ID] = cloneSession(session)
	r.digests[session.RefreshTokenDigest] = session.ID
	return nil
}

func (r *MemorySessionRepository) GetByID(_ context.Context, id string) (*domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, ok := r.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return cloneSession(session), nil
}

func (r *MemorySessionRepository) GetByIDForUpdate(ctx context.Context, id string) (*domain.Session, error) {
	return r.GetByID(ctx, id)
}

func (r *MemorySessionRepository) GetByRefreshDigestForUpdate(_ context.Context, digest string) (*domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.digests[digest]
	if !ok {
		return nil, ErrSessionNotFound
	}
	session, ok := r.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return cloneSession(session), nil
}

func (r *MemorySessionRepository) Update(_ context.Context, session *domain.Session) error {
	if session == nil || session.ID == "" {
		return ErrSessionNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.sessions[session.ID]
	if !ok {
		return ErrSessionNotFound
	}
	if current.RefreshTokenDigest != session.RefreshTokenDigest {
		delete(r.digests, current.RefreshTokenDigest)
		if owner, exists := r.digests[session.RefreshTokenDigest]; exists && owner != session.ID {
			return errors.New("identity session refresh digest already exists")
		}
		r.digests[session.RefreshTokenDigest] = session.ID
	}
	r.sessions[session.ID] = cloneSession(session)
	return nil
}

func (r *MemorySessionRepository) ListByUser(_ context.Context, userID string) ([]*domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]*domain.Session, 0)
	for _, session := range r.sessions {
		if session.UserID == userID {
			items = append(items, cloneSession(session))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].LastActiveAt.After(items[j].LastActiveAt) })
	return items, nil
}

func (r *MemorySessionRepository) RevokeFamily(_ context.Context, familyID, reason string, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for _, session := range r.sessions {
		if session.TokenFamilyID != familyID || session.RevokedAt != nil {
			continue
		}
		stamp := now.UTC()
		session.RevokedAt = &stamp
		session.RevokeReason = reason
		session.UpdatedAt = stamp
		count++
	}
	return count, nil
}

func (r *MemorySessionRepository) RevokeByUser(_ context.Context, userID, reason string, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for _, session := range r.sessions {
		if session.UserID != userID || session.RevokedAt != nil {
			continue
		}
		stamp := now.UTC()
		session.RevokedAt = &stamp
		session.RevokeReason = reason
		session.UpdatedAt = stamp
		count++
	}
	return count, nil
}

func (r *MemorySessionRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := memorySessionSnapshot{sessions: make(map[string]*domain.Session, len(r.sessions)), digests: make(map[string]string, len(r.digests))}
	for id, session := range r.sessions {
		state.sessions[id] = cloneSession(session)
	}
	for digest, id := range r.digests {
		state.digests[digest] = id
	}
	return state
}

func (r *MemorySessionRepository) Restore(value any) {
	state, ok := value.(memorySessionSnapshot)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions = make(map[string]*domain.Session, len(state.sessions))
	for id, session := range state.sessions {
		r.sessions[id] = cloneSession(session)
	}
	r.digests = make(map[string]string, len(state.digests))
	for digest, id := range state.digests {
		r.digests[digest] = id
	}
}

type memorySessionSnapshot struct {
	sessions map[string]*domain.Session
	digests  map[string]string
}

func cloneSession(session *domain.Session) *domain.Session {
	if session == nil {
		return nil
	}
	copy := *session
	if session.RevokedAt != nil {
		stamp := *session.RevokedAt
		copy.RevokedAt = &stamp
	}
	return &copy
}

var _ SessionRepository = (*MemorySessionRepository)(nil)
