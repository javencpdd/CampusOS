package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
)

type v5ChatTestTransport func(context.Context, V5ChatTarget, campusossdk.V5ChatRequest, io.Writer) error

func (f v5ChatTestTransport) Chat(ctx context.Context, target V5ChatTarget, request campusossdk.V5ChatRequest, output io.Writer) error {
	return f(ctx, target, request, output)
}

func v5ChatReply(_ context.Context, _ V5ChatTarget, request campusossdk.V5ChatRequest, output io.Writer) error {
	return json.NewEncoder(output).Encode(campusossdk.V5ChatResponse{RequestID: request.RequestID, ContributionID: request.ContributionID,
		InstanceGeneration: request.InstanceGeneration, Text: "bounded reply", InputTokens: 2, OutputTokens: 3, FinishReason: "completed"})
}

func newV5ChatTestDispatcher(t *testing.T, transport V5ChatTransport) (v5TestPlatform, *V5ChatDispatcher, V5ContributionBinding, identity.SessionPrincipalResolver, *platformfeature.Registry) {
	t.Helper()
	v, _ := newV5ContributionTestPlatform(t)
	i := v5ContributionApproved(t, v)
	v.ready(t, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin))
	v.s.Users = &v5TestUsers{active: true}
	proof := v5TestProof("user", "1001")
	op := v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "user-install"}, false, proof)
	v.ready(t, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: op.Generation, IdempotencyKey: "user-enable"}, false, proof))
	directory := NewV5ContributionDirectory(v.s)
	binding, err := directory.Resolve(context.Background(), "1001", v5ContributionQueries()[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	features := platformfeature.NewAuthoritativeRegistry(platformfeature.NewMemoryStore())
	if err := features.Register(platformfeature.Definition{ID: "contract-chat-consumer", Mode: platformfeature.HotGated, DefaultEnabled: true}); err != nil {
		t.Fatal(err)
	}
	return v, NewV5ChatDispatcher(directory, features, "contract-chat-consumer", transport), binding, proof, features
}

func v5ChatTestRequest(b V5ContributionBinding) []byte {
	return mustV5JSON(campusossdk.V5ChatRequest{RequestID: "chat-1", ContributionID: b.Descriptor().ID, InstanceGeneration: b.Descriptor().InstanceGeneration,
		DeadlineMS: 500, Prompt: "caller supplied prompt", ContextClass: "none"})
}

func TestV5ChatDispatchPromptOnly(t *testing.T) {
	called := 0
	transport := v5ChatTestTransport(func(ctx context.Context, target V5ChatTarget, req campusossdk.V5ChatRequest, out io.Writer) error {
		called++
		if target.Owner != "1001" || target.Contribution.Point != V5AIChat || target.Contribution.InstanceGeneration != req.InstanceGeneration || req.ContextClass != "none" {
			t.Fatal("wrong host routing state", target)
		}
		return v5ChatReply(ctx, target, req, out)
	})
	_, d, b, proof, _ := newV5ChatTestDispatcher(t, transport)
	response, err := d.Chat(context.Background(), b, v5ChatTestRequest(b), proof)
	if err != nil || response.Text != "bounded reply" || called != 1 {
		t.Fatal("host-to-chat typed call failed", response, called, err)
	}
	if _, err := NewV5ChatDispatcher(d.directory, d.features, d.featureID, nil).Chat(context.Background(), b, v5ChatTestRequest(b), proof); err != ErrV5RuntimeMissing {
		t.Fatal("missing adapter fabricated a runtime", err)
	}
}

func TestV5ChatDispatchStrictBoundaries(t *testing.T) {
	called := 0
	transport := v5ChatTestTransport(func(ctx context.Context, target V5ChatTarget, req campusossdk.V5ChatRequest, out io.Writer) error {
		called++
		return v5ChatReply(ctx, target, req, out)
	})
	_, d, b, proof, _ := newV5ChatTestDispatcher(t, transport)
	good := string(v5ChatTestRequest(b))
	for _, tc := range []struct {
		name, raw string
		want      error
	}{
		{"unknown-field", strings.Replace(good, `"prompt":`, `"roles":["admin"],"prompt":`, 1), ErrV5CallInvalid},
		{"duplicate", strings.Replace(good, `"prompt":`, `"prompt":"hidden","prompt":`, 1), ErrV5CallInvalid},
		{"case-alias", strings.Replace(good, `"prompt":`, `"Prompt":`, 1), ErrV5CallInvalid},
		{"null", strings.Replace(good, `"prompt":"caller supplied prompt"`, `"prompt":null`, 1), ErrV5CallInvalid},
		{"invalid-unicode", strings.Replace(good, `caller supplied prompt`, `\ud800`, 1), ErrV5CallInvalid},
		{"private-context", strings.Replace(good, `"none"`, `"private"`, 1), ErrV5CallInvalid},
		{"public-context-needs-future-acl", strings.Replace(good, `"none"`, `"public"`, 1), ErrV5CallInvalid},
		{"prompt-limit", strings.Replace(good, `caller supplied prompt`, strings.Repeat("中", 12001), 1), ErrV5CallInvalid},
		{"large-wire", strings.Repeat(" ", V5ChatMaxRequestBytes+1), ErrV5ContributionBudget},
		{"multiple-documents", good + "{}", ErrV5CallInvalid},
		{"deadline-grant-ceiling", strings.Replace(good, `"deadline_ms":500`, `"deadline_ms":1001`, 1), ErrV5CallDeadline},
		{"target", strings.Replace(good, `"contribution_id":"chat"`, `"contribution_id":"other"`, 1), ErrV5CallTarget},
		{"stale-generation", strings.Replace(good, fmt.Sprintf(`"instance_generation":%d`, b.Descriptor().InstanceGeneration), `"instance_generation":1`, 1), ErrV5ContributionStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := d.Chat(context.Background(), b, []byte(tc.raw), proof)
			if err != tc.want || out.Text != "" {
				t.Fatal("invalid call admitted", err)
			}
		})
	}
	for _, other := range []identity.SessionPrincipalResolver{nil, v5TestProof("user", "1002"), v5TestProof("admin", "1001")} {
		if out, err := d.Chat(context.Background(), b, []byte(good), other); err != ErrV5Denied || out.Text != "" {
			t.Fatal("wrong owner/domain admitted", err)
		}
	}
	if called != 0 {
		t.Fatal("invalid input reached transport", called)
	}
}

