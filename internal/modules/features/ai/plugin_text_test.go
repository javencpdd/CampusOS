package ai

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type pluginTextAdapterFunc func(context.Context, identityport.SessionPrincipalResolver, PluginTextRequest) (PluginTextAnswer, error)

func (f pluginTextAdapterFunc) AnswerText(c context.Context, r identityport.SessionPrincipalResolver, q PluginTextRequest) (PluginTextAnswer, error) {
	return f(c, r, q)
}

func TestPluginTextBoundary(t *testing.T) {
	principal := identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: "101"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "session-a"}
	resolve := func(context.Context) (identityport.PrincipalContext, error) { return principal, nil }
	q := PluginTextRequest{Publisher: "campusos", Key: "chat-echo", Version: "1.1.0", ContributionID: "chat-main", RequestID: "explicit", Prompt: "本人明确提交的文本", TimeoutMS: 1000}
	var calls int
	var enabled atomic.Bool
	enabled.Store(true)
	adapter := pluginTextAdapterFunc(func(ctx context.Context, r identityport.SessionPrincipalResolver, req PluginTextRequest) (PluginTextAnswer, error) {
		calls++
		if req != q {
			t.Fatal("request changed")
		}
		if p, e := r(ctx); e != nil || p != principal {
			t.Fatal("current principal changed")
		}
		return PluginTextAnswer{Text: "Echo: " + req.Prompt, FinishReason: "completed"}, nil
	})
	s := newPluginTextService(adapter, enabled.Load)
	defer s.stop()
	if out, err := s.AnswerText(t.Context(), resolve, q); err != nil || out.Text != "Echo: "+q.Prompt {
		t.Fatal(out, err)
	}
	for _, invalid := range []PluginTextRequest{{}, {Publisher: q.Publisher, Key: q.Key, Version: q.Version, ContributionID: q.ContributionID, RequestID: q.RequestID, Prompt: strings.Repeat("x", 12001), TimeoutMS: 1000}, {Publisher: q.Publisher, Key: q.Key, Version: q.Version, ContributionID: q.ContributionID, RequestID: q.RequestID, Prompt: q.Prompt, TimeoutMS: 30001}} {
		if out, e := s.AnswerText(t.Context(), resolve, invalid); e == nil || out.Text != "" {
			t.Fatal("invalid request dispatched")
		}
	}
	if _, e := s.AnswerText(t.Context(), nil, q); e == nil {
		t.Fatal("nil resolver")
	}
	principal.Actor.Kind, principal.Audience = "admin", "admin"
	if _, e := s.AnswerText(t.Context(), resolve, q); e == nil {
		t.Fatal("same ID admin")
	}
	principal.Actor.Kind, principal.Audience = "user", "user"
	enabled.Store(false)
	if _, e := s.AnswerText(t.Context(), resolve, q); e == nil {
		t.Fatal("disabled Feature")
	}
	if calls != 1 {
		t.Fatal("rejected input reached adapter", calls)
	}
	enabled.Store(true)
	for _, mode := range []string{"session", "feature", "error", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			original := principal
			defer func() { principal = original; enabled.Store(true) }()
			s.adapter = pluginTextAdapterFunc(func(context.Context, identityport.SessionPrincipalResolver, PluginTextRequest) (PluginTextAnswer, error) {
				out := PluginTextAnswer{Text: "UNRELEASED", FinishReason: "completed"}
				switch mode {
				case "session":
					principal.CredentialID = "session-b"
				case "feature":
					enabled.Store(false)
				case "error":
					return out, errors.New("PRIVATE_INTERNAL_PATH")
				case "malformed":
					out.OutputTokens = -1
				}
				return out, nil
			})
			out, e := s.AnswerText(t.Context(), resolve, q)
			if e == nil || out.Text != "" || strings.Contains(e.Error(), "PRIVATE_INTERNAL_PATH") {
				t.Fatal("untrusted output released", out, e)
			}
		})
	}
}

func TestPluginTextStopCancelsAndRestart(t *testing.T) {
	resolve := func(context.Context) (identityport.PrincipalContext, error) {
		return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: "101"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "session"}, nil
	}
	entered := make(chan struct{})
	s := newPluginTextService(pluginTextAdapterFunc(func(c context.Context, _ identityport.SessionPrincipalResolver, _ PluginTextRequest) (PluginTextAnswer, error) {
		close(entered)
		<-c.Done()
		return PluginTextAnswer{Text: "LATE", FinishReason: "completed"}, nil
	}), func() bool { return true })
	q := PluginTextRequest{Publisher: "campusos", Key: "echo", Version: "1.0.0", ContributionID: "text", RequestID: "r", Prompt: "text", TimeoutMS: 5000}
	done := make(chan error, 1)
	go func() {
		out, e := s.AnswerText(t.Context(), resolve, q)
		if out.Text != "" {
			done <- errors.New("late output")
			return
		}
		done <- e
	}()
	<-entered
	s.stop()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled result released")
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not drain")
	}
	if _, e := s.AnswerText(t.Context(), resolve, q); e == nil {
		t.Fatal("stopped call")
	}
	s.start()
	s.adapter = pluginTextAdapterFunc(func(context.Context, identityport.SessionPrincipalResolver, PluginTextRequest) (PluginTextAnswer, error) {
		return PluginTextAnswer{Text: "new", FinishReason: "completed"}, nil
	})
	if out, e := s.AnswerText(t.Context(), resolve, q); e != nil || out.Text != "new" {
		t.Fatal(out, e)
	}
	s.stop()
}
