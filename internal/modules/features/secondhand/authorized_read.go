package secondhand

import (
	"context"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"reflect"
)

type featureReadSnapshot struct {
	ThreadID      string
	ThreadVersion int64
	Detail        *Detail
}

func validFeatureReadDetail(d *Detail, t *domain.Thread) bool {
	if d == nil || t == nil || d.ThreadID != t.ID || d.CreatedBy != t.AuthorID || d.Version < 1 {
		return false
	}
	if d.PriceMinor < 0 || d.Currency != currencyCNY {
		return false
	}
	switch d.ItemCondition {
	case ItemConditionNew, ItemConditionLikeNew, ItemConditionGood, ItemConditionFair:
	default:
		return false
	}
	switch d.TradeMethod {
	case TradeMethodInPerson, TradeMethodCampusDropoff, TradeMethodOther:
	default:
		return false
	}
	switch d.TradeStatus {
	case TradeStatusAvailable, TradeStatusReserved, TradeStatusSold, TradeStatusClosed:
	default:
		return false
	}
	return true
}

func nilFeatureReadPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

func (s *Service) GetForPrincipal(ctx context.Context, id string, resolve identityport.SessionPrincipalResolver) (*Result, error) {
	var selected *domain.Thread
	return identityport.ReadBoundProjection(ctx, resolve, true,
		func(ctx context.Context, p identityport.PrincipalContext) (featureReadSnapshot, error) {
			if err := s.ensureEnabled(); err != nil {
				return featureReadSnapshot{}, err
			}
			query, ok := s.query.(communityport.AuthenticatedThreadQuery)
			if !ok || nilFeatureReadPort(query) {
				return featureReadSnapshot{}, identityport.ErrPolicyDecisionMismatch
			}
			thread, err := query.GetThreadForPrincipal(ctx, id, identityport.SessionPrincipalResolverFromContext(ctx))
			if err != nil {
				return featureReadSnapshot{}, normalizeCommunityError(err)
			}
			if thread == nil || thread.ID != id || thread.ThreadType != domain.ThreadTypeSecondhand || thread.DeletionStatus != domain.DeletionStatusActive || thread.FactsVersion < 1 || (p.Actor.Kind == "user" && p.Actor.ID != thread.AuthorID) {
				return featureReadSnapshot{}, ErrNotFound
			}
			detail, err := s.store.Get(ctx, id)
			if err != nil {
				return featureReadSnapshot{}, normalizeStoreError(err)
			}
			if !validFeatureReadDetail(detail, thread) {
				return featureReadSnapshot{}, identityport.ErrPolicyDecisionMismatch
			}
			selected = thread
			return featureReadSnapshot{ThreadID: id, ThreadVersion: thread.FactsVersion, Detail: cloneDetail(detail)}, nil
		},
		func(_ context.Context, _ identityport.PrincipalContext, snapshot featureReadSnapshot) (*Result, error) {
			return &Result{Thread: selected, Detail: cloneDetail(snapshot.Detail)}, nil
		}, nil)
}

type featurePageSnapshot struct {
	Total int64
	Rows  []featureReadSnapshot
}
type featurePageResult struct {
	Total int64
	Rows  []*Result
}

func (s *Service) ListPublicChecked(ctx context.Context, filter domain.ThreadListFilter) ([]*Result, int64, error) {
	var selected []*Result
	result, err := identityport.ReadBoundProjection(ctx, nil, true,
		func(ctx context.Context, _ identityport.PrincipalContext) (featurePageSnapshot, error) {
			if _, ok := s.query.(communityport.AuthenticatedThreadQuery); !ok || nilFeatureReadPort(s.query) {
				return featurePageSnapshot{}, identityport.ErrPolicyDecisionMismatch
			}
			rows, total, err := s.ListPublic(ctx, filter)
			if err != nil {
				return featurePageSnapshot{}, err
			}
			if total < 0 || total < int64(len(rows)) {
				return featurePageSnapshot{}, identityport.ErrPolicyDecisionMismatch
			}
			snapshot := featurePageSnapshot{Total: total, Rows: make([]featureReadSnapshot, 0, len(rows))}
			seen := make(map[string]bool, len(rows))
			for _, row := range rows {
				if row == nil || row.Thread == nil || row.Thread.ThreadType != domain.ThreadTypeSecondhand || row.Thread.FactsVersion < 1 || row.Thread.PublicationStatus != domain.PublicationStatusPublished || row.Thread.ModerationStatus != domain.ModerationStatusClear || row.Thread.DeletionStatus != domain.DeletionStatusActive || seen[row.Thread.ID] || !validFeatureReadDetail(row.Detail, row.Thread) {
					return featurePageSnapshot{}, identityport.ErrPolicyDecisionMismatch
				}
				seen[row.Thread.ID] = true
				snapshot.Rows = append(snapshot.Rows, featureReadSnapshot{ThreadID: row.Thread.ID, ThreadVersion: row.Thread.FactsVersion, Detail: cloneDetail(row.Detail)})
			}
			selected = rows
			return snapshot, nil
		}, func(_ context.Context, _ identityport.PrincipalContext, snapshot featurePageSnapshot) (featurePageResult, error) {
			return featurePageResult{Total: snapshot.Total, Rows: selected}, nil
		}, nil)
	if err != nil {
		return nil, 0, err
	}
	return result.Rows, result.Total, nil
}
