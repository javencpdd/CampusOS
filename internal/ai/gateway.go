package ai

import (
	"context"
	"time"
)

type GatewayConfig struct {
	DefaultModel  string
	DefaultSource string
}

type Gateway struct {
	provider Provider
	config   GatewayConfig
	logger   CallLogger
	limiter  Limiter
	now      func() time.Time
}

type GatewayOption func(*Gateway)

func WithCallLogger(logger CallLogger) GatewayOption {
	return func(g *Gateway) {
		g.logger = logger
	}
}

func WithLimiter(limiter Limiter) GatewayOption {
	return func(g *Gateway) {
		g.limiter = limiter
	}
}

func WithClock(now func() time.Time) GatewayOption {
	return func(g *Gateway) {
		if now != nil {
			g.now = now
		}
	}
}

func NewGateway(provider Provider, config GatewayConfig, opts ...GatewayOption) (*Gateway, error) {
	if provider == nil {
		return nil, newGatewayError(ErrProviderConfig, "provider is required", nil)
	}
	gateway := &Gateway{
		provider: provider,
		config:   config,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(gateway)
	}
	return gateway, nil
}

func (g *Gateway) ChatCompletion(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if request.Model == "" {
		request.Model = g.config.DefaultModel
	}
	if request.Source == "" {
		request.Source = g.config.DefaultSource
	}
	if request.Source == "" {
		request.Source = "unknown"
	}

	start := g.now()
	release, err := g.acquire(ctx)
	if err != nil {
		status := CallStatusError
		if gatewayErr, ok := err.(*GatewayError); ok && gatewayErr.Code == ErrRateLimited {
			status = CallStatusRateLimited
		}
		g.log(ctx, CallLog{
			Timestamp: start,
			Provider:  g.provider.Name(),
			Model:     request.Model,
			Source:    request.Source,
			Status:    status,
			Error:     err.Error(),
		})
		return nil, err
	}
	defer release()

	response, err := g.provider.ChatCompletion(ctx, request)
	entry := CallLog{
		Timestamp: start,
		Provider:  g.provider.Name(),
		Model:     request.Model,
		Source:    request.Source,
		Duration:  g.now().Sub(start),
	}
	if err != nil {
		entry.Status = CallStatusError
		entry.Error = err.Error()
		g.log(ctx, entry)
		return nil, err
	}
	entry.Status = CallStatusSuccess
	entry.Usage = response.Usage
	if entry.Model == "" {
		entry.Model = response.Model
	}
	g.log(ctx, entry)
	return response, nil
}

func (g *Gateway) acquire(ctx context.Context) (func(), error) {
	if g.limiter == nil {
		return func() {}, nil
	}
	return g.limiter.Acquire(ctx)
}

func (g *Gateway) log(ctx context.Context, entry CallLog) {
	if g.logger == nil {
		return
	}
	_ = g.logger.Log(ctx, entry)
}
