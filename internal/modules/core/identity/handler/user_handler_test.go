package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	platformversion "github.com/campusos/CampusOS/internal/platform/version"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestUpdateUserAuthorizationAndFieldFiltering(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := repository.NewMemoryUserRepository()
	now := time.Now().UTC()
	for _, user := range []*domain.User{
		{ID: "1001", Username: "alice", Nickname: "Alice", Email: "alice@example.com", Status: domain.UserStatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: "1002", Username: "bob", Nickname: "Bob", Email: "bob@example.com", Status: domain.UserStatusActive, CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.Create(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	jwtMgr := auth.NewJWTManager(auth.JWTConfig{Secret: "test-secret", AccessTTL: time.Hour, Issuer: "test"})
	token, err := jwtMgr.GenerateAccessToken("1001", "alice")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewUserHandler(service.NewUserService(repo, jwtMgr, nil, nil))
	router := gin.New()
	router.Use(middleware.TraceID())
	router.PUT("/users/:id", middleware.JWTAuth(jwtMgr), handler.UpdateUser)
	router.GET("/users/:id", handler.GetUser)

	request := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if authenticated {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	if recorder := request(http.MethodPut, "/users/1002", `{"nickname":"Taken over"}`, true); recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-user update status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := request(http.MethodPut, "/users/1001", `{"nickname":"Alice 2","status":"suspended"}`, true); recorder.Code != http.StatusBadRequest {
		t.Fatalf("system field update status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := request(http.MethodPut, "/users/1001", `{"nickname":"Alice 2"}`, true); recorder.Code != http.StatusOK {
		t.Fatalf("self update status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := request(http.MethodPut, "/users/1001", `{"nickname":"Alice 3"}`, false); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous update status = %d: %s", recorder.Code, recorder.Body.String())
	}

	public := request(http.MethodGet, "/users/1001", "", false)
	if public.Code != http.StatusOK {
		t.Fatalf("public profile status = %d: %s", public.Code, public.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(public.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(public.Body.String(), "alice@example.com") {
		t.Fatalf("public profile leaked email: %s", public.Body.String())
	}
	if payload["request_id"] == "" {
		t.Fatalf("public response lacks request_id: %s", public.Body.String())
	}
}

func TestHealthCheckUsesApplicationVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewUserHandler(nil)
	router := gin.New()
	router.GET("/health", handler.HealthCheck)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Version != platformversion.Number {
		t.Fatalf("health version=%q want=%q", payload.Data.Version, platformversion.Number)
	}
}

func TestVerifiedRegistrationHTTPFlowAndLegacyRequestRejection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := repository.NewMemoryUserRepository()
	challengesStore := repository.NewMemoryChallengeRepository()
	challenges, err := service.NewChallengeService(challengesStore, service.ChallengeConfig{
		ActiveKeyID:  "test-v1",
		HMACKeys:     map[string]string{"test-v1": "test-registration-hmac-key"},
		IPHashSecret: "test-registration-ip-hash-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	reliable := reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
	challenges.SetReliability(reliable)
	jwtMgr := auth.NewJWTManager(auth.JWTConfig{Secret: "test-secret", AccessTTL: time.Hour, RefreshTTL: time.Hour, Issuer: "test"})
	usersService := service.NewUserService(users, jwtMgr, users, nil)
	usersService.SetReliability(reliable)
	usersService.SetRegistrationTicketConsumer(challenges)
	handler := NewUserHandler(usersService)
	handler.SetChallengeService(challenges)
	router := gin.New()
	router.POST("/auth/registration/challenge", handler.RequestRegistrationChallenge)
	router.POST("/auth/registration/verify", handler.VerifyRegistrationChallenge)
	router.POST("/auth/register", handler.Register)

	request := func(path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		return recorder
	}

	legacy := request("/auth/register", `{"username":"legacy_path","nickname":"Legacy Path","email":"legacy-path@example.test","password":"Secret123"}`)
	if legacy.Code != http.StatusBadRequest || !strings.Contains(legacy.Body.String(), `"identity.verification_required"`) {
		t.Fatalf("legacy registration response=%d %s", legacy.Code, legacy.Body.String())
	}
	if _, err := users.GetByEmail(context.Background(), "legacy-path@example.test"); !errors.Is(err, repository.ErrUserNotFound) {
		t.Fatalf("legacy request created a user: %v", err)
	}

	challengeResponse := request("/auth/registration/challenge", `{"email":"new-user@example.test"}`)
	if challengeResponse.Code != http.StatusOK {
		t.Fatalf("challenge status=%d body=%s", challengeResponse.Code, challengeResponse.Body.String())
	}
	var challengePayload struct {
		Data domain.ChallengeReceipt `json:"data"`
	}
	if err := json.Unmarshal(challengeResponse.Body.Bytes(), &challengePayload); err != nil {
		t.Fatal(err)
	}
	challenge, err := challengesStore.GetChallenge(context.Background(), challengePayload.Data.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := challenges.Dispatch(context.Background(), challenge.ID)
	if err != nil {
		t.Fatal(err)
	}
	verifyResponse := request("/auth/registration/verify", `{"challenge_id":"`+challengePayload.Data.PublicID+`","code":"`+dispatch.Code+`"}`)
	if verifyResponse.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verifyResponse.Code, verifyResponse.Body.String())
	}
	var ticketPayload struct {
		Data domain.ChallengeTicket `json:"data"`
	}
	if err := json.Unmarshal(verifyResponse.Body.Bytes(), &ticketPayload); err != nil {
		t.Fatal(err)
	}
	registrationBody := `{"username":"verified_user","nickname":"Verified User","email":"new-user@example.test","password":"Secret123","challenge_id":"` + challengePayload.Data.PublicID + `","ticket":"` + ticketPayload.Data.Ticket + `"}`
	registered := request("/auth/register", registrationBody)
	if registered.Code != http.StatusCreated {
		t.Fatalf("registration status=%d body=%s", registered.Code, registered.Body.String())
	}
	user, err := users.GetByEmail(context.Background(), "new-user@example.test")
	if err != nil {
		t.Fatal(err)
	}
	account, err := usersService.GetEmailAccount(context.Background(), user.ID)
	if err != nil || account.VerificationState != domain.VerificationStateVerified {
		t.Fatalf("verified account=%#v err=%v", account, err)
	}
	if reused := request("/auth/register", registrationBody); reused.Code != http.StatusBadRequest || !strings.Contains(reused.Body.String(), `"identity.registration_verification_invalid"`) {
		t.Fatalf("ticket reuse response=%d %s", reused.Code, reused.Body.String())
	}
}

func TestRegistrationChallengeInfrastructureFailureIsRetryableAndRedacted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	challenges, err := service.NewChallengeService(repository.NewMemoryChallengeRepository(), service.ChallengeConfig{
		ActiveKeyID:  "test-v1",
		HMACKeys:     map[string]string{"test-v1": "test-registration-hmac-key"},
		IPHashSecret: "test-registration-ip-hash-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	challenges.SetPolicyReader(failingChallengePolicyReader{})
	handler := NewUserHandler(nil)
	handler.SetChallengeService(challenges)
	router := gin.New()
	router.Use(middleware.TraceID())
	router.POST("/auth/registration/challenge", handler.RequestRegistrationChallenge)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/auth/registration/challenge", strings.NewReader(`{"email":"student@example.test"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("infrastructure failure status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"retryable":true`) || !strings.Contains(recorder.Body.String(), `"internal.error"`) {
		t.Fatalf("infrastructure failure is not safely retryable: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "policy storage failure") || strings.Contains(recorder.Body.String(), "student@example.test") {
		t.Fatalf("infrastructure failure leaked internal or recipient data: %s", recorder.Body.String())
	}
}

type failingChallengePolicyReader struct{}

func (failingChallengePolicyReader) GetChallengePolicy(context.Context) (*domain.ChallengePolicy, error) {
	return nil, errors.New("policy storage failure")
}
