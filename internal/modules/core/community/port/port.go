package port

import (
	"context"
	"errors"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

var (
	ErrCategoryNotFound         = errors.New("community category not found")
	ErrThreadNotFound           = errors.New("community thread not found")
	ErrPostNotFound             = errors.New("community post not found")
	ErrAuthorCommandDenied      = errors.New("community author command denied")
	ErrAuthorCommandInvalid     = errors.New("invalid community author command")
	ErrAuthorCommandUnavailable = errors.New("checked community author command unavailable")
)

type Category struct{ ID, Name string }
type Thread struct{ ID, CategoryID, AuthorID, Status string }
type Post struct{ ID, ThreadID, AuthorID, Status string }
type CategoryReader interface {
	GetCategory(context.Context, string) (Category, error)
}

// CategoryCatalog is the public read contract for appearance and other
// presentation features that need category display metadata.
type CategoryCatalog interface {
	ListCategories(context.Context) ([]*domain.Category, error)
}

// ContentQuery is Community's read-only content fact contract. Consumers may
// choose author/category/tag/pagination filters, but cannot widen public
// visibility by submitting publication, moderation, or deletion states.
type ContentQuery interface {
	GetPublicThread(context.Context, string) (*domain.Thread, error)
	ListPublicThreads(context.Context, domain.ThreadListFilter) ([]*domain.Thread, int64, error)
	ListAuthorThreads(context.Context, string, domain.ThreadListFilter) ([]*domain.Thread, int64, error)
}

// AuthenticatedAuthorContentQuery is the owner-only list contract for trusted
// first-party ingress. resolve must recheck the current user Session; arbitrary
// actor IDs or parsed Principal DTOs do not satisfy this proof boundary.
// Owner selection and the raw active predicate are enforced before pagination.
// Legacy ContentQuery.ListAuthorThreads remains a separate in-process adapter.
type AuthenticatedAuthorContentQuery interface {
	ListAuthorThreadsForPrincipal(context.Context, func(context.Context) (identityport.PrincipalContext, error), domain.ThreadListFilter) ([]*domain.Thread, int64, error)
}

type ThreadReader interface {
	GetThread(context.Context, string) (Thread, error)
}
type ThreadWriter interface {
	SetThreadStatus(context.Context, string, string) error
}
type PostReader interface {
	GetPost(context.Context, string) (Post, error)
}
type PostWriter interface {
	SetPostStatus(context.Context, string, string) error
}

// ModerationGateway is Community's public governance command contract.
// Moderation receives domain values but cannot access repositories or services.
type ModerationGateway interface {
	GetCategory(context.Context, string) (*domain.Category, error)
	GetThread(context.Context, string) (*domain.Thread, error)
	GetPost(context.Context, string) (*domain.Post, error)
	SetPinned(context.Context, string, bool) (*domain.Thread, error)
	SetLocked(context.Context, string, bool) (*domain.Thread, error)
	DeletePostForModeration(context.Context, string, string) error
}

// NamedCategory identifies one board in a user-facing governance message.
type NamedCategory struct{ ID, Name string }

// ModerationNotifier is Community's user-facing notification contract for
// category-scope moderator lifecycle events. Implementations live in Community;
// Moderation consumes the port without accessing notification repositories.
type ModerationNotifier interface {
	NotifyModeratorScopeGranted(context.Context, string, []NamedCategory) error
	NotifyModeratorScopeRevoked(context.Context, string, []NamedCategory) error
}

// ContentGateway is the stable Community contract consumed by built-in
// content features. It deliberately exposes application commands rather than
// repositories or Community service implementations.
type ContentGateway interface {
	CreateThread(context.Context, string, string, domain.CreateThreadRequest, ThreadCreateOptions) (*domain.Thread, error)
	CreateStructuredThread(context.Context, string, string, domain.CreateThreadRequest, ThreadCreateOptions, StructuredThreadParticipant) (*domain.Thread, error)
	GetThread(context.Context, string) (*domain.Thread, error)
	SaveFeatureThread(context.Context, *domain.Thread, string, string) (*domain.Thread, error)
	TrashThread(context.Context, string, string, string, string) error
	SubmitThreadForReview(context.Context, string, string) (*domain.Thread, error)
	TakeDownThread(context.Context, string, string, string) (*domain.Thread, error)
	RestoreThreadDirectly(context.Context, string, string, string) (*domain.Thread, error)
	RestoreThreadFromTrash(context.Context, string, string) (*domain.Thread, error)
	ListThreads(context.Context, domain.ThreadListFilter) ([]*domain.Thread, int64, error)
	InvalidateThreadList(context.Context)
}

// AuthenticatedThreadQuery is a separate checked read seam for Built-in
// Features. Nil resolver selects public-only; a user resolver proves current
// Session ownership. A missing implementation must never use GetThread instead.
type AuthenticatedThreadQuery interface {
	GetThreadForPrincipal(context.Context, string, func(context.Context) (identityport.PrincipalContext, error)) (*domain.Thread, error)
}

// AuthenticatedThreadCreator is a first-party command seam. Community checks
// current User proof and actual posting authority; the Feature owns and checks
// its typed detail participant in the same required-audit transaction.
type AuthenticatedThreadCreator interface {
	CreateStructuredThreadForPrincipal(context.Context, string, domain.CreateThreadRequest, ThreadCreateOptions, StructuredThreadParticipant, identityport.SessionPrincipalResolver) (*domain.Thread, error)
}

// ThreadAuthorParticipant keeps typed facts and mutations in their Feature.
// Facts must be detached snapshots; lock=true requires its actual transaction.
// This in-process seam is never a client/Host API supplied capability.
type ThreadAuthorParticipant interface {
	ThreadType() domain.ThreadType
	LoadAuthorFacts(context.Context, *domain.Thread, bool) (any, error)
	ApplyAuthorCommand(context.Context, *domain.Thread) error
	CheckAuthorResult(context.Context, *domain.Thread) error
	AuthorEventPayload() any
}

type AuthenticatedThreadAuthorCommands interface {
	ExecuteThreadAuthorCommand(context.Context, string, string, string, string, identityport.SessionPrincipalResolver, ThreadAuthorParticipant) error
}

// Author edit preflight uses the same fixed Policy before staged byte I/O.
// Its snapshot is not a reusable write credential; execution reloads and locks.
type AuthenticatedThreadAuthorEditPreflight interface {
	LoadThreadAuthorEditProof(context.Context, string, identityport.SessionPrincipalResolver) (*domain.Thread, error)
}

// Governance participants own their Feature schema and detached facts. Core
// runs the existing named resource command and authority checks in one required
// audit transaction; this in-process seam is never supplied by a client.
type ThreadGovernanceParticipant interface {
	ThreadType() domain.ThreadType
	LoadGovernanceFacts(context.Context, *domain.Thread, bool) (any, error)
	ApplyGovernanceCommand(context.Context, *domain.Thread, identityport.PrincipalRef) error
	CheckGovernanceResult(context.Context, *domain.Thread, identityport.PrincipalRef) error
}
type AuthenticatedThreadGovernanceCommands interface {
	ExecuteThreadGovernanceCommand(context.Context, string, string, string, identityport.SessionPrincipalResolver, ThreadGovernanceParticipant) error
}

var ErrGovernanceCommandDenied = errors.New("governance command denied")
var ErrGovernanceCommandUnavailable = errors.New("governance command unavailable")
var ErrGovernanceCommandConflict = errors.New("governance command state changed")

type ThreadCreateOptions struct {
	Status        domain.ThreadStatus
	ContentFormat string
	ThreadType    domain.ThreadType
	CommandCode   string
	EventType     string
}

// StructuredThreadParticipant is deliberately an internal Built-in Feature
// extension point. It is never exposed through Host API, SDK, MCP, Agent, or
// external plugin runtime contracts.
type StructuredThreadParticipant interface {
	ThreadType() domain.ThreadType
	PersistThreadDetail(context.Context, *domain.Thread) error
}

// PostGovernanceParticipant owns optional module policy/audit facts. It runs
// inside Community's reliable transaction, never as an external capability.
type PostGovernanceParticipant interface {
	CheckPostGovernance(context.Context) error
	RecordPostGovernance(context.Context, identityport.PrincipalContext, *domain.Thread, *domain.Post) error
}
type CheckedPostGovernanceCommands interface {
	DeletePostForPrincipal(context.Context, string, string, identityport.SessionPrincipalResolver, PostGovernanceParticipant) error
}

var ErrPostGovernanceDenied = errors.New("post governance denied")
var ErrPostGovernanceUnavailable = errors.New("post governance unavailable")
