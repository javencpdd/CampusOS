package server

import (
	"strings"
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

func TestAdminSeedCredentialPolicy(t *testing.T) {
	tests := []struct {
		name     string
		options  adminSeedOptions
		wantErr  bool
		wantHash bool
	}{
		{
			name:     "development compatibility is explicit",
			options:  adminSeedOptions{Environment: "development", PasswordHashEnabled: true, AllowDevelopmentDefaultAdmin: true},
			wantHash: true,
		},
		{
			name:    "production needs secret",
			options: adminSeedOptions{Environment: "production", PasswordHashEnabled: true},
			wantErr: true,
		},
		{
			name:     "secret rotates safely",
			options:  adminSeedOptions{Environment: "production", PasswordHashEnabled: true, BootstrapAdminSecret: "a-sufficiently-long-test-secret"},
			wantHash: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			credential, _, err := tt.options.credentialForBootstrap()
			if tt.wantErr {
				if err == nil {
					t.Fatal("credentialForBootstrap() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("credentialForBootstrap(): %v", err)
			}
			if strings.Contains(credential, defaultAdminPassword) {
				t.Fatal("stored credential unexpectedly contains the compatibility password")
			}
			if tt.wantHash && !strings.HasPrefix(credential, "$2") {
				t.Fatalf("credential is not a bcrypt hash: %q", credential)
			}
		})
	}
}
