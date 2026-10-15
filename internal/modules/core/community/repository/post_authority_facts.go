package repository

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"regexp"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

var (
	ErrPostFactsConflict    = errors.New("post authority facts changed")
	ErrPostFactsUnavailable = errors.New("post authority facts unavailable")
)

type FactsGuardedPostRepository interface {
	LockPostForWrite(context.Context, string) (*domain.Post, error)
	ReadPostCommandSnapshot(context.Context, string) (*domain.Post, error)
	UpdatePostIfFactsVersion(context.Context, *domain.Post, int64) error
	DeletePostIfFactsVersion(context.Context, string, int64, identityport.PrincipalRef) error
}

func clonePost(p *domain.Post) *domain.Post {
	if p == nil {
		return nil
	}
	x := *p
	x.Metadata = append(json.RawMessage(nil), p.Metadata...)
	if p.ParentID != nil {
		v := *p.ParentID
		x.ParentID = &v
	}
	if p.DeletedAt != nil {
		v := *p.DeletedAt
		x.DeletedAt = &v
	}
	return &x
}
func preparePost(p *domain.Post) {
	if p.ContentFormat == "" {
		p.ContentFormat = "markdown"
	}
	if p.Status == "" {
		p.Status = "published"
	}
	if p.DeletedActorKind == "" {
		p.DeletedActorKind = "legacy_unknown"
	}
	if len(p.Metadata) == 0 {
		p.Metadata = json.RawMessage(`{}`)
	}
}
func samePostFacts(a, b *domain.Post) bool {
	x, y := clonePost(a), clonePost(b)
	if x == nil || y == nil {
		return false
	}
	x.FactsVersion, y.FactsVersion = 0, 0
	x.LikeCount, y.LikeCount = 0, 0
	x.UpdatedAt, y.UpdatedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(x, y)
}
func (r *MemoryPostRepository) nextPostFactsVersionLocked() (int64, error) {
	if r.versionCounter == math.MaxInt64 {
		return 0, ErrPostFactsUnavailable
	}
	r.versionCounter++
	return r.versionCounter, nil
}

type memoryPostRow struct {
	Post      *domain.Post
	Version   int64
	Metadata  json.RawMessage
	DeletedAt *time.Time
	ActorKind string
	ActorID   string
}
type memoryPostState struct {
	Posts map[string]memoryPostRow
	Next  map[string]int
}

