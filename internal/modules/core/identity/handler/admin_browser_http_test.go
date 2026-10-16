package handler_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/handler"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/pkg/auth"
)

func adminSPAHTTPFixture(t *testing.T) *adminDomainHTTPFixture {
	f := newAdminDomainHTTPFixture(t)
	f.handler.SetSessionHTTPConfig(handler.SessionHTTPConfig{CookieSecure: true})
	ctx := context.Background()
	id := "adm.browser.opaque"
	if err := f.admins.CreateAccount(ctx, repository.AdminDomainAccount{ID: id, Username: "browser-operator", Email: "browser-operator@example.test", DisplayName: "独立管理员"}); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("browser-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.admins.SetCredentialCAS(ctx, id, hash, 0, time.Now()); err != nil || !ok {
		t.Fatal(err)
	}
	if err := f.admins.BindRole(ctx, repository.AdminDomainRoleBinding{ID: "binding.browser.opaque", AccountID: id, RoleID: "security_admin", GrantedBy: id}); err != nil {
		t.Fatal(err)
	}
	return f
}
func adminSPARequest(t *testing.T, f *adminDomainHTTPFixture, method, path, body, token string, cookies []*http.Cookie, csrf, browser bool) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1/admin/identity"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if browser {
		req.Header.Set("X-CampusOS-Client", "browser")
	}
	for _, c := range cookies {
		req.AddCookie(c)
		if csrf && c.Name == "campusos_admin_csrf" {
			req.Header.Set("X-Admin-CSRF-Token", c.Value)
		}
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	result := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	return w, result
}
func adminSPABrowserLogin(t *testing.T, f *adminDomainHTTPFixture) (string, []*http.Cookie) {
	t.Helper()
	w, result := adminSPARequest(t, f, "POST", "/login", `{"username":"browser-operator@example.test","password":"browser-password"}`, "", nil, false, true)
	if w.Code != 200 {
		t.Fatalf("independent login: %d", w.Code)
	}
	data := result["data"].(map[string]any)
	if _, found := data["refresh_token"]; found {
		t.Fatal("browser received raw refresh JSON")
	}
	return data["access_token"].(string), w.Result().Cookies()
}
func TestAdminSPABrowserHTTPCookiesCSRFIndependentCurrentSelfAndOwnLogout(t *testing.T) {
	f := adminSPAHTTPFixture(t)
	token, cookies := adminSPABrowserLogin(t, f)
	if len(cookies) != 2 {
		t.Fatal("Admin cookie count", len(cookies))
	}
	for _, c := range cookies {
		if !c.Secure || c.SameSite != http.SameSiteLaxMode || c.MaxAge < 86400 {
			t.Fatal("cookie lifetime/attributes")
		}
		switch c.Name {
		case "campusos_admin_refresh":
			if !c.HttpOnly || c.Path != "/api/v1/admin/identity" {
				t.Fatal("refresh cookie scope")
			}
		case "campusos_admin_csrf":
			if c.HttpOnly || c.Path != "/" {
				t.Fatal("CSRF cookie scope")
			}
		default:
			t.Fatal("User cookie changed")
		}
	}
	beforeA, beforeS, beforeM := f.admins.Snapshot(), f.sessionRepo.Snapshot(), f.security.Snapshot()
	w, result := adminSPARequest(t, f, "GET", "/me", "", token, nil, false, true)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("current self", w.Code)
	}
	data := result["data"].(map[string]any)
	if data["domain"] != "admin" || data["account"].(map[string]any)["id"] != "adm.browser.opaque" || data["authentication_strength"] != "password" {
		t.Fatal("wrong self domain")
	}
	for _, key := range []string{"email", "refresh", "credential", "nonce", "granted_by", "session_id"} {
		if bytes.Contains(w.Body.Bytes(), []byte(key)) {
			t.Fatal("private current metadata")
		}
	}
	if !reflect.DeepEqual(beforeA, f.admins.Snapshot()) || !reflect.DeepEqual(beforeS, f.sessionRepo.Snapshot()) || !reflect.DeepEqual(beforeM, f.security.Snapshot()) {
		t.Fatal("self read wrote data")
	}
	userToken, err := f.userJWT.GenerateAccessToken("adm.browser.opaque", "browser-operator", auth.AccessTokenContext{SessionID: "user-alias", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	w, _ = adminSPARequest(t, f, "GET", "/me", "", userToken, nil, false, true)
	if w.Code != 401 {
		t.Fatal("same-ID User alias accepted", w.Code)
	}
	w, _ = adminSPARequest(t, f, "GET", "/me", "{}", token, nil, false, true)
	if w.Code != 400 {
		t.Fatal("GET body accepted")
	}
	w, _ = adminSPARequest(t, f, "POST", "/refresh", "{}", "", cookies, false, true)
	if w.Code != 403 || len(w.Result().Cookies()) != 0 {
		t.Fatal("CSRF missing did not preserve session")
	}
	w, result = adminSPARequest(t, f, "POST", "/refresh", "{}", "", cookies, true, true)
	if w.Code != 200 {
		t.Fatal("cookie rotation", w.Code)
	}
	newToken := result["data"].(map[string]any)["access_token"].(string)
	newCookies := w.Result().Cookies()
	w, _ = adminSPARequest(t, f, "GET", "/me", "", token, nil, false, true)
	if w.Code != 401 {
		t.Fatal("old access survived rotation")
	}
	w, _ = adminSPARequest(t, f, "POST", "/logout", "{}", newToken, newCookies, false, true)
	if w.Code != 403 {
		t.Fatal("logout without Admin CSRF")
	}
	w, _ = adminSPARequest(t, f, "POST", "/logout", "{}", newToken, newCookies, true, true)
	if w.Code != 200 {
		t.Fatal("own logout", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.MaxAge != -1 || !strings.HasPrefix(c.Name, "campusos_admin_") {
			t.Fatal("wrong cookies cleared")
		}
	}
	w, _ = adminSPARequest(t, f, "GET", "/me", "", newToken, nil, false, true)
	if w.Code != 401 {
		t.Fatal("logout did not revoke")
	}
}
func TestAdminSPABrowserHTTPNoBodyFallbackExplicitCLICompatibilityAndReuseFamily(t *testing.T) {
	f := adminSPAHTTPFixture(t)
	f.handler.SetSessionHTTPConfig(handler.SessionHTTPConfig{RefreshBodyCompat: true})
	w, result := adminSPARequest(t, f, "POST", "/login", `{"username":"browser-operator","password":"browser-password"}`, "", nil, false, false)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	raw := result["data"].(map[string]any)["refresh_token"].(string)
	cookies := w.Result().Cookies()
	for _, browser := range []bool{false, true} {
		w, _ = adminSPARequest(t, f, "POST", "/refresh", `{"refresh_token":"`+raw+`"}`, "", cookies, true, browser)
		if w.Code != 400 {
			t.Fatal("cookie request fell back to body", w.Code)
		}
	}
	w, result = adminSPARequest(t, f, "POST", "/refresh", `{"refresh_token":"`+raw+`"}`, "", nil, false, false)
	if w.Code != 200 {
		t.Fatal("explicit CLI rotation", w.Code)
	}
	current := result["data"].(map[string]any)["access_token"].(string)
	w, _ = adminSPARequest(t, f, "POST", "/refresh", `{"refresh_token":"`+raw+`"}`, "", nil, false, false)
	if w.Code != 401 {
		t.Fatal("reused refresh accepted")
	}
	w, _ = adminSPARequest(t, f, "GET", "/me", "", current, nil, false, true)
	if w.Code != 401 {
		t.Fatal("reuse revocation rolled back")
	}
	for _, signal := range []string{"Origin", "Sec-Fetch-Site"} {
		req := httptest.NewRequest("POST", "/api/v1/admin/identity/login", strings.NewReader(`{"username":"browser-operator","password":"browser-password"}`))
		req.Header.Set(signal, "same-origin")
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		if rec.Code != 200 || strings.Contains(rec.Body.String(), "refresh_token") {
			t.Fatal("browser signal exposed CLI credential")
		}
	}
}
func adminSPATOTP(secret string, step int64) string {
	value, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(step))
	mac := hmac.New(sha1.New, value)
	_, _ = mac.Write(message[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 15
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(digest[offset:offset+4])&0x7fffffff)%1000000)
}
func TestAdminSPABrowserHTTPPasswordSelfEnrollmentConfirmationAndCurrentStepUp(t *testing.T) {
	f := adminSPAHTTPFixture(t)
	token, cookies := adminSPABrowserLogin(t, f)
	w, _ := adminSPARequest(t, f, "POST", "/mfa/enroll", `{}`, token, cookies, false, true)
	if w.Code != 403 {
		t.Fatal("factor enrolled without CSRF")
	}
	w, result := adminSPARequest(t, f, "POST", "/mfa/enroll", `{"issuer":"CampusOS"}`, token, cookies, true, true)
	if w.Code != 200 {
		t.Fatal("password self enrollment", w.Code)
	}
	uri, err := url.Parse(result["data"].(map[string]any)["otpauth_url"].(string))
	if err != nil {
		t.Fatal("invalid enrollment URI")
	}
	secret := uri.Query().Get("secret")
	step := time.Now().Unix() / 30
	code := adminSPATOTP(secret, step)
	w, _ = adminSPARequest(t, f, "POST", "/mfa/confirm", `{"code":"`+code+`"}`, token, cookies, true, true)
	if w.Code != 200 {
		t.Fatal("factor confirmation", w.Code)
	}
	w, _ = adminSPARequest(t, f, "POST", "/mfa/step-up", `{"code":"`+code+`"}`, token, cookies, true, true)
	if w.Code != 409 {
		t.Fatal("confirm code replay accepted", w.Code)
	}
	w, _ = adminSPARequest(t, f, "POST", "/mfa/step-up", `{"code":"`+adminSPATOTP(secret, step+1)+`"}`, token, cookies, true, true)
	if w.Code != 200 {
		t.Fatal("actual password step-up", w.Code)
	}
	w, result = adminSPARequest(t, f, "GET", "/me", "", token, nil, false, true)
	if w.Code != 200 || result["data"].(map[string]any)["authentication_strength"] != "mfa" || result["data"].(map[string]any)["mfa_enrolled"] != true {
		t.Fatal("current strength not visible")
	}
}
