package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type ragTestModel struct {
	plan   LocalModelPlan
	calls  int
	answer func(context.Context, identityport.SessionPrincipalResolver, string, func(context.Context) error) (string, error)
}

func (m *ragTestModel) PlanLocal(context.Context, identityport.SessionPrincipalResolver, int64) (LocalModelPlan, error) {
	return m.plan, nil
}
func (m *ragTestModel) AnswerLocal(ctx context.Context, r identityport.SessionPrincipalResolver, id string, _ int64, payload string, guard func(context.Context) error) (LocalModelAnswer, error) {
	m.calls++
	if e := guard(ctx); e != nil {
		return LocalModelAnswer{}, e
	}
	raw := `{"answer":"公开文本","citations":["E1"]}`
	var e error
	if m.answer != nil {
		raw, e = m.answer(ctx, r, payload, guard)
	}
	return LocalModelAnswer{raw, id}, e
}
func ragFixture(t *testing.T) (*collectionsFixture, *PublicLocalRAG, *ragTestModel, PublicLocalQuestion) {
	f, index, retrieval, c := retrievalFixture(t, 1)
	model := &ragTestModel{plan: LocalModelPlan{"local-chat", 11, 12}}
	svc := newPublicLocalRAG(retrieval, f.s, index, model)
	t.Cleanup(svc.stop)
	return f, svc, model, PublicLocalQuestion{c.ID, "公开文本", "ar_" + strings.Repeat("a", 32), 12}
}
func TestPublicLocalRAGAnswerAndNoEvidence(t *testing.T) {
	_, s, m, q := ragFixture(t)
	m.answer = func(ctx context.Context, r identityport.SessionPrincipalResolver, payload string, guard func(context.Context) error) (string, error) {
		var p struct {
			Question string            `json:"question"`
			Evidence []json.RawMessage `json:"evidence"`
		}
		if json.Unmarshal([]byte(payload), &p) != nil || p.Question != q.Question || len(p.Evidence) < 1 || len(p.Evidence) > maxRAGEvidence || strings.Contains(payload, "session-101") {
			t.Fatal("untrusted input shape/authority leaked")
		}
		return `{"answer":"公开文本","citations":["E1"]}`, guard(ctx)
	}
	out, e := s.Answer(t.Context(), privateTestResolver, q)
	if e != nil || out.Contract != PublicLocalRAGVersion || out.Status != "answered" || out.RunID != q.RunID || len(out.Citations) != 1 || out.Citations[0].Citation.CollectionID != q.CollectionID || out.Citations[0].ID != "E1" {
		t.Fatal("current cited answer", out, e)
	}
	q.Question = "NO_MATCH_99"
	out, e = s.Answer(t.Context(), privateTestResolver, q)
	if e != nil || out.Status != "refused" || out.Reason != "no_evidence" || out.Answer != "" || len(out.Citations) != 0 || m.calls != 1 {
		t.Fatal("no evidence contacted model", out, e)
	}
}
func TestPublicLocalRAGRejectsForgedAndMalformedAnswers(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown_citation":   `{"answer":"UNTRUSTED","citations":["E99"]}`,
		"duplicate_citation": `{"answer":"x","citations":["E1","E1"]}`,
		"missing_citation":   `{"answer":"x","citations":[]}`,
		"null":               `{"answer":"x","citations":null}`,
		"tools":              `{"answer":"x","citations":["E1"],"tools":["fetch_private"]}`,
		"duplicate_field":    `{"answer":"x","answer":"y","citations":["E1"]}`,
		"unicode":            `{"answer":"\ud800","citations":["E1"]}`,
		"overlimit":          `{"answer":"` + strings.Repeat("x", 4097) + `","citations":["E1"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, s, m, q := ragFixture(t)
			m.answer = func(context.Context, identityport.SessionPrincipalResolver, string, func(context.Context) error) (string, error) {
				return raw, nil
			}
			out, e := s.Answer(t.Context(), privateTestResolver, q)
			if !errors.Is(e, ErrRAGAnswer) || out.Answer != "" || len(out.Citations) != 0 || strings.Contains(e.Error(), "UNTRUSTED") {
				t.Fatal("unsafe answer escaped", out, e)
			}
		})
	}
}
func TestPublicLocalRAGRechecksAfterModelAndStops(t *testing.T) {
	for _, mode := range []string{"source_private", "source_body", "model_plan", "feature", "session"} {
		t.Run(mode, func(t *testing.T) {
			f, s, m, q := ragFixture(t)
			revoked := false
			resolve := func(c context.Context) (identityport.PrincipalContext, error) {
				if revoked {
					return identityport.PrincipalContext{}, ErrRAGDenied
				}
				return privateTestResolver(c)
			}
			m.answer = func(context.Context, identityport.SessionPrincipalResolver, string, func(context.Context) error) (string, error) {
				switch mode {
				case "source_private", "source_body":
					row, e := f.threads.GetByID(t.Context(), "100")
					if e != nil {
						t.Fatal(e)
					}
					if mode == "source_private" {
						row.PublicationStatus = "private"
						row.Status = "private"
					} else {
						row.Content = "changed"
					}
					if e = f.threads.Update(t.Context(), row); e != nil {
						t.Fatal(e)
					}
				case "model_plan":
					m.plan.ProfileRevision++
				case "feature":
					f.enabled = false
				case "session":
					revoked = true
				}
				return `{"answer":"LATE_UNTRUSTED","citations":["E1"]}`, nil
			}
			out, e := s.Answer(t.Context(), resolve, q)
			if e == nil || out.Answer != "" || len(out.Citations) != 0 {
				t.Fatal("late answer crossed changed authority", mode, out, e)
			}
		})
	}
	_, s, m, q := ragFixture(t)
	s.stop()
	if out, e := s.Answer(t.Context(), privateTestResolver, q); e == nil || out.Answer != "" || m.calls != 0 {
		t.Fatal("stopped called model")
	}
	s.start()
	if _, e := s.Answer(t.Context(), privateTestResolver, q); e != nil {
		t.Fatal("restart", e)
	}
}
func TestPublicLocalRAGIdentityLocalOnlyAndCancelled(t *testing.T) {
	_, s, m, q := ragFixture(t)
	for _, kind := range []string{"admin", "integration", "anonymous"} {
		resolve := func(context.Context) (identityport.PrincipalContext, error) {
			p := privateTestPrincipal()
			p.Actor.Kind, p.Audience = kind, kind
			return p, nil
		}
		if out, e := s.Answer(t.Context(), resolve, q); e == nil || out.Answer != "" || m.calls != 0 {
			t.Fatal("wrong principal", kind)
		}
	}
	m.plan.ProfileID = "remote-chat"
	if _, e := s.Answer(t.Context(), privateTestResolver, q); e == nil || m.calls != 0 {
		t.Fatal("remote fallback allowed")
	}
	m.plan.ProfileID = "local-chat"
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e := s.Answer(ctx, privateTestResolver, q); e == nil || m.calls != 0 {
		t.Fatal("cancelled called model")
	}
}
func TestPublicLocalRAGStopCancelsPendingCall(t *testing.T) {
	_, s, m, q := ragFixture(t)
	started := make(chan struct{})
	var completed atomic.Bool
	m.answer = func(c context.Context, _ identityport.SessionPrincipalResolver, _ string, _ func(context.Context) error) (string, error) {
		close(started)
		<-c.Done()
		completed.Store(true)
		return "", c.Err()
	}
	done := make(chan error, 1)
	go func() { _, e := s.Answer(t.Context(), privateTestResolver, q); done <- e }()
	<-started
	s.stop()
	if e := <-done; e == nil || !completed.Load() {
		t.Fatal("stop returned while pending model call remained")
	}
}
