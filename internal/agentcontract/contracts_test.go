package agentcontract

import "testing"

func TestRunnerRejectsHostCapabilities(t *testing.T) {
	for _, p := range []Permission{DatabaseAccess, JWTPrivateKey, UserToken, FullFilesystem} {
		if err := (RunnerManifest{ID: "r", Runtime: "grpc", Permissions: []Permission{p}}).Validate(); err == nil {
			t.Fatalf("permission %s was allowed", p)
		}
	}
}
func TestCampusOSCredentialsNeverExport(t *testing.T) {
	for _, c := range []SecretClass{CampusOSCredential, InfrastructureSecret} {
		d := ExportDecision(c, true, true)
		if d.Export || !d.Redact || !d.RequiresReauthentication {
			t.Fatalf("unsafe decision: %#v", d)
		}
	}
	if d := ExportDecision(ThirdPartySecret, false, true); d.Export {
		t.Fatal("plaintext third-party secret was exported")
	}
}
