package knowledge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

var ErrRAGConsent = errors.New("knowledge.rag_consent_required")

const (
	ragApprovalTTL           = 2 * time.Minute
	ragApprovalPerOwnerLimit = 32
	ragApprovalGlobalLimit   = 256
)

// ragApproval holds only opaque identity/review hashes. It is not an authority
// for reading a source or dispatching a model: callers must also recheck their
// current identity, source, model and exact reviewed payload at each boundary.
type ragApproval struct {
	owner, review [32]byte
	expires       time.Time
	used          bool
	cancel        context.CancelFunc
}

type ragApprovalLedger struct {
	mu      sync.Mutex
	entries map[string]ragApproval
	stopped bool
	now     func() time.Time
}

func newRAGApprovalLedger() *ragApprovalLedger {
	return &ragApprovalLedger{entries: make(map[string]ragApproval), now: time.Now}
}

func ragApprovalDigest(value string) ([32]byte, bool) {
	var out [32]byte
	if len(value) != 64 {
		return out, false
	}
	raw, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(raw) != value {
		return out, false
	}
	copy(out[:], raw)
	return out, true
}

func validRAGApprovalID(id string) bool {
	if len(id) != 35 || id[:3] != "kr_" {
		return false
	}
	raw, err := hex.DecodeString(id[3:])
	return err == nil && hex.EncodeToString(raw) == id[3:]
}

// pruneLocked is bounded by the global inventory limit. Valid records are never
// evicted to make room for another request, including another owner's request.
func (l *ragApprovalLedger) pruneLocked(now time.Time) []context.CancelFunc {
	var cancels []context.CancelFunc
	for id, entry := range l.entries {
		if !now.Before(entry.expires) {
			delete(l.entries, id)
			if entry.cancel != nil {
				cancels = append(cancels, entry.cancel)
			}
		}
	}
	return cancels
}

func cancelRAGApprovals(cancels []context.CancelFunc) {
	for _, cancel := range cancels {
		cancel()
	}
}

func (l *ragApprovalLedger) prepare(ownerDigest, reviewDigest string) (string, time.Time, error) {
	owner, ownerOK := ragApprovalDigest(ownerDigest)
	review, reviewOK := ragApprovalDigest(reviewDigest)
	if l == nil || !ownerOK || !reviewOK {
		return "", time.Time{}, ErrRAGConsent
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", time.Time{}, ErrRAGConsent
	}
	id := "kr_" + hex.EncodeToString(random[:])
	now := l.now()
	l.mu.Lock()
	cancels := l.pruneLocked(now)
	defer func() {
		l.mu.Unlock()
		cancelRAGApprovals(cancels)
	}()
	if l.stopped || len(l.entries) >= ragApprovalGlobalLimit {
		return "", time.Time{}, ErrRAGConsent
	}
	count := 0
	for _, entry := range l.entries {
		if entry.owner == owner {
			count++
		}
	}
	if count >= ragApprovalPerOwnerLimit {
		return "", time.Time{}, ErrRAGConsent
	}
	if _, exists := l.entries[id]; exists {
		return "", time.Time{}, ErrRAGConsent
	}
	expires := now.Add(ragApprovalTTL)
	l.entries[id] = ragApproval{owner: owner, review: review, expires: expires}
	return id, expires, nil
}

func (l *ragApprovalLedger) consume(id, ownerDigest, reviewDigest string, cancel context.CancelFunc) error {
	owner, ownerOK := ragApprovalDigest(ownerDigest)
	review, reviewOK := ragApprovalDigest(reviewDigest)
	if l == nil || !validRAGApprovalID(id) || !ownerOK || !reviewOK || cancel == nil {
		return ErrRAGConsent
	}
	now := l.now()
	l.mu.Lock()
	cancels := l.pruneLocked(now)
	defer func() {
		l.mu.Unlock()
		cancelRAGApprovals(cancels)
	}()
	entry, exists := l.entries[id]
	if l.stopped || !exists || entry.used || entry.owner != owner || entry.review != review {
		return ErrRAGConsent
	}
	entry.used, entry.cancel = true, cancel
	l.entries[id] = entry
	return nil
}

// current permits only the one consumed invocation, never a prepared review.
func (l *ragApprovalLedger) current(id, ownerDigest, reviewDigest string) error {
	owner, ownerOK := ragApprovalDigest(ownerDigest)
	review, reviewOK := ragApprovalDigest(reviewDigest)
	if l == nil || !validRAGApprovalID(id) || !ownerOK || !reviewOK {
		return ErrRAGConsent
	}
	now := l.now()
	l.mu.Lock()
	cancels := l.pruneLocked(now)
	defer func() {
		l.mu.Unlock()
		cancelRAGApprovals(cancels)
	}()
	entry, exists := l.entries[id]
	if l.stopped || !exists || !entry.used || entry.owner != owner || entry.review != review {
		return ErrRAGConsent
	}
	return nil
}

func (l *ragApprovalLedger) revoke(id, ownerDigest string) error {
	owner, ownerOK := ragApprovalDigest(ownerDigest)
	if l == nil || !validRAGApprovalID(id) || !ownerOK {
		return ErrRAGConsent
	}
	now := l.now()
	l.mu.Lock()
	cancels := l.pruneLocked(now)
	defer func() {
		l.mu.Unlock()
		cancelRAGApprovals(cancels)
	}()
	entry, exists := l.entries[id]
	if l.stopped || !exists || entry.owner != owner {
		return ErrRAGConsent
	}
	delete(l.entries, id)
	if entry.cancel != nil {
		cancels = append(cancels, entry.cancel)
	}
	return nil
}

// finish removes a completed invocation; successful completion is not a
// revocation and does not itself cancel the caller's context.
func (l *ragApprovalLedger) finish(id string) {
	if l == nil || !validRAGApprovalID(id) {
		return
	}
	l.mu.Lock()
	delete(l.entries, id)
	l.mu.Unlock()
}

func (l *ragApprovalLedger) clear(stopped bool) {
	if l == nil {
		return
	}
	l.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(l.entries))
	for _, entry := range l.entries {
		if entry.cancel != nil {
			cancels = append(cancels, entry.cancel)
		}
	}
	l.entries = make(map[string]ragApproval)
	l.stopped = stopped
	l.mu.Unlock()
	cancelRAGApprovals(cancels)
}

func (l *ragApprovalLedger) reset() { l.clear(false) }
func (l *ragApprovalLedger) stop()  { l.clear(true) }