func TestV5ChatDispatchResponseValidation(t *testing.T) {
	var response string
	transport := v5ChatTestTransport(func(_ context.Context, _ V5ChatTarget, _ campusossdk.V5ChatRequest, out io.Writer) error {
		// io.Copy must not obtain a promoted bytes.Buffer.ReadFrom bypass.
		_, _ = io.Copy(out, strings.NewReader(response))
		return nil // Even an adapter ignoring a writer error cannot hide it.
	})
	_, d, b, proof, _ := newV5ChatTestDispatcher(t, transport)
	good := string(mustV5JSON(campusossdk.V5ChatResponse{RequestID: "chat-1", ContributionID: b.Descriptor().ID, InstanceGeneration: b.Descriptor().InstanceGeneration,
		Text: "reply", InputTokens: 1, OutputTokens: 1, FinishReason: "completed"}))
	for _, tc := range []struct {
		name, raw string
		want      error
	}{
		{"wrong-request", strings.Replace(good, "chat-1", "other", 1), ErrV5CallTarget},
		{"wrong-target", strings.Replace(good, `"contribution_id":"chat"`, `"contribution_id":"other"`, 1), ErrV5CallTarget},
		{"wrong-generation", strings.Replace(good, fmt.Sprintf(`"instance_generation":%d`, b.Descriptor().InstanceGeneration), `"instance_generation":1`, 1), ErrV5CallTarget},
		{"duplicate", strings.Replace(good, `"text":`, `"text":"hidden","text":`, 1), ErrV5CallInvalid},
		{"case-alias", strings.Replace(good, `"text":`, `"Text":`, 1), ErrV5CallInvalid},
		{"null", strings.Replace(good, `"text":"reply"`, `"text":null`, 1), ErrV5CallInvalid},
		{"tokens", strings.Replace(good, `"input_tokens":1`, `"input_tokens":-1`, 1), ErrV5CallInvalid},
		{"finish-reason", strings.Replace(good, `"completed"`, `"tool_call"`, 1), ErrV5CallInvalid},
		{"text-size", strings.Replace(good, "reply", strings.Repeat("中", 50001), 1), ErrV5CallInvalid},
		{"wire-size", strings.Repeat("x", V5ChatMaxResponseBytes+1), ErrV5ContributionBudget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response = tc.raw
			out, err := d.Chat(context.Background(), b, v5ChatTestRequest(b), proof)
			if err != tc.want || out.Text != "" {
				t.Fatal("invalid response released", err)
			}
		})
	}
	d.transport = v5ChatTestTransport(func(context.Context, V5ChatTarget, campusossdk.V5ChatRequest, io.Writer) error {
		return errors.New("private adapter details must never escape")
	})
	if out, err := d.Chat(context.Background(), b, v5ChatTestRequest(b), proof); err != ErrV5RuntimeMissing || out.Text != "" {
		t.Fatal("transport details escaped", err)
	}
}

