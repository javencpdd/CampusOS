// Package webcompanion provides short-lived, session-bound public resource jobs.
// It stores no durable credentials or content and never fetches a URL itself.
package webcompanion

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

var ErrUnavailable = errors.New("web companion unavailable")
var ErrDenied = errors.New("web companion denied")

type Pair struct {
	Code      string `json:"pairing_code"`
	ExpiresAt int64  `json:"expires_at"`
}
type Reader struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Online    bool   `json:"online"`
	ExpiresAt int64  `json:"expires_at"`
	ProxyMode string `json:"proxy_mode"`
}
type Job struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	MaxBytes int64  `json:"max_bytes"`
}
type pairing struct {
	owner   string
	expires time.Time
	alive   func(context.Context) bool
}
type reader struct {
	Reader
	owner         string
	token         [32]byte
	expires, seen time.Time
	alive         func(context.Context) bool
	queue         chan *pending
	closed        chan struct{}
	polling       bool
}
type result struct {
	bytes []byte
	err   error
}
type pending struct {
	Job
	reader               *reader
	ctx                  context.Context
	authorize            func(context.Context) bool
	result               chan result
	delivered, completed bool
}
type Hub struct {
	mu      sync.Mutex
	now     func() time.Time
	pairs   map[[32]byte]pairing
	readers map[string]*reader
	jobs    map[string]*pending
}

