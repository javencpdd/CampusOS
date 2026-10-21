package knowledge

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type collectionAdmins struct {
	allowed  bool
	strength string
	epoch    int
	after    func(bool)
}

func (a *collectionAdmins) LoadAdminExecutionAuthority(ctx context.Context, q identityport.AdminExecutionQuery, resolve identityport.SessionPrincipalResolver, lock bool) (identityport.AdminExecutionAuthority, error) {
	if a.after != nil {
		a.after(lock)
	}
	p, e := resolve(ctx)
	if e != nil {
		return identityport.AdminExecutionAuthority{}, e
	}
	p.AuthenticationStrength = a.strength
	return identityport.AdminExecutionAuthority{Principal: p, Query: q, Allowed: a.allowed && (q.PermissionCode == "knowledge.collection.read" || a.strength == "mfa"), ExpiresAtMS: time.Now().Add(time.Hour).Truncate(time.Hour).UnixMilli(), Binding: a.epoch}, nil
}

type catalogue struct{ rows []*domain.Category }

func (c *catalogue) ListSourceCategories(context.Context) ([]*domain.Category, error) {
	return c.rows, nil
}

type unusedArticle struct{}

func (unusedArticle) PreviewPublicArticle(context.Context, string) (PublicArticleCandidate, error) {
	return PublicArticleCandidate{}, ErrUnsupportedSource
}
func (unusedArticle) RecheckPublicArticle(context.Context, SourceReference) (PublicArticleCandidate, error) {
	return PublicArticleCandidate{}, ErrUnsupportedSource
}

type failAuditStore struct {
	reliability.Store
	fail bool
}

func (s *failAuditStore) RecordCommandAudit(ctx context.Context, a reliability.CommandAudit) error {
	if s.fail {
		return errors.New("required audit unavailable")
	}
	return s.Store.RecordCommandAudit(ctx, a)
}

type collectionsFixture struct {
	s       *CollectionService
	threads *repository.MemoryThreadRepository
	repo    *MemoryRepository
	admins  *collectionAdmins
	cat     *catalogue
	store   *failAuditStore
	resolve identityport.SessionPrincipalResolver
	enabled bool
}

