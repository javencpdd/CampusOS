package space

import (
	"context"
	"errors"
	"math"
	"reflect"

	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

type currentSpaceContentSnapshot struct {
	Facts *spaceAdminFacts
	Rows  []*communitydomain.Thread
	Total int64
}
type currentSpaceContentResult struct {
	Items []*SpaceContent
	Total int64
}

func detachedCurrentSpaceThread(row *communitydomain.Thread) *communitydomain.Thread {
	if row == nil {
		return nil
	}
	value := *row
	value.Tags = append([]string(nil), row.Tags...)
	value.CreatedAt = value.CreatedAt.UTC()
	value.UpdatedAt = value.UpdatedAt.UTC()
	value.ViewCount, value.ReplyCount, value.LikeCount = 0, 0, 0
	if row.ModerationAt != nil {
		at := row.ModerationAt.UTC()
		value.ModerationAt = &at
	}
	if row.DeletedAt != nil {
		at := row.DeletedAt.UTC()
		value.DeletedAt = &at
	}
	return &value
}
func validateCurrentSpaceThreadPage(rows []*communitydomain.Thread, total int64, owner string, page, size int, public bool) error {
	if total < 0 || int64(page-1) > math.MaxInt64/int64(size) {
		return ErrSpaceFactsUnavailable
	}
	offset := int64(page-1) * int64(size)
	want := int64(0)
	if offset < total {
		want = total - offset
		if want > int64(size) {
			want = int64(size)
		}
	}
	if int64(len(rows)) != want {
		return ErrSpaceFactsUnavailable
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if row == nil || row.ID == "" || seen[row.ID] || row.AuthorID != owner || row.FactsVersion < 1 || row.DeletedAt != nil || row.DeletionStatus != "active" {
			return ErrSpaceFactsUnavailable
		}
		seen[row.ID] = true
		if row.PublicationStatus != "draft" && row.PublicationStatus != "published" && row.PublicationStatus != "private" {
			return ErrSpaceFactsUnavailable
		}
		if row.ModerationStatus != "clear" && row.ModerationStatus != "pending" && row.ModerationStatus != "rejected" && row.ModerationStatus != "taken_down" {
			return ErrSpaceFactsUnavailable
		}
		if public && (row.PublicationStatus != "published" || row.ModerationStatus != "clear") {
			return ErrSpaceFactsUnavailable
		}
	}
	return nil
}
func (s *Service) currentSpaceContents(ctx context.Context, key string, byName, public bool, resolve identityport.SessionPrincipalResolver, page, size int) ([]*SpaceContent, int64, error) {
	page, size = normalizePage(page), normalizePageSize(size)
	if s == nil || corestorage.NilImageAdapter(s.contentQuery) {
		if public {
			return nil, 0, ErrSpaceFactsUnavailable
		}
		return nil, 0, ErrAuthenticatedAuthorQueryUnavailable
	}
	query := s.contentQuery
	var author communityport.AuthenticatedAuthorContentQuery
	if !public {
		var ok bool
		author, ok = query.(communityport.AuthenticatedAuthorContentQuery)
		if !ok || corestorage.NilImageAdapter(author) {
			return nil, 0, ErrAuthenticatedAuthorQueryUnavailable
		}
	}
	operation := "read_contents"
	if public {
		operation = "contents"
		var err error
		key, err = validateCurrentSpaceKey(key, byName)
		if err != nil {
			return nil, 0, err
		}
	}
	result, err := identityport.ReadBoundProjection(ctx, resolve, public, func(ctx context.Context, p identityport.PrincipalContext) (currentSpaceContentSnapshot, error) {
		f, err := s.loadCurrentReadSpace(ctx, key, byName, public, operation, p)
		if err != nil {
			return currentSpaceContentSnapshot{}, err
		}
		snapshot := currentSpaceContentSnapshot{Facts: f, Rows: []*communitydomain.Thread{}}
		if f.Space.Space.SyncEnabled {
			categories := normalizedFilterValues(f.Space.Space.SyncCategories)
			if len(categories) == 0 {
				categories = nil
			}
			filter := communitydomain.ThreadListFilter{CategoryIDs: categories, AnyTags: normalizedFilterValues(f.Space.Space.SyncTags), Page: page, PageSize: size}
			var rows []*communitydomain.Thread
			var total int64
			if public {
				filter.AuthorID = f.User.ID
				rows, total, err = query.ListPublicThreads(ctx, filter)
			} else {
				bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
					current, e := resolveOwnContentsPrincipal(ctx, resolve)
					if e != nil {
						return current, e
					}
					if !reflect.DeepEqual(p, current) {
						return current, identityport.ErrPolicyFactsChanged
					}
					return current, nil
				}
				rows, total, err = author.ListAuthorThreadsForPrincipal(ctx, bound, filter)
			}
			if err != nil {
				return currentSpaceContentSnapshot{}, errors.Join(ErrSpaceFactsUnavailable, err)
			}
			if err := validateCurrentSpaceThreadPage(rows, total, f.User.ID, page, size, public); err != nil {
				return currentSpaceContentSnapshot{}, err
			}
			snapshot.Total = total
			for _, row := range rows {
				snapshot.Rows = append(snapshot.Rows, detachedCurrentSpaceThread(row))
			}
		}
		final, err := s.loadCurrentReadSpace(ctx, key, byName, public, operation, p)
		if err != nil {
			return currentSpaceContentSnapshot{}, err
		}
		if !reflect.DeepEqual(f, final) {
			return currentSpaceContentSnapshot{}, identityport.ErrPolicyFactsChanged
		}
		return snapshot, nil
	}, func(_ context.Context, _ identityport.PrincipalContext, snapshot currentSpaceContentSnapshot) (currentSpaceContentResult, error) {
		items := make([]*SpaceContent, 0, len(snapshot.Rows))
		for _, row := range snapshot.Rows {
			item := contentFromThread(row)
			if public {
				item.ModerationReason = ""
			}
			items = append(items, item)
		}
		return currentSpaceContentResult{Items: items, Total: snapshot.Total}, nil
	}, nil)
	if err != nil {
		return nil, 0, err
	}
	return result.Items, result.Total, nil
}
func (s *Service) ListCurrentPublicContentsByUserID(ctx context.Context, id string, page, size int) ([]*SpaceContent, int64, error) {
	return s.currentSpaceContents(ctx, id, false, true, nil, page, size)
}
func (s *Service) ListCurrentPublicContentsByUsername(ctx context.Context, name string, page, size int) ([]*SpaceContent, int64, error) {
	return s.currentSpaceContents(ctx, name, true, true, nil, page, size)
}
