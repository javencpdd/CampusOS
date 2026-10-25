package ai

import (
	"context"
	"sync"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

const PortPluginText = "ai.plugin-text/v1"

// PluginTextRequest names an exact optional contribution. It carries only text
// explicitly supplied for that provider, never a resource, model Profile or URL.
type PluginTextRequest struct {
	Publisher, Key, Version, ContributionID string
	RequestID, Prompt                       string
	TimeoutMS                               int64
}

type PluginTextAnswer struct {
	Text                      string
	InputTokens, OutputTokens int64
	FinishReason              string
}

// PluginTextAdapter is implemented at the composition root. AI does not import
// the plugin platform, and its built-in managed paths do not depend on it.
type PluginTextAdapter interface {
	AnswerText(context.Context, identityport.SessionPrincipalResolver, PluginTextRequest) (PluginTextAnswer, error)
}

type PluginText interface {
	AnswerText(context.Context, identityport.SessionPrincipalResolver, PluginTextRequest) (PluginTextAnswer, error)
}

type pluginTextService struct {
	adapter   PluginTextAdapter
	enabled   func() bool
	lifecycle sync.Mutex
	mu        sync.Mutex
	life      context.Context
	cancel    context.CancelFunc
	calls     sync.WaitGroup
}

func newPluginTextService(adapter PluginTextAdapter, enabled func() bool) *pluginTextService {
	life, cancel := context.WithCancel(context.Background())
	return &pluginTextService{adapter: adapter, enabled: enabled, life: life, cancel: cancel}
}

func (s *pluginTextService) stop() {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	s.cancel()
	s.mu.Unlock()
	s.calls.Wait()
}

func (s *pluginTextService) start() {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.life.Err() != nil {
		s.life, s.cancel = context.WithCancel(context.Background())
	}
}

func (s *pluginTextService) AnswerText(ctx context.Context, resolve identityport.SessionPrincipalResolver, req PluginTextRequest) (PluginTextAnswer, error) {
	zero := PluginTextAnswer{}
	if !validText(req.Publisher, 128, true) || !validText(req.Key, 128, true) || !validText(req.Version, 64, true) || !validText(req.ContributionID, 128, true) ||
		!validText(req.RequestID, 128, true) || !validText(req.Prompt, 12000, true) || req.TimeoutMS < 1 || req.TimeoutMS > 30000 {
		return zero, ErrManagedDenied
	}
	if s == nil || s.adapter == nil || s.enabled == nil || !s.enabled() {
		return zero, ErrManagedUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	s.mu.Lock()
	if s.life.Err() != nil {
		s.mu.Unlock()
		return zero, ErrManagedUnavailable
	}
	s.calls.Add(1)
	stop := context.AfterFunc(s.life, cancel)
	s.mu.Unlock()
	defer s.calls.Done()
	defer stop()
	_, bound, err := managedPrincipal(ctx, resolve)
	if err != nil {
		return zero, ErrManagedDenied
	}
	current := func(c context.Context) (identityport.PrincipalContext, error) {
		if c.Err() != nil || !s.enabled() {
			return identityport.PrincipalContext{}, ErrManagedDenied
		}
		return bound(c)
	}
	out, err := s.adapter.AnswerText(ctx, current, req)
	if err != nil {
		return zero, ErrManagedUnavailable
	}
	if _, err = current(ctx); err != nil {
		return zero, ErrManagedDenied
	}
	if !validText(out.Text, 200000, false) || out.InputTokens < 0 || out.OutputTokens < 0 || (out.FinishReason != "completed" && out.FinishReason != "length") {
		return zero, ErrManagedDenied
	}
	return out, nil
}
