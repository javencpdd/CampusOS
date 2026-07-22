package emaildelivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/observability"
)

const (
	ChallengeRequestedEvent  = "identity.email.challenge.requested.v1"
	ChallengeConsumer        = "core.email-delivery.challenge"
	MFALocalRecoveryEvent    = "identity.mfa.local_recovered.v1"
	MFALocalRecoveryConsumer = "core.email-delivery.mfa-local-recovery"
)

var (
	ErrInvalidDeliveryEvent = errors.New("email delivery event is invalid")
	ErrDeliveryUnavailable  = errors.New("email provider is temporarily unavailable")
)

type Status struct {
	Module         string     `json:"module"`
	Provider       string     `json:"provider"`
	State          string     `json:"state"`
	LastError      string     `json:"last_error,omitempty"`
	LastDeliveryAt *time.Time `json:"last_delivery_at,omitempty"`
	Delivered      int64      `json:"delivered"`
	Skipped        int64      `json:"skipped"`
}

type Service struct {
	dispatch identityport.ChallengeDispatchReader
	accounts identityport.AccountReader
	reliable *reliability.Service
	sender   Sender
	meter    observability.Meter

	mu     sync.RWMutex
	status Status
}

func (s *Service) SetMeter(meter observability.Meter) {
	if s != nil {
		s.meter = meter
	}
}

func NewService(dispatch identityport.ChallengeDispatchReader, accounts identityport.AccountReader, reliable *reliability.Service, sender Sender) (*Service, error) {
	if dispatch == nil {
		return nil, errors.New("identity challenge dispatch reader is required")
	}
	if accounts == nil {
		return nil, errors.New("identity account reader is required")
	}
	if reliable == nil {
		return nil, errors.New("reliability service is required")
	}
	if sender == nil {
		return nil, errors.New("email sender is required")
	}
	health := sender.Health(context.Background())
	state := health.State
	if state == "" {
		state = "healthy"
	}
	return &Service{
		dispatch: dispatch, accounts: accounts,
		reliable: reliable,
		sender:   sender,
		status:   Status{Module: ModuleID, Provider: sender.Provider(), State: state},
	}, nil
}

