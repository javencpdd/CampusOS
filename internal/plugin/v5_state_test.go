package plugin

import (
	"context"
	"testing"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestV5StateRollbackAndVisiblePagination(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryV5StateStore()
	tx := transaction.NewMemory(s)
	n, _ := s.next(ctx)
	if err := tx.Within(ctx, func(ctx context.Context) error {
		for _, key := range []string{"aaa", "bbb", "ccc"} {
			c := V5Coordinate{"campusos", key, "1.0.0"}
			i := V5Installation{Publisher: c.Publisher, Key: c.Key, Version: c.Version, Installed: true}
			if err := s.save(ctx, v5Global, v5StateID(c, ""), i); err != nil {
				return err
			}
		}
		s.next(ctx)
		return ErrV5Conflict
	}); err != ErrV5Conflict {
		t.Fatal(err)
	}
	next, _ := s.next(ctx)
	if next <= n+1 {
		t.Fatal("generation reused on rollback")
	}
	rows, _ := s.list(ctx, v5Global, v5Query{})
	if len(rows) != 0 {
		t.Fatal("business rows survived failed transaction")
	}
	for _, key := range []string{"aaa", "bbb", "ccc"} {
		c := V5Coordinate{"campusos", key, "1.0.0"}
		s.save(ctx, v5Global, v5StateID(c, ""), V5Installation{Publisher: c.Publisher, Key: c.Key, Version: c.Version, Installed: true})
		s.save(ctx, v5Grant, c.id(), V5Grant{V5Coordinate: c, Approved: key == "ccc"})
	}
	rows, err := s.list(ctx, v5Global, v5Query{Visible: true, Limit: 1})
	if err != nil || len(rows) != 1 {
		t.Fatal("visibility not filtered before limit", err)
	}
	var i V5Installation
	if err := s.load(ctx, v5Global, "campusos/ccc#system", &i); err != nil {
		t.Fatal(err)
	}
	i.Key = "detached"
	var again V5Installation
	s.load(ctx, v5Global, "campusos/ccc#system", &again)
	if again.Key != "ccc" {
		t.Fatal("mutable read alias")
	}
}
