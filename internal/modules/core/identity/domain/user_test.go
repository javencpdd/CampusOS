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

func TestUserProjectionsKeepEmailInsideAdminBoundary(t *testing.T) {
	user := &User{ID: "1001", Username: "alice", Nickname: "Alice", Email: " Alice@Example.TEST ", Status: UserStatusActive}
	if got := user.Public(); got.Username != "alice" {
		t.Fatalf("unexpected public projection: %#v", got)
	}
	if got := user.Admin().Email; got != "alice@example.test" {
		t.Fatalf("admin email=%q", got)
	}
	user.Email = LegacySharedPlaceholderEmail
	if got := user.Admin().Email; got != "" {
		t.Fatalf("reserved compatibility email leaked to admin directory: %q", got)
	}
}