// DeliverMFALocalRecovery sends a security notice after the durable local
// break-glass command has committed. The Outbox event only carries the user
// aggregate ID and a fixed action; recipient data is re-read through the
// narrow credential-free Identity AccountReader and never enters the event.
func (s *Service) DeliverMFALocalRecovery(ctx context.Context, event reliability.Event) error {
	started := time.Now()
	result := "invalid"
	defer func() { s.observeDelivery(result, time.Since(started)) }()
	if s == nil || s.accounts == nil || strings.TrimSpace(event.AggregateID) == "" {
		return reliability.Permanent(ErrInvalidDeliveryEvent)
	}
	var payload struct {
		Action string `json:"action"`
	}
	decoder := json.NewDecoder(bytes.NewReader(event.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || strings.TrimSpace(payload.Action) != "mfa_reset" {
		return reliability.Permanent(ErrInvalidDeliveryEvent)
	}
	account, err := s.accounts.GetEmailAccount(ctx, strings.TrimSpace(event.AggregateID))
	if err != nil || !deliverableSecurityEmail(account) {
		result = "skipped"
		s.markSkipped()
		return nil
	}
	message := Message{
		To: account.IdentifierNormalized, Subject: "CampusOS 账号安全提醒",
		Text:           "您的 CampusOS 多因素认证已通过受控本机恢复被关闭，且所有登录设备已退出。若这不是您本人或已授权管理员执行的操作，请立即联系平台管理员。\n",
		IdempotencyKey: "identity-mfa-local-recovery:" + event.ID,
	}
	if err := s.sender.Send(ctx, message); err != nil {
		result = "unavailable"
		s.markDegraded()
		return reliability.Retryable(ErrDeliveryUnavailable, 30*time.Second)
	}
	result = "delivered"
	s.markDelivered()
	return nil
}

// DeliverChallenge consumes an intentionally minimal durable event. It only
// holds the reconstructed code while constructing the process-local message.
// A stale replay is acknowledged without an SMTP call; a provider failure is
// retried with a generic operational error that cannot leak recipient data.
func (s *Service) DeliverChallenge(ctx context.Context, event reliability.Event) error {
	started := time.Now()
	result := "invalid"
	defer func() { s.observeDelivery(result, time.Since(started)) }()
	var payload struct {
		ChallengeID string `json:"challenge_id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(event.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || strings.TrimSpace(payload.ChallengeID) == "" {
		return reliability.Permanent(ErrInvalidDeliveryEvent)
	}
	dispatch, err := s.dispatch.Dispatch(ctx, strings.TrimSpace(payload.ChallengeID))
	if errors.Is(err, identityport.ErrChallengeNotDeliverable) {
		result = "skipped"
		s.markSkipped()
		return nil
	}
	if err != nil {
		result = "unavailable"
		s.markDegraded()
		return reliability.Retryable(ErrDeliveryUnavailable, 30*time.Second)
	}
	message := challengeMessage(dispatch)
	if err := s.sender.Send(ctx, message); err != nil {
		result = "unavailable"
		s.markDegraded()
		return reliability.Retryable(ErrDeliveryUnavailable, 30*time.Second)
	}
	result = "delivered"
	s.markDelivered()
	return nil
}

func (s *Service) observeDelivery(result string, duration time.Duration) {
	if s == nil || s.meter == nil {
		return
	}
	labels := observability.Labels{"provider": s.sender.Provider(), "result": result}
	_ = s.meter.AddCounter("campusos_email_delivery_total", labels, 1)
	_ = s.meter.Observe("campusos_email_delivery_duration_seconds", labels, duration.Seconds())
}

func (s *Service) Status() Status {
	if s == nil {
		return Status{Module: ModuleID, State: "unhealthy"}
	}
	s.mu.RLock()
	result := s.status
	s.mu.RUnlock()
	if health := s.sender.Health(context.Background()); health.State == "degraded" && result.State == "healthy" {
		result.State = "degraded"
		result.LastError = safeHealthMessage(health.Message)
	}
	return result
}

func (s *Service) markDelivered() {
	now := time.Now().UTC()
	s.mu.Lock()
	s.status.State = "healthy"
	s.status.LastError = ""
	s.status.LastDeliveryAt = &now
	s.status.Delivered++
	s.mu.Unlock()
}

func (s *Service) markSkipped() {
	s.mu.Lock()
	s.status.Skipped++
	s.mu.Unlock()
}

func (s *Service) markDegraded() {
	s.mu.Lock()
	s.status.State = "degraded"
	s.status.LastError = "email provider delivery failed"
	s.mu.Unlock()
}

func challengeMessage(dispatch identityport.ChallengeDispatch) Message {
	purpose := strings.TrimSpace(dispatch.Purpose)
	subject := "CampusOS 验证码"
	contextLabel := "完成请求"
	switch purpose {
	case "registration":
		contextLabel = "完成注册"
	case "email_binding":
		contextLabel = "绑定邮箱"
	case "password_reset":
		contextLabel = "重置密码"
	}
	text := fmt.Sprintf("您正在使用 CampusOS%s。\n\n验证码：%s\n\n验证码将在 %s 失效。若非本人操作，请忽略此邮件。\n", contextLabel, dispatch.Code, dispatch.ExpiresAt.UTC().Format(time.RFC3339))
	if purpose == "password_reset" && dispatch.PublicID != "" {
		// The public challenge ID is an opaque correlation value, not a secret.
		// It is required for administrator-assisted recovery because the user
		// did not initiate the browser request that created the Challenge.
		text += fmt.Sprintf("\n恢复请求编号：%s\n", dispatch.PublicID)
	}
	return Message{
		To:             dispatch.Email,
		Subject:        subject,
		Text:           text,
		IdempotencyKey: "identity-challenge:" + dispatch.ChallengeID,
	}
}

func deliverableSecurityEmail(account identityport.EmailAccount) bool {
	if strings.TrimSpace(account.IdentifierNormalized) == "" {
		return false
	}
	switch strings.TrimSpace(account.VerificationState) {
	case "verified", "legacy_accepted", "system_managed":
		return true
	default:
		return false
	}
}

func safeHealthMessage(value string) string {
	if strings.TrimSpace(value) == "" {
		return "email provider is unavailable"
	}
	return "email provider is unavailable"
}