func NewHub() *Hub {
	return &Hub{now: time.Now, pairs: map[[32]byte]pairing{}, readers: map[string]*reader{}, jobs: map[string]*pending{}}
}
func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
func (h *Hub) remove(r *reader) {
	if h.readers[r.ID] == r {
		delete(h.readers, r.ID)
		close(r.closed)
	}
}
func (h *Hub) sweep() {
	for code, p := range h.pairs {
		if !h.now().Before(p.expires) {
			delete(h.pairs, code)
		}
	}
	for _, r := range h.readers {
		if !h.now().Before(r.expires) {
			h.remove(r)
		}
	}
}
func (h *Hub) Begin(owner string, alive func(context.Context) bool) (Pair, error) {
	if owner == "" || alive == nil {
		return Pair{}, ErrDenied
	}
	code := random(16)
	if code == "" {
		return Pair{}, ErrUnavailable
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweep()
	for key, p := range h.pairs {
		if p.owner == owner {
			delete(h.pairs, key)
		}
	}
	if len(h.pairs) >= 64 {
		return Pair{}, ErrUnavailable
	}
	expires := h.now().Add(2 * time.Minute)
	h.pairs[sha256.Sum256([]byte(code))] = pairing{owner, expires, alive}
	return Pair{code, expires.UnixMilli()}, nil
}
func (h *Hub) Connect(ctx context.Context, code, name string) (string, Reader, error) {
	return h.ConnectWithMode(ctx, code, name, "direct")
}
func (h *Hub) ConnectWithMode(ctx context.Context, code, name, mode string) (string, Reader, error) {
	if (mode != "direct" && mode != "http" && mode != "https" && mode != "socks5") || len(code) != 32 || len(name) == 0 || len(name) > 80 {
		return "", Reader{}, ErrDenied
	}
	h.mu.Lock()
	h.sweep()
	key := sha256.Sum256([]byte(code))
	p, ok := h.pairs[key]
	delete(h.pairs, key)
	h.mu.Unlock()
	if !ok || !p.alive(ctx) {
		return "", Reader{}, ErrDenied
	}
	token, id := random(32), random(16)
	if token == "" || id == "" {
		return "", Reader{}, ErrUnavailable
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweep()
	for _, r := range h.readers {
		if r.owner == p.owner {
			h.remove(r)
		}
	}
	if len(h.readers) >= 64 {
		return "", Reader{}, ErrUnavailable
	}
	r := &reader{Reader: Reader{ID: id, Name: name, ProxyMode: mode, Online: true, ExpiresAt: h.now().Add(2 * time.Hour).UnixMilli()}, owner: p.owner, token: sha256.Sum256([]byte(token)), expires: h.now().Add(2 * time.Hour), seen: h.now(), alive: p.alive, queue: make(chan *pending, 2), closed: make(chan struct{})}
	h.readers[id] = r
	return token, r.Reader, nil
}
func (h *Hub) List(owner string) []Reader {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweep()
	out := []Reader{}
	for _, r := range h.readers {
		if r.owner == owner {
			view := r.Reader
			view.Online = h.now().Sub(r.seen) < 30*time.Second
			out = append(out, view)
		}
	}
	return out
}
func (h *Hub) Disconnect(owner, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.readers[id]
	if r == nil || r.owner != owner {
		return ErrDenied
	}
	h.remove(r)
	return nil
}
func (h *Hub) authenticated(ctx context.Context, token string) (*reader, error) {
	if len(token) != 64 {
		return nil, ErrDenied
	}
	hash := sha256.Sum256([]byte(token))
	h.mu.Lock()
	h.sweep()
	var found *reader
	for _, r := range h.readers {
		if subtle.ConstantTimeCompare(r.token[:], hash[:]) == 1 {
			found = r
			break
		}
	}
	h.mu.Unlock()
	if found == nil {
		return nil, ErrDenied
	}
	if !found.alive(ctx) {
		h.mu.Lock()
		h.remove(found)
		h.mu.Unlock()
		return nil, ErrDenied
	}
	return found, nil
}
func (h *Hub) Poll(ctx context.Context, token string) (*Job, error) {
	r, err := h.authenticated(ctx, token)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	if r.polling {
		h.mu.Unlock()
		return nil, ErrUnavailable
	}
	r.polling = true
	r.seen = h.now()
	h.mu.Unlock()
	defer func() { h.mu.Lock(); r.polling = false; h.mu.Unlock() }()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ErrUnavailable
		case <-r.closed:
			return nil, ErrDenied
		case <-timer.C:
			return nil, nil
		case job := <-r.queue:
			if job.ctx.Err() != nil || !job.authorize(job.ctx) {
				select {
				case job.result <- result{err: ErrDenied}:
				default:
				}
				continue
			}
			h.mu.Lock()
			valid := h.jobs[job.ID] == job && h.readers[r.ID] == r && job.ctx.Err() == nil
			if valid {
				job.delivered = true
			}
			h.mu.Unlock()
			if valid {
				copy := job.Job
				return &copy, nil
			}
		}
	}
}
func (h *Hub) Validate(ctx context.Context, token string) error {
	_, err := h.authenticated(ctx, token)
	return err
}
func (h *Hub) Complete(ctx context.Context, token, id string, body []byte, failed bool) error {
	r, err := h.authenticated(ctx, token)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	job := h.jobs[id]
	if job == nil || job.reader != r || !job.delivered || job.completed || job.ctx.Err() != nil || int64(len(body)) > job.MaxBytes || failed && len(body) != 0 {
		return ErrDenied
	}
	job.completed = true
	value := result{bytes: append([]byte(nil), body...)}
	if failed {
		value.err = ErrUnavailable
	}
	job.result <- value
	return nil
}
func (h *Hub) Fetch(ctx context.Context, owner, id, target string, maxBytes int64, authorize func(context.Context) bool) ([]byte, error) {
	if maxBytes < 1 || maxBytes > 1<<20 || authorize == nil || !authorize(ctx) {
		return nil, ErrDenied
	}
	jobID := random(16)
	if jobID == "" {
		return nil, ErrUnavailable
	}
	h.mu.Lock()
	h.sweep()
	r := h.readers[id]
	if r == nil {
		h.mu.Unlock()
		return nil, ErrUnavailable
	}
	if r.owner != owner {
		h.mu.Unlock()
		return nil, ErrDenied
	}
	if h.now().Sub(r.seen) >= 30*time.Second || len(h.jobs) >= 16 {
		h.mu.Unlock()
		return nil, ErrUnavailable
	}
	job := &pending{Job: Job{jobID, target, maxBytes}, reader: r, ctx: ctx, authorize: authorize, result: make(chan result, 1)}
	h.jobs[jobID] = job
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.jobs, jobID); h.mu.Unlock() }()
	select {
	case r.queue <- job:
	case <-ctx.Done():
		return nil, ErrUnavailable
	case <-r.closed:
		return nil, ErrDenied
	default:
		return nil, ErrUnavailable
	}
	select {
	case <-ctx.Done():
		return nil, ErrUnavailable
	case <-r.closed:
		return nil, ErrDenied
	case value := <-job.result:
		if !authorize(ctx) {
			return nil, ErrDenied
		}
		return value.bytes, value.err
	}
}
