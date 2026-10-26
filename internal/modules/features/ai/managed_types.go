package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"reflect"
	"sync"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/jackc/pgx/v5/pgxpool"
)

const PortManagedChat = "ai.managed-chat/v1"
const ManagedContractVersion = "campusos.ai-managed/v1"
const ManagedEventsVersion = "campusos.ai-events/v1"

var (
	ErrManagedMFA         = errors.New("ai.mfa_required")
	ErrManagedUnavailable = errors.New("ai.managed_unavailable")
	ErrManagedDenied      = errors.New("ai.managed_denied")
	ErrManagedConflict    = errors.New("ai.managed_conflict")
	ErrManagedBudget      = errors.New("ai.managed_budget")
	ErrManagedReplay      = errors.New("ai.managed_replay")
)

func managedID(prefix string) string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return ""
	}
	return prefix + hex.EncodeToString(b[:])
}
func managedIDValid(prefix, id string) bool {
	if len(id) != len(prefix)+32 || id[:len(prefix)] != prefix {
		return false
	}
	b, e := hex.DecodeString(id[len(prefix):])
	return e == nil && hex.EncodeToString(b) == id[len(prefix):]
}

type ManagedProfile struct {
	ID            string `json:"id"`
	Revision      int64  `json:"revision,string"`
	Definition    string `json:"definition"`
	Model         string `json:"model"`
	Enabled       bool   `json:"enabled"`
	TimeoutMS     int64  `json:"timeout_ms"`
	MaxTokens     int    `json:"max_tokens"`
	MaxConcurrent int    `json:"max_concurrent"`
	Endpoint      string `json:"-"`
	Fingerprint   string `json:"-"`
	SecretRef     string `json:"-"`
}
type ManagedBinding struct {
	Name      string `json:"name"`
	ProfileID string `json:"profile_id"`
	Revision  int64  `json:"revision,string"`
}
type ManagedBudget struct {
	ProfileID string `json:"profile_id"`
	Quota     int64  `json:"quota"`
	Charged   int64  `json:"charged"`
	Reserved  int64  `json:"reserved"`
}
type ManagedEvent struct {
	Contract     string      `json:"contract"`
	RunID        string      `json:"run_id"`
	UsageKnown   *bool       `json:"usage_known,omitempty"`
	Usage        *TokenUsage `json:"usage,omitempty"`
	ChargedUnits *int        `json:"charged_units,omitempty"`
	ErrorCode    string      `json:"error_code,omitempty"`
	Ordinal      int         `json:"ordinal"`
	Kind         string      `json:"kind"`
}
type ManagedRun struct {
	Contract        string         `json:"contract"`
	CreatedAt       time.Time      `json:"created_at"`
	ID              string         `json:"id"`
	Owner           string         `json:"-"`
	ProfileID       string         `json:"profile_id"`
	ProfileRevision int64          `json:"profile_revision,string"`
	BindingRevision int64          `json:"binding_revision,string"`
	InstanceID      string         `json:"instance_id"`
	State           string         `json:"state"`
	Lease           int64          `json:"-"`
	Charged         *int           `json:"charged_units"`
	UsageKnown      bool           `json:"usage_known"`
	Usage           TokenUsage     `json:"usage"`
	ErrorCode       string         `json:"error_code"`
	Events          []ManagedEvent `json:"events"`
}
type ManagedResult struct {
	Run      ManagedRun   `json:"run"`
	Response ChatResponse `json:"response"`
}
type ManagedInventory struct {
	Profiles []ManagedProfile `json:"profiles"`
	Bindings []ManagedBinding `json:"bindings"`
	Budgets  []ManagedBudget  `json:"budgets"`
}

// Invocation accepts explicit prompt text only. No Knowledge/source IDs or
// private context authority exist here; RAG consent is a separate 14b contract.
type ManagedChat interface {
	Invoke(context.Context, identityport.SessionPrincipalResolver, string, int64, ChatRequest) (ManagedResult, error)
	Run(context.Context, identityport.SessionPrincipalResolver, string) (ManagedRun, error)
}
type managedInstance struct {
	ID              string
	ProfileRevision int64
	BindingRevision int64
	ProfileID       string
	ctx             context.Context
	cancel          context.CancelFunc
}
type ManagedService struct {
	calls            sync.WaitGroup
	pool             *pgxpool.Pool
	reliable         *reliability.Service
	admins           identityport.AdminExecutionAuthorityReader
	enabled          func() bool
	keys             *security.Keyring
	configs          []managedDeployment
	defaultProfile   string
	configurationErr error
	mu               sync.Mutex
	stopped          bool
	instances        map[string]*managedInstance
	cancel           context.CancelFunc
	done             chan struct{}
	remote           func(security.EgressPolicy) (managedEgress, error)
}
type managedEgress interface {
	Do(context.Context, security.EgressRequest) (security.EgressResponse, error)
}

func managedPrincipal(ctx context.Context, resolve identityport.SessionPrincipalResolver) (identityport.PrincipalContext, identityport.SessionPrincipalResolver, error) {
	var zero identityport.PrincipalContext
	if resolve == nil {
		return zero, nil, ErrManagedDenied
	}
	p, e := resolve(ctx)
	if e != nil {
		return zero, nil, e
	}
	if p.Validate() != nil || p.Actor.Kind != "user" || p.Audience != "user" || p.Actor.ID == "" {
		return zero, nil, ErrManagedDenied
	}
	bound := func(c context.Context) (identityport.PrincipalContext, error) {
		q, e := resolve(c)
		if e != nil {
			return q, e
		}
		if q.Validate() != nil || !reflect.DeepEqual(p, q) {
			return zero, ErrManagedDenied
		}
		return q, nil
	}
	return p, bound, nil
}
func (s *ManagedService) check(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if s == nil || s.pool == nil || s.reliable == nil || s.configurationErr != nil || s.keys == nil || s.enabled == nil || !s.enabled() {
		return ErrManagedUnavailable
	}
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		return ErrManagedUnavailable
	}
	return nil
}
func (s *ManagedService) command(ctx context.Context, code, actorType, actor, id string, fn func(context.Context) error) error {
	permission := ""
	if actorType == "admin" {
		permission = "ai.ai.manage"
	}
	return s.reliable.Execute(ctx, reliability.Command{Code: "ai." + code, ActorType: actorType, ActorID: actor, ResourceType: "ai.managed", ResourceID: id, PermissionCode: permission, OperationCode: "ai." + code, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("ai.managed.changed.v1", "ai.managed", id, map[string]string{"id": id, "operation": code})
	}}, fn)
}
func (s *ManagedService) invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, i := range s.instances {
		i.cancel()
	}
	s.instances = map[string]*managedInstance{}
}
func (s *ManagedService) instance(p ManagedProfile, b ManagedBinding) *managedInstance {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.instances[p.ID]
	if i == nil || i.ProfileRevision != p.Revision || i.BindingRevision != b.Revision || i.ctx.Err() != nil {
		if i != nil {
			i.cancel()
		}
		ctx, cancel := context.WithCancel(context.Background())
		if s.stopped {
			cancel()
		}
		i = &managedInstance{ID: managedID("ai_"), ProfileRevision: p.Revision, BindingRevision: b.Revision, ProfileID: p.ID, ctx: ctx, cancel: cancel}
		s.instances[p.ID] = i
	}
	return i
}
func safeManagedCode(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	if errors.Is(err, ErrManagedConflict) {
		return "changed"
	}
	if errors.Is(err, ErrManagedUnavailable) {
		return "protection"
	}
	return "provider"
}
func managedNow() int64 { return time.Now().UnixMilli() }
