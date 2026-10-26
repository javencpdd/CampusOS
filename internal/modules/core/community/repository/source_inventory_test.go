package repository

import (
	"reflect"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

func TestSourceInventoryNumericKeysetAndNoReadGrant(t *testing.T) {
	r := NewMemoryThreadRepository()
	for _, id := range []string{"2", "10", "11", "100"} {
		if e := r.Create(t.Context(), &domain.Thread{ID: id, CategoryID: "91", AuthorID: "1", Content: "PRIVATE", PublicationStatus: domain.PublicationStatusPrivate}); e != nil {
			t.Fatal(e)
		}
	}
	first, e := r.SourceThreadIDs(t.Context(), "91", "", 2)
	if e != nil || !reflect.DeepEqual(first, []string{"2", "10"}) {
		t.Fatal(first, e)
	}
	last, e := r.SourceThreadIDs(t.Context(), "91", first[1], 2)
	if e != nil || !reflect.DeepEqual(last, []string{"11", "100"}) {
		t.Fatal(last, e)
	}
	for _, tc := range []struct {
		b, a string
		n    int
	}{{"091", "", 1}, {"91", "0", 1}, {"91", "", 34}, {"91", "", 0}} {
		if _, e := r.SourceThreadIDs(t.Context(), tc.b, tc.a, tc.n); e == nil {
			t.Fatal("invalid inventory scope accepted", tc)
		}
	}
}
