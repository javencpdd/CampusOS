package server

import (
	"testing"

	"github.com/campusos/CampusOS/pkg/auth"
)

func TestIsDefaultAdminCredential(t *testing.T) {
	hash, err := auth.HashPassword(defaultAdminPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	tests := []struct {
		name       string
		credential string
		want       bool
	}{
		{name: "plaintext default", credential: defaultAdminPassword, want: true},
		{name: "bcrypt default", credential: hash, want: true},
		{name: "legacy bad hash", credential: legacyAdminBadHash, want: true},
		{name: "different plaintext", credential: "other-password", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDefaultAdminCredential(tt.credential); got != tt.want {
				t.Fatalf("isDefaultAdminCredential() = %v, want %v", got, tt.want)
			}
		})
	}
}
