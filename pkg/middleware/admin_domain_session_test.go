package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/gin-gonic/gin"
)

type adminDomainProofStub struct {
	facts AdminSessionFacts
	err   error
}

func (s adminDomainProofStub) VerifyAdminSession(context.Context, string) (AdminSessionFacts, error) {
	return s.facts, s.err
}

func TestAdminCurrentSessionJWTSubjectBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "admin-subject-binding-test-key", Issuer: "admin-subject-binding", Audience: "admin", AccessTTL: time.Hour})
	token, err := jwt.GenerateAccessToken("admin.subject", "subject", auth.AccessTokenContext{SessionID: "admin.session", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		facts AdminSessionFacts
		err   error
		want  int
	}{{"current", AdminSessionFacts{AccountID: "admin.subject", Strength: "password"}, nil, 200}, {"different-subject", AdminSessionFacts{AccountID: "admin.other", Strength: "password"}, nil, 401}, {"empty-subject", AdminSessionFacts{Strength: "password"}, nil, 401}, {"unknown-strength", AdminSessionFacts{AccountID: "admin.subject", Strength: "workload"}, nil, 401}, {"revoked", AdminSessionFacts{}, errors.New("revoked"), 401}} {
		t.Run(test.name, func(t *testing.T) {
			r := gin.New()
			r.Use(RequireAdminDomainAccess(jwt, adminDomainProofStub{test.facts, test.err}, nil))
			r.GET("/", func(c *gin.Context) {
				if c.GetString("admin_account_id") != "admin.subject" {
					t.Fatal("substituted actor")
				}
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Actor-ID", "admin.other")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != test.want {
				t.Fatal(rec.Code, rec.Body.String())
			}
		})
	}
}
