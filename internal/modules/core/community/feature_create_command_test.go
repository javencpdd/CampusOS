package community

import (
	"context"
	"errors"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
)

func TestFeatureThreadCreatorStoppedAdapters(t *testing.T) {
	for _, creator := range []communityport.AuthenticatedThreadCreator{(*moduleContentGateway)(nil), &moduleContentGateway{}, (*contentGateway)(nil), &contentGateway{}} {
		row, err := creator.CreateStructuredThreadForPrincipal(context.Background(), "", domain.CreateThreadRequest{}, communityport.ThreadCreateOptions{}, nil, nil)
		if row != nil || !errors.Is(err, communityport.ErrAuthorCommandUnavailable) {
			t.Fatal(row, err)
		}
	}
}

func TestFeatureAuthorCommandStoppedAdapters(t *testing.T) {
	for _, command := range []communityport.AuthenticatedThreadAuthorCommands{(*moduleContentGateway)(nil), &moduleContentGateway{}, (*contentGateway)(nil), &contentGateway{}} {
		if err := command.ExecuteThreadAuthorCommand(context.Background(), "id", "edit", "feature.test.update", "test.updated", nil, nil); !errors.Is(err, communityport.ErrAuthorCommandUnavailable) {
			t.Fatal(err)
		}
		preflight := command.(communityport.AuthenticatedThreadAuthorEditPreflight)
		if row, err := preflight.LoadThreadAuthorEditProof(context.Background(), "id", nil); row != nil || !errors.Is(err, communityport.ErrAuthorCommandUnavailable) {
			t.Fatal(row, err)
		}
	}
}
