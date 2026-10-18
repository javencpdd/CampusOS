package plugin

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
)

type V5EventDelivery struct {
	V5Coordinate
	ID         string                  `json:"id"`
	Source     string                  `json:"source_event_id"`
	Instance   string                  `json:"instance_id"`
	Owner      string                  `json:"owner_user_id"`
	Generation int64                   `json:"instance_generation,string"`
	Event      campusossdk.V5SafeEvent `json:"event"`
	Status     string                  `json:"status"`
	Attempts   int                     `json:"attempts"`
	Lease      int64                   `json:"lease_generation,string"`
	LeaseUntil int64                   `json:"lease_until_ms"`
	NextAt     int64                   `json:"next_at_ms"`
	Error      string                  `json:"error_code"`
}
type V5EventStore interface {
	Enqueue(context.Context, V5EventDelivery) error
	Claim(context.Context, int64, int) ([]V5EventDelivery, error)
	Finish(context.Context, V5EventDelivery, int64, string, string, int64) error
}

var v5EventIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var v5SourceIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

func validV5Delivery(d V5EventDelivery) bool {
	return d.V5Coordinate.valid() && v5EventIDPattern.MatchString(d.ID) && v5EventIDPattern.MatchString(d.Instance) && v5SourceIDPattern.MatchString(d.Source) && d.Event.EventID == d.Source && v5OwnerValid(d.Owner) && d.Generation > 0 && validV5SafeEvent(d.Event) && d.Status == "queued" && d.Attempts == 0 && d.Lease == 0 && d.LeaseUntil == 0 && d.NextAt >= 0 && d.Error == ""
}

func validV5EventFinish(status, code string, next int64) bool {
	return (status == "done" || status == "retry" || status == "dead") && len(code) <= 128 && (code == "" || strings.HasPrefix(code, "plugin.event_")) && next >= 0
}

func v5EventClaimLimit(limit int) int {
	if limit < 1 || limit > 50 {
		return 16
	}
	return limit
}

type MemoryV5EventStore struct {
	mu       sync.Mutex
	rows     map[string]V5EventDelivery
	sequence int64
}

func NewMemoryV5EventStore() *MemoryV5EventStore {
	return &MemoryV5EventStore{rows: map[string]V5EventDelivery{}}
}
func (s *MemoryV5EventStore) Enqueue(ctx context.Context, d V5EventDelivery) error {
	if !transaction.Active(ctx) {
		return ErrV5PlatformUnavailable
	}
	if !validV5Delivery(d) {
		return ErrV5Invalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for _, old := range s.rows {
		if old.Source == d.Source && old.Instance == d.Instance && old.Generation == d.Generation {
			return nil
		}
	}
	s.rows[d.ID] = d
	return nil
}
func (s *MemoryV5EventStore) Claim(ctx context.Context, now int64, limit int) ([]V5EventDelivery, error) {
	limit = v5EventClaimLimit(limit)
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	ids := []string{}
	for id, d := range s.rows {
		if (d.Status == "queued" || d.Status == "retry" || d.Status == "working") && d.NextAt <= now && d.LeaseUntil <= now {
			if d.Attempts >= 5 {
				d.Status = "dead"
				d.Error = "plugin.event_dead_letter"
				s.rows[id] = d
			} else {
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := []V5EventDelivery{}
	for _, id := range ids {
		d := s.rows[id]
		s.sequence++
		d.Lease = s.sequence
		d.LeaseUntil = now + 30000
		d.Status = "working"
		d.Attempts++
		s.rows[id] = d
		out = append(out, d)
	}
	return out, nil
}
func (s *MemoryV5EventStore) Finish(ctx context.Context, d V5EventDelivery, now int64, status, code string, next int64) error {
	if !validV5EventFinish(status, code, next) {
		return ErrV5Invalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.rows[d.ID]
	if !ok || old.Lease != d.Lease || old.Status != "working" || old.LeaseUntil <= now {
		return ErrV5Conflict
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	old.Status = status
	old.Error = code
	old.NextAt = next
	old.LeaseUntil = 0
	s.rows[d.ID] = old
	return nil
}
func (s *MemoryV5EventStore) Snapshot() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.rows)
	return b
}
func (s *MemoryV5EventStore) Restore(snapshot any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Unmarshal into a fresh map: decoding {} into a nonnil map retains
	// inserted keys and would let an audit-failed enqueue escape rollback.
	rows := map[string]V5EventDelivery{}
	if json.Unmarshal(snapshot.([]byte), &rows) == nil {
		s.rows = rows
	}
}
