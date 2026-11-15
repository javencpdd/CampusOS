package plugin

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestClientReadIsOptionalAndRequiresItsOwnGrant(t *testing.T) {
	v, user, h := newUIActionClientFixture(t, false)
	ctx := context.Background()
	facts, err := v.s.UI(ctx, v.c, false, user)
	if err != nil {
		t.Fatal(err)
	}
	status, err := h.uiAction(ctx, v.c, V5UIActionRequest{Proof: facts.Proof, Action: "web.clients.status"}, user)
	if err != nil || status.LocalAvailable || len(status.Clients) != 0 {
		t.Fatal("optional capability did not fail closed", err)
	}
	if _, err = h.uiAction(ctx, v.c, V5UIActionRequest{Proof: facts.Proof, Action: "web.clients.pair"}, user); err == nil {
		t.Fatal("unapproved client pairing")
	}
}
func TestClientLeaseSurvivesConfigurationButCannotReviveAfterLifecycleChange(t *testing.T) {
	v, user, h := newUIActionFixture(t)
	ctx := context.Background()
	facts, err := v.s.UI(ctx, v.c, false, user)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := h.uiAction(ctx, v.c, V5UIActionRequest{Proof: facts.Proof, Action: "web.clients.pair"}, user)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := h.clients().ConnectWithMode(ctx, pair.PairingCode, "User PC", "socks5")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := v.s.configuration(ctx, v.c, "1001", v.manifest)
	v.mutate(t, V5Mutation{Kind: "configure", ExpectedRevision: cfg.Revision, Values: cfg.Values, ConfigurationPart: "values", IdempotencyKey: "client-config"}, false, user)
	fresh, err := v.s.UI(ctx, v.c, false, user)
	if err != nil || fresh.Proof == facts.Proof || fresh.ClientProof != facts.ClientProof || h.clients().Validate(ctx, token) != nil {
		t.Fatal("ordinary configuration killed the native connection", err)
	}
	own, _ := v.s.installation(ctx, v.c, "1001")
	v.mutate(t, V5Mutation{Kind: "disable", ExpectedGeneration: own.Generation, IdempotencyKey: "client-off"}, false, user)
	own, _ = v.s.installation(ctx, v.c, "1001")
	v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: own.Generation, IdempotencyKey: "client-on"}, false, user)
	if h.clients().Validate(ctx, token) == nil {
		t.Fatal("old native token revived after disable and enable")
	}
}
func TestLocalReadReturnsUntrustedBytesAndNeverFallsBackToServer(t *testing.T) {
	v, user, h := newUIActionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	facts, err := v.s.UI(ctx, v.c, false, user)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := h.uiAction(ctx, v.c, V5UIActionRequest{Proof: facts.Proof, Action: "web.clients.pair"}, user)
	if err != nil {
		t.Fatal(err)
	}
	token, client, err := h.clients().ConnectWithMode(ctx, pair.PairingCode, "Private PC", "http")
	if err != nil {
		t.Fatal(err)
	}
	h.clientTarget = func(context.Context, string) error { return nil }
	serverReads := 0
	h.publicRead = func(context.Context, string, int64) ([]byte, string, error) {
		serverReads++
		return []byte("server"), "text/plain", nil
	}
	done := make(chan V5UIActionResult, 1)
	failed := make(chan error, 1)
	q := V5UIActionRequest{Proof: facts.Proof, Action: "web.read", URL: "https://public.example/document", Kind: "document", ClientID: client.ID}
	go func() { result, err := h.uiAction(ctx, v.c, q, user); done <- result; failed <- err }()
	job, err := h.clients().Poll(ctx, token)
	if err != nil || job == nil {
		t.Fatal("local job missing", err)
	}
	if err = h.clients().Complete(ctx, token, job.ID, []byte("local public bytes"), false); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if err = <-failed; err != nil || string(result.Bytes) != "local public bytes" || serverReads != 0 {
		t.Fatal("wrong reader", err)
	}
	owner, _ := h.clientOwner(ctx, v.c, user)
	if err = h.clients().Disconnect(owner, client.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = h.uiAction(ctx, v.c, q, user); err == nil || serverReads != 0 {
		t.Fatal("lost client silently used server")
	}
	q.ClientID = strings.Repeat("b", 32)
	if _, err = h.uiAction(ctx, v.c, q, user); err == nil || serverReads != 0 {
		t.Fatal("unknown client silently used server")
	}
}
