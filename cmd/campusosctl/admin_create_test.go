package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestLocalAdminCreateInputIsBoundedAndStrict(t *testing.T) {
	for _, input := range []string{
		`{"username":"operator","email":"operator@example.test","password":"fixture-password","proof":"unexpected"}`,
		`{"username":"operator","email":"operator@example.test","password":"fixture-password"} {}`,
		`{"username":"operator","email":"operator@example.test","password":""}`,
		strings.Repeat("x", 4097),
	} {
		if _, err := decodeLocalAdminCreate(strings.NewReader(input)); err == nil {
			t.Fatal("invalid local provisioning input was accepted")
		}
	}
	request, err := decodeLocalAdminCreate(strings.NewReader(`{"username":"operator","email":"operator@example.test","password":"fixture-password"}`))
	if err != nil || request.Username != "operator" || request.Password != "fixture-password" {
		t.Fatal("valid fixture input was rejected")
	}
	var output bytes.Buffer
	if err := runLocalAdminCreate([]string{"--password", "fixture-secret"}, &output, nil); err == nil || strings.Contains(err.Error(), "fixture-secret") || output.Len() != 0 {
		t.Fatal("password flags must be rejected without echoing values")
	}
}
