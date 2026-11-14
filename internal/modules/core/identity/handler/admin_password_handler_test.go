package handler_test

import (
	"net/http"
	"testing"
)

func TestAdminOwnPasswordChangeRequiresCSRFAndCurrentPasswordAndRevokesSessions(t *testing.T) {
	f := adminSPAHTTPFixture(t)
	token, cookies := adminSPABrowserLogin(t, f)
	otherToken, _ := adminSPABrowserLogin(t, f)
	body := `{"current_password":"browser-password","new_password":"changed-fixture-password"}`
	response, _ := adminSPARequest(t, f, "POST", "/password", body, token, cookies, false, true)
	if response.Code != http.StatusForbidden {
		t.Fatal("missing CSRF was accepted", response.Code)
	}
	response, _ = adminSPARequest(t, f, "POST", "/password", `{"current_password":"wrong-password","new_password":"changed-fixture-password"}`, token, cookies, true, true)
	if response.Code != http.StatusBadRequest {
		t.Fatal("wrong current password was accepted", response.Code)
	}
	response, _ = adminSPARequest(t, f, "GET", "/me", "", token, nil, false, true)
	if response.Code != http.StatusOK {
		t.Fatal("failed password change invalidated an existing session", response.Code)
	}
	response, _ = adminSPARequest(t, f, "POST", "/password", body, f.userJWTToken(t), nil, false, true)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("User-domain token changed an Admin password", response.Code)
	}
	response, _ = adminSPARequest(t, f, "POST", "/password", body, token, cookies, true, true)
	if response.Code != http.StatusOK {
		t.Fatal("valid password change", response.Code, response.Body.String())
	}
	for _, existing := range []string{token, otherToken} {
		response, _ = adminSPARequest(t, f, "GET", "/me", "", existing, nil, false, true)
		if response.Code != http.StatusUnauthorized {
			t.Fatal("old Admin session survived password change", response.Code)
		}
	}
	response, _ = adminSPARequest(t, f, "POST", "/login", `{"username":"browser-operator","password":"browser-password"}`, "", nil, false, true)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("old password still works", response.Code)
	}
	response, _ = adminSPARequest(t, f, "POST", "/login", `{"username":"browser-operator","password":"changed-fixture-password"}`, "", nil, false, true)
	if response.Code != http.StatusOK {
		t.Fatal("new password does not work", response.Code)
	}
}

func (f *adminDomainHTTPFixture) userJWTToken(t *testing.T) string {
	t.Helper()
	token, err := f.userJWT.GenerateAccessToken("user-fixture", "user-fixture")
	if err != nil {
		t.Fatal(err)
	}
	return token
}
