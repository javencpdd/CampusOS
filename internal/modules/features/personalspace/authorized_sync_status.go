package space

import (
	"context"
	"errors"
	"reflect"
	"strings"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

func safeSpaceSyncError(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "同步未完成，请重试；如仍失败，请联系管理员。"
}

type currentSpaceSyncSnapshot struct {
	Facts *spaceAdminFacts
	Total int64
}

func (s *Service) GetSyncStatusForPrincipal(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*SpaceSyncStatus, error) {
	return identityport.ReadBoundProjection(ctx, resolve, false, func(ctx context.Context, p identityport.PrincipalContext) (currentSpaceSyncSnapshot, error) {
		f, err := s.loadCurrentReadSpace(ctx, "", false, false, "read_sync_status", p)
		if err != nil {
			return currentSpaceSyncSnapshot{}, err
		}
		reader, ok := s.repo.(OperationalRepository)
		if !ok || corestorage.NilImageAdapter(reader) {
			return currentSpaceSyncSnapshot{}, ErrSpaceFactsUnavailable
		}
		total, err := reader.CountContentsByUserID(ctx, p.Actor.ID)
		if err != nil || total < 0 {
			return currentSpaceSyncSnapshot{}, errors.Join(ErrSpaceFactsUnavailable, err)
		}
		current, err := s.loadCurrentReadSpace(ctx, "", false, false, "read_sync_status", p)
		if err != nil {
			return currentSpaceSyncSnapshot{}, err
		}
		if !reflect.DeepEqual(f, current) {
			return currentSpaceSyncSnapshot{}, identityport.ErrPolicyFactsChanged
		}
		return currentSpaceSyncSnapshot{Facts: f, Total: total}, nil
	}, func(_ context.Context, p identityport.PrincipalContext, snapshot currentSpaceSyncSnapshot) (*SpaceSyncStatus, error) {
		row := snapshot.Facts.Space.Space
		result := &SpaceSyncStatus{UserID: p.Actor.ID, SyncEnabled: row.SyncEnabled, LastSyncError: safeSpaceSyncError(row.LastSyncError), ContentTotal: snapshot.Total, Disabled: row.DisabledAt != nil}
		if row.LastSyncAt != nil {
			at := *row.LastSyncAt
			result.LastSyncAt = &at
		}
		return result, nil
	}, nil)
}
