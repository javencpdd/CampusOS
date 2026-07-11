package request

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type strictPayload struct {
	Name string `json:"name" binding:"required"`
}

func TestBindJSONStrict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "valid", body: `{"name":"CampusOS"}`},
		{name: "unknown field", body: `{"name":"CampusOS","status":"admin"}`, wantErr: true},
		{name: "missing required", body: `{}`, wantErr: true},
		{name: "trailing value", body: `{"name":"CampusOS"}{}`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/", strings.NewReader(test.body))
			var payload strictPayload
			err := BindJSONStrict(ctx, &payload)
			if (err != nil) != test.wantErr {
				t.Fatalf("BindJSONStrict() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestBindJSONStrictOptionalAcceptsOnlyEmptyOrValidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		body    string
		wantErr bool
	}{
		{body: ""},
		{body: `{"name":"CampusOS"}`},
		{body: `{"unknown":true}`, wantErr: true},
	} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", "/", strings.NewReader(test.body))
		var payload strictPayload
		err := BindJSONStrictOptional(ctx, &payload)
		if (err != nil) != test.wantErr {
			t.Fatalf("body %q: error = %v, wantErr %v", test.body, err, test.wantErr)
		}
	}
}
