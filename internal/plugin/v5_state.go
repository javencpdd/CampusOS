package plugin

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"sync"
)

type V5Coordinate struct {
	Publisher string `json:"publisher"`
	Key       string `json:"plugin_key"`
	Version   string `json:"version"`
}

func (c V5Coordinate) family() string                      { return c.Publisher + "/" + c.Key }
func (c V5Coordinate) id() string                          { return c.family() + "@" + c.Version }
func (c V5Coordinate) valid() bool                         { return v5FamilyValid(c.Publisher, c.Key) && v5Version(c.Version) }
func v5StateID(c V5Coordinate, owner string) string        { return c.family() + "#" + v5OwnerName(owner) }
func v5VersionOwnerID(c V5Coordinate, owner string) string { return c.id() + "#" + v5OwnerName(owner) }

type V5PackageRelease struct {
	V5Coordinate
	Digest   string          `json:"package_digest"`
	Size     int             `json:"size_bytes"`
	Manifest json.RawMessage `json:"manifest"`
	Release  json.RawMessage `json:"release"`
	Storage  string          `json:"storage"`
	Revision int64           `json:"revision,string"`
}
type V5Installation struct {
	Publisher          string `json:"publisher"`
	Key                string `json:"plugin_key"`
	Owner              string `json:"owner_user_id"`
	Version            string `json:"version"`
	DesiredVersion     string `json:"desired_version"`
	Installed          bool   `json:"installed"`
	Desired            string `json:"desired"`
	Observed           string `json:"observed"`
	Generation         int64  `json:"generation,string"`
	Revision           int64  `json:"revision,string"`
	ObservedGeneration int64  `json:"observed_generation,string"`
	ActiveGeneration   int64  `json:"active_generation,string"`
	Error              string `json:"error_code"`
	ConfigDigest       string `json:"config_digest"`
}

func (s V5Installation) coordinate() V5Coordinate { return V5Coordinate{s.Publisher, s.Key, s.Version} }

type V5Grant struct {
	V5Coordinate
	Approved  bool                       `json:"approved"`
	Consumes  []PluginV5Consume          `json:"consumes"`
	Resources *PluginV5ManifestResources `json:"host_resources,omitempty"`
	Revision  int64                      `json:"revision,string"`
}
type V5Consent struct {
	V5Coordinate
	Owner         string `json:"owner_user_id"`
	Accepted      bool   `json:"accepted"`
	PurposeHash   string `json:"purpose_hash"`
	GrantRevision int64  `json:"grant_revision,string"`
	Revision      int64  `json:"revision,string"`
}
type V5Configuration struct {
	V5Coordinate
	Owner      string            `json:"owner_user_id"`
	Values     json.RawMessage   `json:"values"`
	SecretRefs map[string]string `json:"secret_refs"`
	Revision   int64             `json:"revision,string"`
}
type V5Operation struct {
	V5Coordinate
	ID              string         `json:"id"`
	ActorKind       string         `json:"actor_kind"`
	ActorID         string         `json:"actor_id"`
	Owner           string         `json:"owner_user_id"`
	IdempotencyKey  string         `json:"idempotency_key"`
	RequestHash     string         `json:"request_hash"`
	Kind            string         `json:"kind"`
	Generation      int64          `json:"generation,string"`
	Status          string         `json:"status"`
	Error           string         `json:"error_code"`
	Purge           bool           `json:"purge_data"`
	Revision        int64          `json:"revision,string"`
	Lease           int64          `json:"lease_generation,string"`
	LeaseUntil      int64          `json:"lease_until_ms"`
	Attempts        int            `json:"attempts"`
	StorageRevision int64          `json:"storage_revision,string"`
	Preparation     *V5Preparation `json:"preparation,omitempty"`
	CleanupAfter    string         `json:"cleanup_after"`
}
type V5Preparation struct {
	GlobalGeneration     int64  `json:"global_generation,string"`
	GrantRevision        int64  `json:"grant_revision,string"`
	SystemConfigRevision int64  `json:"system_config_revision,string"`
	UserConfigRevision   int64  `json:"user_config_revision,string"`
	PackageDigest        string `json:"package_digest"`
	ConfigDigest         string `json:"config_digest"`
}

// Finite record kinds select physically separate tables. Their identity,
// generation and relationship columns are generated from these DTOs in SQL,
// so there is one state fact, with database CHECK/PK/FK/immutability guards.
type v5RecordKind string

const (
	v5Release   v5RecordKind = "plugin_package_releases"
	v5Global    v5RecordKind = "plugin_installations"
	v5User      v5RecordKind = "plugin_user_installations"
	v5Grant     v5RecordKind = "plugin_v5_admin_grants"
	v5Consent   v5RecordKind = "plugin_v5_user_consents"
	v5Config    v5RecordKind = "plugin_v5_configurations"
	v5Operation v5RecordKind = "plugin_operations"
)

var v5RecordKinds = []v5RecordKind{v5Release, v5Global, v5User, v5Grant, v5Consent, v5Config, v5Operation}

type v5Query struct {
	After, Owner   string
	Publisher, Key string
	Limit          int
	Pending        bool
	Now            int64
	Visible        bool
}
type V5StateStore interface {
	lock(context.Context, string) error
	next(context.Context) (int64, error)
	load(context.Context, v5RecordKind, string, any) error
	save(context.Context, v5RecordKind, string, any) error
	list(context.Context, v5RecordKind, v5Query) ([]json.RawMessage, error)
	operation(context.Context, string, string, string) (V5Operation, error)
	referenced(context.Context, V5Coordinate) (bool, error)
}