func (r *MemoryPostRepository) ReadPostCommandSnapshot(_ context.Context, id string) (*domain.Post, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.posts[id]
	if !ok {
		return nil, ErrPostNotFound
	}
	return clonePost(p), nil
}
func (r *MemoryPostRepository) LockPostForWrite(ctx context.Context, id string) (*domain.Post, error) {
	if !transaction.Active(ctx) {
		return nil, ErrThreadWriteTransactionRequired
	}
	return r.ReadPostCommandSnapshot(ctx, id)
}
func (r *MemoryPostRepository) UpdatePostIfFactsVersion(ctx context.Context, p *domain.Post, expected int64) error {
	if !transaction.Active(ctx) {
		return ErrThreadWriteTransactionRequired
	}
	if p == nil || expected < 1 {
		return ErrPostFactsConflict
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.posts[p.ID]
	if !ok || current.DeletedAt != nil {
		return ErrPostNotFound
	}
	if current.FactsVersion != expected {
		return ErrPostFactsConflict
	}
	candidate := clonePost(current)
	candidate.Content = p.Content
	candidate.ContentFormat = p.ContentFormat
	candidate.UpdatedAt = time.Now().UTC()
	preparePost(candidate)
	if !samePostFacts(current, candidate) {
		v, err := r.nextPostFactsVersionLocked()
		if err != nil {
			return err
		}
		candidate.FactsVersion = v
	}
	r.posts[p.ID] = candidate
	p.FactsVersion = candidate.FactsVersion
	p.LikeCount = current.LikeCount
	return nil
}

var postActorIDShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func validPostDeletionActor(a identityport.PrincipalRef) bool {
	return (a.Kind == "user" || a.Kind == "admin") && postActorIDShape.MatchString(a.ID)
}

func (r *MemoryPostRepository) DeletePostIfFactsVersion(ctx context.Context, id string, expected int64, actor identityport.PrincipalRef) error {
	if !transaction.Active(ctx) {
		return ErrThreadWriteTransactionRequired
	}
	if expected < 1 || !validPostDeletionActor(actor) {
		return ErrPostFactsConflict
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.posts[id]
	if !ok || p.DeletedAt != nil {
		return ErrPostNotFound
	}
	if p.FactsVersion != expected {
		return ErrPostFactsConflict
	}
	version, err := r.nextPostFactsVersionLocked()
	if err != nil {
		return err
	}
	x := clonePost(p)
	now := time.Now().UTC()
	x.DeletedAt = &now
	x.Status = "deleted"
	x.DeletedActorKind, x.DeletedActorID = actor.Kind, actor.ID
	x.FactsVersion = version
	r.posts[id] = x
	return nil
}

const postSelectColumns = `id,thread_id,author_id,author_name,parent_id,parent_floor_number,content,status,like_count,floor_number,created_at,updated_at,content_format,metadata,deleted_at,facts_version,deleted_actor_kind,deleted_actor_id`

func scanPost(row rowScanner) (*domain.Post, error) {
	p := &domain.Post{}
	err := row.Scan(&p.ID, &p.ThreadID, &p.AuthorID, &p.AuthorName, &p.ParentID, &p.ParentFloorNumber, &p.Content, &p.Status, &p.LikeCount, &p.FloorNumber, &p.CreatedAt, &p.UpdatedAt, &p.ContentFormat, &p.Metadata, &p.DeletedAt, &p.FactsVersion, &p.DeletedActorKind, &p.DeletedActorID)
	return p, err
}
func (r *PgPostRepository) checkPostFactsGuard(ctx context.Context) error {
	var ok bool
	err := r.db(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.posts'::regclass AND tgname='trg_posts_facts_version' AND NOT tgisinternal AND tgenabled IN('O','A') AND tgtype=23 AND tgfoid=to_regprocedure('public.advance_post_facts_version()')) AND EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.post_facts_version_seq') AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1)`).Scan(&ok)
	if err != nil || !ok {
		return ErrPostFactsUnavailable
	}
	return nil
}
func (r *PgPostRepository) readPostCommand(ctx context.Context, id string, lock bool) (*domain.Post, error) {
	if err := r.checkPostFactsGuard(ctx); err != nil {
		return nil, err
	}
	q := `SELECT ` + postSelectColumns + ` FROM posts WHERE id=$1`
	if lock {
		q += ` FOR UPDATE`
	}
	p, err := scanPost(r.db(ctx).QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPostNotFound
	}
	return p, err
}
func (r *PgPostRepository) ReadPostCommandSnapshot(ctx context.Context, id string) (*domain.Post, error) {
	return r.readPostCommand(ctx, id, false)
}
func (r *PgPostRepository) LockPostForWrite(ctx context.Context, id string) (*domain.Post, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return nil, ErrThreadWriteTransactionRequired
	}
	return r.readPostCommand(ctx, id, true)
}
func (r *PgPostRepository) UpdatePostIfFactsVersion(ctx context.Context, p *domain.Post, expected int64) error {
	if _, ok := transaction.FromContext(ctx); !ok {
		return ErrThreadWriteTransactionRequired
	}
	if p == nil || expected < 1 {
		return ErrPostFactsConflict
	}
	if err := r.checkPostFactsGuard(ctx); err != nil {
		return err
	}
	preparePost(p)
	err := r.db(ctx).QueryRow(ctx, `UPDATE posts SET content=$2,content_format=$3,updated_at=NOW() WHERE id=$1 AND facts_version=$4 AND deleted_at IS NULL RETURNING facts_version,like_count`, p.ID, p.Content, p.ContentFormat, expected).Scan(&p.FactsVersion, &p.LikeCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPostFactsConflict
	}
	return err
}
func (r *PgPostRepository) DeletePostIfFactsVersion(ctx context.Context, id string, expected int64, actor identityport.PrincipalRef) error {
	if _, ok := transaction.FromContext(ctx); !ok {
		return ErrThreadWriteTransactionRequired
	}
	if expected < 1 || !validPostDeletionActor(actor) {
		return ErrPostFactsConflict
	}
	if err := r.checkPostFactsGuard(ctx); err != nil {
		return err
	}
	tag, err := r.db(ctx).Exec(ctx, `UPDATE posts SET deleted_at=NOW(),status='deleted',deleted_actor_kind=$3,deleted_actor_id=$4 WHERE id=$1 AND facts_version=$2 AND deleted_at IS NULL`, id, expected, actor.Kind, actor.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrPostFactsConflict
	}
	return nil
}

func validPostActorFacts(p *domain.Post) bool {
	if p.DeletedActorKind == "legacy_unknown" {
		return p.DeletedActorID == ""
	}
	return p.DeletedAt != nil && validPostDeletionActor(identityport.PrincipalRef{Kind: p.DeletedActorKind, ID: p.DeletedActorID})
}
