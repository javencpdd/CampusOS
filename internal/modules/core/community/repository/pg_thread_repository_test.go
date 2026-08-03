package repository

import (
	"reflect"
	"testing"
)

func TestParseBigIntIDsForPostgresCategoryArray(t *testing.T) {
	got, err := parseBigIntIDs([]string{"1785430481912942767", " 1785430486658849320 "})
	if err != nil {
		t.Fatalf("parse category IDs: %v", err)
	}
	want := []int64{1785430481912942767, 1785430486658849320}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected category IDs: got %v want %v", got, want)
	}
}

func TestParseBigIntIDsRejectsInvalidCategoryID(t *testing.T) {
	for _, values := range [][]string{{"not-an-id"}, {"0"}, {""}} {
		if _, err := parseBigIntIDs(values); err == nil {
			t.Fatalf("expected invalid category IDs %v to fail", values)
		}
	}
}

func TestParseBigIntIDsKeepsEmptySetForEmptyGroup(t *testing.T) {
	got, err := parseBigIntIDs([]string{})
	if err != nil {
		t.Fatalf("parse empty category set: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("empty group must remain a non-nil empty set, got %#v", got)
	}
}
