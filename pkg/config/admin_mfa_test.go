package config

import "testing"

func TestAdminMFAOnlyExplicitFalseDisablesRequirement(t *testing.T) {
	for _, value := range []string{"", "true", "false", " FALSE ", "0", "off", "invalid"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("AUTH_ADMIN_MFA_REQUIRED", value)
			want := value == "false" || value == " FALSE "
			if got := Load().Auth.AdminMFADisabled; got != want {
				t.Fatalf("MFA disabled=%v want %v", got, want)
			}
		})
	}
}
