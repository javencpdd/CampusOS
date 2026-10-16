package portadapt

import (
	"context"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"testing"
)

func TestSpaceAdminTargetUserReaderRequiresTransactionAndCurrentOwningProjection(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryUserRepository()
	user := &identitydomain.User{ID: "1001", Username: "owner", Email: "owner@example.test", Status: "suspended", MustChangePassword: true}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	reader := NewRepositoryUserReader(repo)
	if _, err := reader.GetUserForUpdate(ctx, "1001"); err == nil {
		t.Fatal("unretained target read accepted")
	}
	var nilRepo *repository.PgUserRepository
	err := transaction.NewMemory(repo).Within(ctx, func(tx context.Context) error {
		for _, source := range []*RepositoryUserReader{nil, NewRepositoryUserReader(nilRepo)} {
			if _, err := source.GetUserForUpdate(tx, "1001"); err == nil {
				t.Fatal("missing adapter accepted")
			}
		}
		u, err := reader.GetUserForUpdate(tx, "1001")
		if err != nil || u.ID != "1001" || u.Email != "owner@example.test" || u.Status != "suspended" {
			t.Fatal(u, err)
		}
		user.Email = "changed@example.test"
		if err := repo.Update(tx, user); err != nil {
			return err
		}
		current, err := reader.GetUserForUpdate(tx, "1001")
		if err != nil || current.Email != "changed@example.test" {
			t.Fatal(current, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
