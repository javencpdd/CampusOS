package repository

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestSourceReadBatchBoundsAndRequiresTransaction(t *testing.T) {
	for _, ids := range [][]string{{"0"}, {"01"}, {"+1"}, {"1 OR 1=1"}, {strings.Repeat("9", 40)}, make([]string, 1001)} {
		if _, e := sourceReadIDs(ids); e == nil {
			t.Fatal("invalid source batch accepted")
		}
	}
	if ids, e := sourceReadIDs([]string{"10", "2", "10"}); e != nil || !reflect.DeepEqual(ids, []int64{2, 10}) {
		t.Fatal(ids, e)
	}
	called := false
	read := func(context.Context) error { called = true; return nil }
	if e := NewPgThreadRepository(nil).WithSourceThreadReadSnapshot(context.Background(), []string{"1"}, read); e == nil {
		t.Fatal("source snapshot outside transaction")
	}
	if e := NewPgCategoryRepository(nil).WithSourceCategoryReadSnapshot(context.Background(), read); e == nil {
		t.Fatal("category snapshot outside transaction")
	}
	if called {
		t.Fatal("unlocked callback executed")
	}
}
