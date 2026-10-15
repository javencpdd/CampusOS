package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
)

func moderationPreviewFixture(t *testing.T) (*PostService, userFlagEnv, *repository.MemoryPostRepository, *identityrepo.MemoryDelegationRepository) {
	t.Helper()
	e := userFlagFixture(t)
	ctx := context.Background()
	posts := repository.NewMemoryPostRepository()
	for _, id := range []string{"reply.one", "reply.two"} {
		if err := posts.Create(ctx, &domain.Post{ID: id, ThreadID: "190101", AuthorID: "190102", Content: "private reply body", Status: "published"}); err != nil {
			t.Fatal(err)
		}
	}
	grants := identityrepo.NewMemoryDelegationRepository()
	now := time.Now()
	if err := grants.InsertDelegations(ctx, []identityrepo.Delegation{{ID: "grant.preview", Kind: "grant", SubjectKind: "user", SubjectID: "190104", Action: "community.post.delete", BoardID: "190103", NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	s := NewPostService(posts, nil)
	s.SetThreadRepository(e.threads)
	s.SetThreadCommandSource(e.s)
	s.SetGovernanceAuthority(identityservice.NewBoardGovernanceAuthorityService(grants, nil, nil))
	return s, e, posts, grants
}

func previewConfig(context.Context) (communityport.ModerationPreviewConfig, error) {
	return communityport.ModerationPreviewConfig{Pin: true, Lock: true, DeletePost: true}, nil
}

func TestModerationPreviewActualPolicyBoundedBatchAndSafeDTO(t *testing.T) {
	for _, mode := range []string{"current", "empty", "no_role", "no_grant", "trash", "deleted_post", "disabled", "duplicate", "unknown", "cross_parent", "limit", "missing"} {
		t.Run(mode, func(t *testing.T) {
			s, e, posts, grants := moderationPreviewFixture(t)
			ctx := context.Background()
			ids := []string{"reply.one", "reply.two"}
			config := previewConfig
			switch mode {
			case "empty":
				ids = nil
			case "no_role":
				e.roles.RevokeRole(ctx, "190104", 2)
			case "no_grant":
				grants.SetStatusCAS(ctx, "grant.preview", 1, "revoked", time.Now())
			case "trash":
				row, _ := e.threads.GetByID(ctx, "190101")
				row.DeletionStatus = "trashed"
				e.threads.Update(ctx, row)
			case "deleted_post":
				posts.Delete(ctx, "reply.one")
			case "disabled":
				config = func(context.Context) (communityport.ModerationPreviewConfig, error) {
					return communityport.ModerationPreviewConfig{}, nil
				}
			case "duplicate":
				ids = []string{"reply.one", "reply.one"}
			case "unknown":
				ids = []string{"missing.reply"}
			case "cross_parent":
				row, _ := posts.GetByID(ctx, "reply.one")
				row.ThreadID = "other.thread"
				posts.Update(ctx, row)
			case "limit":
				ids = make([]string, 51)
			case "missing":
				s.SetThreadCommandSource(nil)
			}
			beforePosts, beforeThread := posts.Snapshot(), e.threads.Snapshot()
			result, err := s.PreviewUserModeration(ctx, "190101", ids, e.proof, config)
			if mode == "duplicate" || mode == "unknown" || mode == "cross_parent" || mode == "limit" || mode == "missing" {
				if err == nil || result != nil {
					t.Fatal("invalid batch accepted", mode, result, err)
				}
				return
			}
			if err != nil || result == nil || len(result.Posts) != len(ids) {
				t.Fatal(mode, result, err)
			}
			wantFlags := mode != "no_role" && mode != "trash" && mode != "disabled"
			if result.Pin != wantFlags || result.Lock != wantFlags {
				t.Fatal("flag preview disagrees with command policy", mode, result)
			}
			for _, item := range result.Posts {
				want := mode != "no_grant" && mode != "trash" && mode != "disabled" && !(mode == "deleted_post" && item.ID == "reply.one")
				if item.Delete != want {
					t.Fatal("actual post preview disagrees with delete policy", mode, item)
				}
			}
			raw, _ := json.Marshal(result)
			for _, private := range []string{"private", "190102", "190103", "grant.preview", "facts_version", "credential"} {
				if strings.Contains(string(raw), private) {
					t.Fatal("preview leaked facts", string(raw))
				}
			}
			if !reflect.DeepEqual(beforePosts, posts.Snapshot()) || !reflect.DeepEqual(beforeThread, e.threads.Snapshot()) {
				t.Fatal("preview mutated resources or display counters")
			}
		})
	}
}

type previewPostProbe struct {
	*repository.MemoryPostRepository
	reads int
	hook  func(int)
}

func (r *previewPostProbe) ReadPostCommandSnapshot(ctx context.Context, id string) (*domain.Post, error) {
	r.reads++
	row, err := r.MemoryPostRepository.ReadPostCommandSnapshot(ctx, id)
	if r.hook != nil {
		r.hook(r.reads)
	}
	return row, err
}

func TestModerationPreviewWholeAndEmptyFinalRechecks(t *testing.T) {
	for _, mode := range []string{"earlier_post_aba", "parent_aba", "grant_revoke", "role_aba", "session_revoke", "config", "empty_role_aba", "counters"} {
		t.Run(mode, func(t *testing.T) {
			s, e, posts, grants := moderationPreviewFixture(t)
			ctx := context.Background()
			ids := []string{"reply.one", "reply.two"}
			config := previewConfig
			changed := false
			mutate := func() {
				if changed {
					return
				}
				changed = true
				switch mode {
				case "earlier_post_aba":
					row, _ := posts.GetByID(ctx, "reply.one")
					body := row.Content
					row.Content = "temporary body"
					posts.Update(ctx, row)
					row.Content = body
					posts.Update(ctx, row)
				case "parent_aba":
					row, _ := e.threads.GetByID(ctx, "190101")
					body := row.Content
					row.Content = "temporary parent"
					e.threads.Update(ctx, row)
					row.Content = body
					e.threads.Update(ctx, row)
				case "grant_revoke":
					grants.SetStatusCAS(ctx, "grant.preview", 1, "revoked", time.Now())
				case "role_aba", "empty_role_aba":
					e.roles.ReplaceRolePermissions(ctx, 2, nil, "test")
					e.roles.ReplaceRolePermissions(ctx, 2, []string{"community.thread.pin", "community.thread.lock"}, "test")
				case "session_revoke":
					p, _ := e.proof(ctx)
					session, _ := e.sessions.GetByID(ctx, p.CredentialID)
					at := time.Now()
					session.RevokedAt = &at
					e.sessions.Update(ctx, session)
				case "counters":
					e.threads.IncrementViewCount(ctx, "190101")
					row, _ := posts.GetByID(ctx, "reply.one")
					row.LikeCount++
					posts.Update(ctx, row)
				}
			}
			if mode == "empty_role_aba" {
				ids = nil
				calls := 0
				config = func(context.Context) (communityport.ModerationPreviewConfig, error) {
					calls++
					if calls == 2 {
						mutate()
					}
					return previewConfig(ctx)
				}
			} else {
				s.repo = &previewPostProbe{MemoryPostRepository: posts, hook: func(read int) {
					if read == 2 {
						mutate()
					}
				}}
				if mode == "config" {
					config = func(context.Context) (communityport.ModerationPreviewConfig, error) {
						if changed {
							return communityport.ModerationPreviewConfig{}, nil
						}
						return previewConfig(ctx)
					}
				}
			}
			result, err := s.PreviewUserModeration(ctx, "190101", ids, e.proof, config)
			if mode == "counters" {
				if err != nil || result == nil || !result.Pin || !result.Posts[0].Delete {
					t.Fatal(result, err)
				}
				return
			}
			if err == nil || result != nil {
				t.Fatal("stale whole batch or empty proof accepted", mode, result, err)
			}
		})
	}
	s, e, _, _ := moderationPreviewFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.PreviewUserModeration(ctx, "190101", nil, e.proof, previewConfig); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