func TestV5ChatDispatchCurrentFactsBeforeAndAfter(t *testing.T) {
	for _, action := range []string{"feature-before", "feature-during", "session-before", "session-during", "credential-change", "revoke", "configure", "uninstall"} {
		t.Run(action, func(t *testing.T) {
			v, d, b, proof, features := newV5ChatTestDispatcher(t, v5ChatTestTransport(v5ChatReply))
			live := true
			changedCredential := false
			current := func(ctx context.Context) (identity.PrincipalContext, error) {
				if !live {
					return identity.PrincipalContext{}, errors.New("current session revoked")
				}
				p, err := proof(ctx)
				if changedCredential {
					p.CredentialID = "other.session"
				}
				return p, err
			}
			called := 0
			d.transport = v5ChatTestTransport(func(ctx context.Context, target V5ChatTarget, req campusossdk.V5ChatRequest, out io.Writer) error {
				called++
				switch action {
				case "feature-during":
					if _, err := features.Request(d.featureID, false); err != nil {
						t.Fatal(err)
					}
				case "session-during":
					live = false
				case "credential-change":
					changedCredential = true
				case "revoke":
					g, _ := v.s.grant(ctx, v.c)
					v.mutate(t, V5Mutation{Kind: "revoke", ExpectedRevision: g.Revision, IdempotencyKey: "revoke-during-chat"}, true, v.admin)
				case "configure":
					v.mutate(t, V5Mutation{Kind: "configure", Values: json.RawMessage(`{"locale":"en-US"}`), IdempotencyKey: "configure-during-chat"}, true, v.admin)
				case "uninstall":
					i, _ := v.s.installation(ctx, v.c, "1001")
					v.mutate(t, V5Mutation{Kind: "uninstall", ExpectedGeneration: i.Generation, IdempotencyKey: "uninstall-during-chat"}, false, proof)
				}
				return v5ChatReply(ctx, target, req, out)
			})
			if action == "feature-before" {
				if _, err := features.Request(d.featureID, false); err != nil {
					t.Fatal(err)
				}
			}
			if action == "session-before" {
				live = false
			}
			out, err := d.Chat(context.Background(), b, v5ChatTestRequest(b), current)
			if err == nil || out.Text != "" {
				t.Fatal("response escaped stale host facts", err)
			}
			if strings.HasSuffix(action, "-before") && called != 0 || !strings.HasSuffix(action, "-before") && called != 1 {
				t.Fatal("wrong admission ordering", called)
			}
		})
	}
}

func TestV5ChatDispatchDeadlineAndConcurrency(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	var count atomic.Int64
	transport := v5ChatTestTransport(func(ctx context.Context, target V5ChatTarget, req campusossdk.V5ChatRequest, out io.Writer) error {
		if count.Add(1) == 1 {
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return v5ChatReply(ctx, target, req, out)
	})
	_, d, b, proof, _ := newV5ChatTestDispatcher(t, transport)
	done := make(chan error, 1)
	go func() {
		_, err := d.Chat(context.Background(), b, v5ChatTestRequest(b), proof)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first call never entered transport")
	}
	if _, err := d.Chat(context.Background(), b, v5ChatTestRequest(b), proof); err != ErrV5ContributionBudget || count.Load() != 1 {
		t.Fatal("grant concurrency was bypassed", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := d.Chat(context.Background(), b, v5ChatTestRequest(b), proof); err != nil {
		t.Fatal("permit was not released", err)
	}
	d.transport = v5ChatTestTransport(func(ctx context.Context, _ V5ChatTarget, _ campusossdk.V5ChatRequest, _ io.Writer) error {
		<-ctx.Done()
		return nil // A late successful status is still rejected.
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if out, err := d.Chat(ctx, b, v5ChatTestRequest(b), proof); err != ErrV5CallDeadline || out.Text != "" {
		t.Fatal("late response/deadline was accepted", err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if out, err := d.Chat(cancelled, b, v5ChatTestRequest(b), proof); err != context.Canceled || out.Text != "" {
		t.Fatal("parent cancellation was ignored", err)
	}
	if d.total != 0 || len(d.active) != 0 {
		t.Fatal("failed call leaked concurrency permits")
	}
}
