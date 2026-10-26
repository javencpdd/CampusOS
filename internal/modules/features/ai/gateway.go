package ai

import (
	"context"
	"errors"
	"reflect"
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
	if provider == nil || (reflect.ValueOf(provider).Kind() == reflect.Pointer && reflect.ValueOf(provider).IsNil()) {
		return nil, newGatewayError(ErrProviderConfig, "provider is required", nil)
	}
	if err := provider.Definition().validate(); err != nil {
		return nil, err
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
	if err := ctx.Err(); err != nil {
		return nil, classifyContextError(err)
	}
	if err := g.provider.Definition().validate(); err != nil {
		return nil, err
	}
	if request.Model != "" && request.Model != g.config.DefaultModel || request.Source != "" && request.Source != g.config.DefaultSource {
		return nil, newGatewayError(ErrInvalidRequest, "per-call model/source override is not supported", nil)
	}
	request.Model = g.config.DefaultModel
	request.Source = g.config.DefaultSource
	if request.Source == "" {
		request.Source = "unknown"
	}
	if err := request.validate(); err != nil {
		return nil, err
	}
	request.Messages = append([]Message(nil), request.Messages...)
	if request.Temperature != nil {
		v := *request.Temperature
		request.Temperature = &v
	}
	if request.Metadata != nil {
		copy := make(map[string]string, len(request.Metadata))
		for k, v := range request.Metadata {
			copy[k] = v
		}
		request.Metadata = copy
	}

	start := g.now()
	release, err := g.acquire(ctx)
	if err != nil {
		err = safeGatewayError(err)
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
			Error:     safeGatewayError(err).Error(),
		})
		return nil, err
	}
	defer release()

	if err := ctx.Err(); err != nil {
		return nil, classifyContextError(err)
	}
	response, err := g.provider.ChatCompletion(ctx, request)
	if ctx.Err() != nil {
		response, err = nil, classifyContextError(ctx.Err())
	}
	if err == nil {
		err = validateTextResponse(response)
	}
	if err != nil {
		err = safeGatewayError(err)
	}
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
		entry.Model = g.config.DefaultModel
	}
	g.log(ctx, entry)
	if err := ctx.Err(); err != nil {
		return nil, classifyContextError(err)
	}
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

// Provider payloads, URLs, keys and wrapped vendor errors never become log or
// caller messages. Context sentinels retain their programmatic identity.
func safeGatewayError(err error) *GatewayError {
	if errors.Is(err, context.Canceled) {
		return newGatewayError(ErrContextCancelled, "context cancelled", context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newGatewayError(ErrContextDeadline, "context deadline exceeded", context.DeadlineExceeded)
	}
	code := ErrProviderRequest
	status := 0
	var typed *GatewayError
	if errors.As(err, &typed) {
		switch typed.Code {
		case ErrInvalidRequest, ErrUnsupportedCapability, ErrProviderConfig, ErrProviderRequest, ErrProviderResponse, ErrRateLimited:
			code = typed.Code
		}
		if typed.StatusCode >= 100 && typed.StatusCode <= 599 {
			status = typed.StatusCode
		}
	}
	return newProviderHTTPError(code, status, string(code), nil)
}
