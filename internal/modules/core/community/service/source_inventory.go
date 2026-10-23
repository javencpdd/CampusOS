package service

import (
	"context"

	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func (s *ThreadService) SourceThreadIDs(ctx context.Context, board, after string, limit int) ([]string, error) {
	reader, ok := s.repo.(repository.SourceReferenceInventory)
	if !ok {
		return nil, identityport.ErrPolicyDecisionMismatch
	}
	return reader.SourceThreadIDs(ctx, board, after, limit)
}
