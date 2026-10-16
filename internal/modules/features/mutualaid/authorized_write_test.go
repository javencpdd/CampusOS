package mutualaid

import (
	"context"
	"errors"
	"reflect"
	"testing"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type rawAuthorCommands struct{ communityport.ContentGateway }

func typedAuthorUpdateRequest() UpdateRequest {
	return UpdateRequest{Title: "Updated title", Content: "Updated body", AidType: AidTypeOffer, LocationScope: "After", ContactMode: ContactModeComment, Version: 1}
}
func TestMutualAidAuthorWriteUpdatePolicyAndFacts(t *testing.T) { runTypedAuthorWrite(t, false) }
func TestMutualAidAuthorWriteStatusPolicyAndFacts(t *testing.T) { runTypedAuthorWrite(t, true) }
func runTypedAuthorWrite(t *testing.T, status bool) {
	for _, mode := range []string{"current", "other", "admin", "nil-proof", "missing-port", "trashed", "owner-aba", "detail-version", "credential", "audit"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, threads, cats, _, events, core := typedCreateFixture(t)
			created, err := s.Create(ctx, "1001", "Owner", validCreateRequest())
			if err != nil {
				t.Fatal(err)
			}
			var proof identityport.SessionPrincipalResolver = typedReadProof("1001")
			switch mode {
			case "other":
				proof = typedReadProof("1002")
			case "admin":
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, _ := typedReadProof("1001")(ctx)
					p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
					return p, nil
				}
			case "nil-proof":
				proof = nil
			case "missing-port":
				s.community = rawAuthorCommands{s.community}
			case "trashed":
				row, _ := threads.GetByID(ctx, created.Thread.ID)
				row.DeletionStatus = "trashed"
				if err := threads.Update(ctx, row); err != nil {
					t.Fatal(err)
				}
			}
			beforeThread := threads.Snapshot()
			persisted := s.store.(transaction.Snapshotter)
			beforeDetail, beforeEvents := persisted.Snapshot(), events.Snapshot()
			if mode == "audit" {
				d := reliability.NewService(transaction.NewMemory(cats, persisted, events), createFailAudit{events})
				core.SetReliability(d)
				s.SetReliability(d)
			}
			if mode == "owner-aba" || mode == "detail-version" || mode == "credential" {
				base := proof
				calls := 0
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					calls++
					p, _ := base(ctx)
					if calls == 2 {
						switch mode {
						case "owner-aba":
							row, _ := threads.GetByID(ctx, created.Thread.ID)
							row.AuthorID = "1002"
							threads.Update(ctx, row)
							row.AuthorID = "1001"
							threads.Update(ctx, row)
						case "detail-version":
							d, _ := s.store.Get(ctx, created.Thread.ID)
							if err := s.store.Update(ctx, d, 1); err != nil {
								t.Fatal(err)
							}
						case "credential":
							p.CredentialID = "changed"
						}
					}
					return p, nil
				}
			}
			var result *Result
			if status {
				result, err = s.UpdateStatusForPrincipal(ctx, created.Thread.ID, UpdateStatusRequest{AidStatus: AidStatusInProgress, Version: 1}, proof)
			} else {
				result, err = s.UpdateForPrincipal(ctx, created.Thread.ID, typedAuthorUpdateRequest(), proof)
			}
			if (err == nil) != (mode == "current") || (err != nil && result != nil) {
				t.Fatal(result, err)
			}
			if result != nil && (result.Detail.Version != 2 || result.Detail.CreatedBy != "1001") {
				t.Fatal("typed version/owner", result)
			}
			if mode == "audit" || mode == "credential" {
				if !reflect.DeepEqual(beforeThread, threads.Snapshot()) || !reflect.DeepEqual(beforeDetail, persisted.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
					t.Fatal("rollback lost")
				}
			}
			if mode == "nil-proof" && !errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable) {
				t.Fatal("proof fallback", err)
			}
		})
	}
}
