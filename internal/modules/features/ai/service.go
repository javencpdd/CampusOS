package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/campusos/CampusOS/pkg/config"
)

type Service struct {
	managed        *ManagedService
	enabled        bool
	ready          bool
	provider       string
	providerConfig OpenAICompatibleConfig
	gateway        *Gateway
	logger         CallLogStore
	err            error
}

type ServiceStatus struct {
	Definition *Definition            `json:"definition,omitempty"`
	Enabled    bool                   `json:"enabled"`
	Ready      bool                   `json:"ready"`
	Provider   string                 `json:"provider"`
	Config     map[string]interface{} `json:"config,omitempty"`
	Error      string                 `json:"error,omitempty"`
	Logs       int                    `json:"logs"`
}

func NewServiceFromConfig(cfg config.AIConfig) (*Service, error) {
	service := &Service{
		enabled:  cfg.Enabled,
		provider: cfg.Provider,
	}
	if !cfg.Enabled {
		return service, nil
	}
	if cfg.Provider != defaultOpenAIProviderName {
		err := newGatewayError(ErrProviderConfig, fmt.Sprintf("unsupported provider %q", cfg.Provider), nil)
		service.err = err
		return service, err
	}
	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		wrapped := newGatewayError(ErrProviderConfig, "invalid AI timeout", err)
		service.err = wrapped
		return service, wrapped
	}
	providerConfig := OpenAICompatibleConfig{
		Name:    cfg.Provider,
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
		Timeout: timeout,
	}
	provider, err := NewOpenAICompatibleProvider(providerConfig)
	if err != nil {
		service.err = err
		return service, err
	}
	logger := NewMemoryCallLogger()
	gateway, err := NewGateway(provider, GatewayConfig{
		DefaultModel:  cfg.Model,
		DefaultSource: "campusos-api",
	}, WithCallLogger(logger), WithLimiter(NewInMemoryLimiter(cfg.MaxConcurrent, cfg.MaxRequestsPerMinute)))
	if err != nil {
		service.err = err
		return service, err
	}
	service.ready = true
	service.providerConfig = providerConfig
	service.gateway = gateway
	service.logger = logger
	return service, nil
}

func (s *Service) ChatCompletion(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	if s != nil && s.managed != nil {
		return nil, ErrManagedDenied
	}
	if s == nil || !s.ready || s.gateway == nil {
		return nil, newGatewayError(ErrProviderConfig, "AI Gateway is not ready", nil)
	}
	return s.gateway.ChatCompletion(ctx, request)
}

func (s *Service) Status() ServiceStatus {
	if s == nil {
		return ServiceStatus{Enabled: false, Ready: false}
	}
	if s.managed != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		status := ServiceStatus{Enabled: s.enabled, Provider: "managed", Ready: s.managed.check(ctx) == nil}
		if status.Ready {
			b, e := s.managed.binding(ctx, false)
			if e == nil {
				p, e := s.managed.profile(ctx, b.ProfileID, false)
				if e == nil && p.Enabled {
					d := textDefinition()
					d.ID = p.Definition
					status.Definition = &d
					secret, e := s.managed.secret(ctx, p)
					clear(secret)
					status.Ready = e == nil
				} else {
					status.Ready = false
				}
			} else {
				status.Ready = false
			}
		}
		if !status.Ready {
			status.Error = "ai.managed_unavailable"
		}
		return status
	}
	logs, _ := s.ListLogs(context.Background(), 0)
	status := ServiceStatus{
		Enabled:  s.enabled,
		Ready:    s.ready,
		Provider: s.provider,
		Logs:     len(logs),
	}
	if s.gateway != nil {
		d := s.gateway.provider.Definition()
		status.Definition = &d
	}
	if s.providerConfig.BaseURL != "" || s.providerConfig.Model != "" {
		status.Config = s.providerConfig.Redacted()
	}
	if s.err != nil {
		status.Error = s.err.Error()
	}
	return status
}

func (s *Service) ListLogs(ctx context.Context, limit int) ([]CallLog, error) {
	if s == nil || s.logger == nil {
		return []CallLog{}, nil
	}
	return s.logger.List(ctx, limit)
}

func (s *Service) SetCallLogStore(store CallLogStore) {
	if s == nil || store == nil {
		return
	}
	s.logger = store
	if s.gateway != nil {
		s.gateway.logger = store
	}
}