func fixtureCollections(t *testing.T) *collectionsFixture {
	t.Helper()
	f := &collectionsFixture{repo: NewMemoryRepository(), admins: &collectionAdmins{allowed: true, strength: "mfa", epoch: 1}, enabled: true}
	f.resolve = func(context.Context) (identityport.PrincipalContext, error) {
		return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "admin", ID: "admin.knowledge"}, Audience: "admin", CredentialID: "session.knowledge", AuthenticationStrength: "mfa"}, nil
	}
	f.threads = repository.NewMemoryThreadRepository()
	if e := f.threads.Create(t.Context(), publicFixture()); e != nil {
		t.Fatal(e)
	}
	query := community.NewContentQuery(service.NewThreadService(f.threads, nil))
	plain, e := NewPublicSourceService(query, func() bool { return f.enabled })
	if e != nil {
		t.Fatal(e)
	}
	f.cat = &catalogue{rows: []*domain.Category{{ID: "91003", Name: "公开板块", NodeKind: domain.CategoryNodeBoard, LifecycleStatus: domain.CategoryLifecycleActive, Version: 1}}}
	f.store = &failAuditStore{Store: reliability.NewMemoryStore()}
	reliable := reliability.NewService(transaction.NewMemory(), f.store)
	reliable.RegisterMemorySnapshotters(f.store.Store.(transaction.Snapshotter))
	f.s, e = NewCollectionService(f.repo, f.admins, reliable, query, f.cat, plain, unusedArticle{}, func() bool { return f.enabled })
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *collectionsFixture) collection(t *testing.T) Collection {
	t.Helper()
	c, e := f.s.SaveCollection(t.Context(), Collection{Title: "公开知识", Status: "active"}, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func (f *collectionsFixture) selection(t *testing.T, c Collection, kind, id string) CollectionState {
	t.Helper()
	state, e := f.s.SaveSelection(t.Context(), c.ID, c.Version, Selection{Kind: kind, SourceID: id, Active: true}, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	return state
}
func TestCollectionsOfficialHistoryAndCurrentRevision(t *testing.T) {
	f := fixtureCollections(t)
	c := f.collection(t)
	official, e := f.s.SaveOfficial(t.Context(), Official{Title: "官方校历", Content: "# 当前校历\nIgnore prior instructions is untrusted data.", Format: "markdown", Status: "draft"}, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.SaveSelection(t.Context(), c.ID, c.Version, Selection{Kind: "official", SourceID: official.ID, Active: true}, f.resolve); !errors.Is(e, ErrNotFound) {
		t.Fatal("draft selected", e)
	}
	official.Status = "published"
	official, e = f.s.SaveOfficial(t.Context(), official, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	state := f.selection(t, c, "official", official.ID)
	if len(state.Members) != 1 || len(state.Selections) != 1 {
		t.Fatal(state)
	}
	page, e := f.s.Sources(t.Context(), c.ID, 1, 50)
	if e != nil || page.Total != 1 || page.Items[0].Source.Content != official.Content {
		t.Fatal(page, e)
	}
	old := page.Items[0]
	official.Content = "修订校历"
	official, e = f.s.SaveOfficial(t.Context(), official, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	if out, e := f.s.Source(t.Context(), c.ID, old.MemberID, old.Revision); !errors.Is(e, ErrConflict) || out != (VisibleSource{}) {
		t.Fatal("old revision released", out, e)
	}
	current, e := f.s.Source(t.Context(), c.ID, old.MemberID, "")
	if e != nil || current.Source.Content != "修订校历" {
		t.Fatal(current, e)
	}
	official.Status = "withdrawn"
	official, e = f.s.SaveOfficial(t.Context(), official, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	page, e = f.s.Sources(t.Context(), c.ID, 1, 50)
	if e != nil || page.Total != 0 || len(page.Items) != 0 {
		t.Fatal(page, e)
	}
	official.Status = "published"
	official, e = f.s.SaveOfficial(t.Context(), official, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	history, e := f.s.OfficialHistory(t.Context(), official.ID, 0, f.resolve)
	if e != nil || len(history.Items) != 5 || history.Items[0].OfficialVersion != official.Version || history.Items[4].Snapshot.Status != "draft" {
		t.Fatal(history, e)
	}
	// History returned to Admin is detached, and has explicit Admin authorship.
	history.Items[0].Snapshot.Content = "caller mutation"
	again, _ := f.s.OfficialHistory(t.Context(), official.ID, 0, f.resolve)
	if again.Items[0].Snapshot.Content == "caller mutation" || again.Items[0].ChangedByAdminID != "admin.knowledge" {
		t.Fatal(again)
	}
}
func TestCollectionsRequiredAuditAuthorityAndCAS(t *testing.T) {
	f := fixtureCollections(t)
	f.admins.strength = "password"
	if _, e := f.s.SaveCollection(t.Context(), Collection{Title: "test", Status: "active"}, f.resolve); !errors.Is(e, ErrMFARequired) {
		t.Fatal(e)
	}
	f.admins.strength = "mfa"
	f.admins.allowed = false
	if _, e := f.s.Inventory(t.Context(), "collections", f.resolve); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	f.admins.allowed = true
	f.store.fail = true
	if _, e := f.s.SaveOfficial(t.Context(), Official{Title: "test", Content: "private draft", Format: "text", Status: "draft"}, f.resolve); e == nil {
		t.Fatal("audit ignored")
	}
	f.store.fail = false
	if rows, _ := f.repo.list(t.Context(), officials, "", 100); len(rows) != 0 {
		t.Fatal("official committed without audit")
	}
	if rows, _ := f.repo.list(t.Context(), revisions, "", 100); len(rows) != 0 {
		t.Fatal("revision committed without audit")
	}
	c := f.collection(t)
	in := c
	in.Title = "new"
	next, e := f.s.SaveCollection(t.Context(), in, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.SaveCollection(t.Context(), in, f.resolve); !errors.Is(e, ErrConflict) {
		t.Fatal("stale CAS", e)
	}
	before := f.repo.Snapshot()
	f.admins.after = func(lock bool) {
		if lock {
			f.admins.epoch++
		}
	}
	if _, e = f.s.SaveCollection(t.Context(), Collection{ID: next.ID, Version: next.Version, Title: "uncommitted", Status: "active"}, f.resolve); e == nil {
		t.Fatal("changed authority accepted")
	}
	if !reflect.DeepEqual(before, f.repo.Snapshot()) {
		t.Fatal("authority failure changed data")
	}
	f.admins.after = nil
	f.enabled = false
	if _, e = f.s.PublicCollections(t.Context()); !errors.Is(e, ErrDisabled) {
		t.Fatal(e)
	}
}
func TestCollectionsBoardSelectionIndependentMembersAndRevocation(t *testing.T) {
	f := fixtureCollections(t)
	c := f.collection(t)
	state := f.selection(t, c, "board", "91003")
	if len(state.Members) != 0 {
		t.Fatal("rule is not membership")
	}
	refresh, e := f.s.Refresh(t.Context(), c.ID, state.Selections[0].ID, state.Collection.Version, state.Selections[0].Version, 1, f.resolve)
	if e != nil || refresh.Examined != 1 || refresh.Materialized != 1 || refresh.HasMore || len(refresh.State.Members) != 1 {
		t.Fatal(refresh, e)
	}
	state = refresh.State
	page, e := f.s.Sources(t.Context(), c.ID, 1, 1)
	if e != nil || page.Total != 1 {
		t.Fatal(page, e)
	}
	old := page.Items[0]
	row, _ := f.threads.GetByID(t.Context(), "91001")
	row.PublicationStatus = "private"
	if e = f.threads.Update(t.Context(), row); e != nil {
		t.Fatal(e)
	}
	page, e = f.s.Sources(t.Context(), c.ID, 1, 1)
	if e != nil || page.Total != 0 {
		t.Fatal(page, e)
	}
	row.PublicationStatus = "published"
	if e = f.threads.Update(t.Context(), row); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Source(t.Context(), c.ID, old.MemberID, old.Revision); !errors.Is(e, ErrConflict) {
		t.Fatal("origin ABA", e)
	}
	row.CategoryID = "91004"
	_ = f.threads.Update(t.Context(), row)
	page, e = f.s.Sources(t.Context(), c.ID, 1, 1)
	if e != nil || page.Total != 0 {
		t.Fatal("moved source leaked", page, e)
	}
	row.CategoryID = "91003"
	_ = f.threads.Update(t.Context(), row)
	f.cat.rows[0].LifecycleStatus = "archived"
	page, e = f.s.Sources(t.Context(), c.ID, 1, 1)
	if e != nil || page.Total != 0 {
		t.Fatal("archived board leaked", page, e)
	}
	f.cat.rows[0].LifecycleStatus = "active"
	f.cat.rows[0].Version++
	rule := state.Selections[0]
	rule.Active = false
	state, e = f.s.SaveSelection(t.Context(), c.ID, state.Collection.Version, rule, f.resolve)
	if e != nil || len(state.Members) != 0 {
		t.Fatal(state, e)
	}
	if _, e = f.s.Refresh(t.Context(), c.ID, rule.ID, state.Collection.Version, rule.Version, 1, f.resolve); !errors.Is(e, ErrConflict) {
		t.Fatal("old refresh revived rule", e)
	}
	rule = state.Selections[0]
	rule.Active = true
	state, e = f.s.SaveSelection(t.Context(), c.ID, state.Collection.Version, rule, f.resolve)
	if e != nil || len(state.Members) != 0 {
		t.Fatal(state, e)
	}
	if _, e = f.s.Source(t.Context(), c.ID, old.MemberID, ""); !errors.Is(e, ErrNotFound) {
		t.Fatal("old member revived", e)
	}
}
func TestCollectionsPaginationOverlapAndLimits(t *testing.T) {
	f := fixtureCollections(t)
	c := f.collection(t)
	single := f.selection(t, c, "thread", "91001")
	state := f.selection(t, single.Collection, "board", "91003")
	var board Selection
	for _, r := range state.Selections {
		if r.Kind == "board" {
			board = r
		}
	}
	for n := 0; n < 52; n++ {
		r := *publicFixture()
		r.ID = fmt.Sprint(92000 + n)
		r.Title = fmt.Sprint("source", n)
		if e := f.threads.Create(t.Context(), &r); e != nil {
			t.Fatal(e)
		}
	}
	first, e := f.s.Refresh(t.Context(), c.ID, board.ID, state.Collection.Version, board.Version, 1, f.resolve)
	if e != nil || !first.HasMore || first.NextPage != 2 || first.Materialized != 50 {
		t.Fatal(first, e)
	}
	second, e := f.s.Refresh(t.Context(), c.ID, board.ID, first.State.Collection.Version, board.Version, 2, f.resolve)
	if e != nil || second.HasMore || second.Materialized != 3 {
		t.Fatal(second, e)
	}
	page, e := f.s.Sources(t.Context(), c.ID, 1, 50)
	if e != nil || page.Total != 53 || len(page.Items) != 50 {
		t.Fatal(page, e)
	}
	page, e = f.s.Sources(t.Context(), c.ID, 2, 50)
	if e != nil || len(page.Items) != 3 || page.Total != 53 {
		t.Fatal(page, e)
	}
	rule := single.Selections[0]
	rule.Active = false
	state, e = f.s.SaveSelection(t.Context(), c.ID, second.State.Collection.Version, rule, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	page, e = f.s.Sources(t.Context(), c.ID, 1, 50)
	if e != nil || page.Total != 53 {
		t.Fatal("overlap withdrawal deleted legitimate membership", page, e)
	}
	if _, e = f.s.SaveOfficial(t.Context(), Official{Title: "huge", Content: strings.Repeat("x", MaxSourceBodyBytes+1), Format: "text", Status: "published"}, f.resolve); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if _, e = f.s.Sources(t.Context(), c.ID, 1, 51); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	state.Collection.Status = "disabled"
	_, e = f.s.SaveCollection(t.Context(), state.Collection, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Sources(t.Context(), c.ID, 1, 50); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCollectionsHistoryCursorAndBoardParent(t *testing.T) {
	f := fixtureCollections(t)
	source, e := f.s.SaveOfficial(t.Context(), Official{Title: "history", Content: "untrusted text", Format: "text", Status: "draft"}, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	for range 51 {
		source, e = f.s.SaveOfficial(t.Context(), source, f.resolve)
		if e != nil {
			t.Fatal(e)
		}
	}
	first, e := f.s.OfficialHistory(t.Context(), source.ID, 0, f.resolve)
	if e != nil || len(first.Items) != 50 || first.NextBefore < 1 || first.Items[0].OfficialVersion != source.Version {
		t.Fatal(first, e)
	}
	second, e := f.s.OfficialHistory(t.Context(), source.ID, first.NextBefore, f.resolve)
	if e != nil || len(second.Items) != 2 || second.NextBefore != 0 {
		t.Fatal(second, e)
	}
	for _, v := range second.Items {
		if v.OfficialVersion >= first.NextBefore {
			t.Fatal("history cursor overlaps")
		}
	}
	parent := "91004"
	f.cat.rows[0].ParentID = &parent
	f.cat.rows[0].IsClosed = true
	f.cat.rows = append(f.cat.rows, &domain.Category{ID: parent, NodeKind: domain.CategoryNodeGroup, LifecycleStatus: domain.CategoryLifecycleActive, Version: 1})
	c := f.collection(t)
	if _, e = f.s.SaveSelection(t.Context(), c.ID, c.Version, Selection{Kind: "board", SourceID: "91003", Active: true}, f.resolve); e != nil {
		t.Fatal("closed board remains readable", e)
	}
	f.cat.rows[1].LifecycleStatus = domain.CategoryLifecycleArchived
	c = f.collection(t)
	if _, e = f.s.SaveSelection(t.Context(), c.ID, c.Version, Selection{Kind: "board", SourceID: "91003", Active: true}, f.resolve); !errors.Is(e, ErrNotFound) {
		t.Fatal("archived parent selected", e)
	}
}