func v5KindValid(k v5RecordKind) bool {
	for _, v := range v5RecordKinds {
		if v == k {
			return true
		}
	}
	return false
}
func v5RecordID(k v5RecordKind, b []byte) string {
	var x struct {
		Publisher string `json:"publisher"`
		Key       string `json:"plugin_key"`
		Version   string `json:"version"`
		Owner     string `json:"owner_user_id"`
		ID        string `json:"id"`
	}
	if json.Unmarshal(b, &x) != nil {
		return ""
	}
	c := V5Coordinate{x.Publisher, x.Key, x.Version}
	switch k {
	case v5Release, v5Grant:
		return c.id()
	case v5Global, v5User:
		return v5StateID(c, x.Owner)
	case v5Consent, v5Config:
		return v5VersionOwnerID(c, x.Owner)
	case v5Operation:
		return x.ID
	}
	return ""
}

type MemoryV5StateStore struct {
	mu       sync.Mutex
	sequence int64
	records  map[v5RecordKind]map[string]json.RawMessage
}

func NewMemoryV5StateStore() *MemoryV5StateStore {
	s := &MemoryV5StateStore{records: map[v5RecordKind]map[string]json.RawMessage{}}
	for _, k := range v5RecordKinds {
		s.records[k] = map[string]json.RawMessage{}
	}
	return s
}
func (s *MemoryV5StateStore) lock(ctx context.Context, key string) error { return ctx.Err() }
func (s *MemoryV5StateStore) next(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return 0, ErrV5PlatformUnavailable
	}
	s.sequence++
	return s.sequence, nil
}
func (s *MemoryV5StateStore) load(ctx context.Context, k v5RecordKind, id string, out any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return ErrV5PlatformUnavailable
	}
	b, ok := s.records[k][id]
	if !ok {
		return ErrV5NotFound
	}
	if json.Unmarshal(b, out) != nil {
		return ErrV5PlatformUnavailable
	}
	return nil
}
func (s *MemoryV5StateStore) save(ctx context.Context, k v5RecordKind, id string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(v)
	if ctx.Err() != nil || err != nil || !v5KindValid(k) || id != v5RecordID(k, b) {
		return ErrV5Invalid
	}
	if k == v5Release {
		if old, ok := s.records[k][id]; ok {
			var a, z V5PackageRelease
			json.Unmarshal(old, &a)
			json.Unmarshal(b, &z)
			if a.Digest != z.Digest || a.Size != z.Size || string(a.Manifest) != string(z.Manifest) {
				return ErrPluginV5PackageConflict
			}
		}
	}
	s.records[k][id] = b
	return nil
}
func (s *MemoryV5StateStore) list(ctx context.Context, k v5RecordKind, q v5Query) ([]json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil || !v5KindValid(k) {
		return nil, ErrV5PlatformUnavailable
	}
	limit := q.Limit
	if limit < 1 || limit > 51 {
		limit = 51
	}
	ids := []string{}
	for id, b := range s.records[k] {
		if id <= q.After {
			continue
		}
		var v struct {
			Owner     string `json:"owner_user_id"`
			Publisher string `json:"publisher"`
			Key       string `json:"plugin_key"`
			Status    string `json:"status"`
			Lease     int64  `json:"lease_until_ms"`
			Actor     string `json:"actor_id"`
		}
		json.Unmarshal(b, &v)
		if q.Publisher != "" && (v.Publisher != q.Publisher || v.Key != q.Key) {
			continue
		}
		if q.Owner != "" && v.Owner != q.Owner {
			continue
		}
		if q.Pending && (v.Status != "queued" && v.Status != "working" && v.Status != "waiting" || v.Lease > q.Now) {
			continue
		}
		if q.Visible {
			var i V5Installation
			json.Unmarshal(b, &i)
			var g V5Grant
			json.Unmarshal(s.records[v5Grant][i.coordinate().id()], &g)
			if !i.Installed || !g.Approved {
				continue
			}
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if q.Pending {
		sort.Slice(ids, func(i, j int) bool {
			var a, b V5Operation
			json.Unmarshal(s.records[k][ids[i]], &a)
			json.Unmarshal(s.records[k][ids[j]], &b)
			rank := func(status string) int {
				switch status {
				case "queued":
					return 0
				case "working":
					return 1
				default:
					return 2
				}
			}
			if rank(a.Status) != rank(b.Status) {
				return rank(a.Status) < rank(b.Status)
			}
			if a.LeaseUntil != b.LeaseUntil {
				return a.LeaseUntil < b.LeaseUntil
			}
			return a.ID < b.ID
		})
	}
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := []json.RawMessage{}
	for _, id := range ids {
		out = append(out, append(json.RawMessage(nil), s.records[k][id]...))
	}
	return out, nil
}
func (s *MemoryV5StateStore) Snapshot() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.records)
	return b
}
func (s *MemoryV5StateStore) operation(ctx context.Context, kind, actor, key string) (V5Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.records[v5Operation] {
		var op V5Operation
		json.Unmarshal(b, &op)
		if op.ActorKind == kind && op.ActorID == actor && op.IdempotencyKey == key {
			return op, nil
		}
	}
	return V5Operation{}, ErrV5NotFound
}
func (s *MemoryV5StateStore) referenced(ctx context.Context, c V5Coordinate) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range []v5RecordKind{v5Global, v5User} {
		for _, b := range s.records[k] {
			var i V5Installation
			json.Unmarshal(b, &i)
			if i.Publisher == c.Publisher && i.Key == c.Key && i.Installed && (i.Version == c.Version || i.DesiredVersion == c.Version) {
				return true, nil
			}
		}
	}
	return false, nil
}
func (s *MemoryV5StateStore) Restore(v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	json.Unmarshal(v.([]byte), &s.records)
}

func v5Counter(v int64) string { return strconv.FormatInt(v, 10) }
