package domain

import "testing"

func TestNormalizeEmailAndReservedPlaceholder(t *testing.T) {
	if got := NormalizeEmail("  Name@Example.TEST "); got != "name@example.test" {
		t.Fatalf("NormalizeEmail()=%q", got)
	}
	if !IsReservedEmail(" 1904650862@QQ.COM ") {
		t.Fatal("legacy placeholder should be reserved after normalization")
	}
	if IsReservedEmail("student@example.test") {
		t.Fatal("ordinary email must not be reserved")
	}
}
