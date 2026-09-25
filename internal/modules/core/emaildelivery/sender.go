// Package emaildelivery owns CampusOS's process-local email provider boundary.
// It receives only an ephemeral dispatch projection from Identity and never
// persists message content, verification codes, recipients, or SMTP secrets.
package emaildelivery

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrSenderUnavailable = errors.New("email provider is temporarily unavailable")

type smtpFailure struct {
	stage string
	cause error
}

func (e *smtpFailure) Error() string {
	if e == nil || e.cause == nil {
		return "SMTP operation failed"
	}
	return "SMTP " + e.stage + " failed: " + e.cause.Error()
}

func (e *smtpFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func wrapSMTPFailure(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &smtpFailure{stage: stage, cause: err}
}

// Message exists only between the durable-event consumer and a process-local
// sender. It must never be serialized into the Outbox, logs, audit details, or
// an HTTP response.
type Message struct {
	To             string
	Subject        string
	Text           string
	IdempotencyKey string
}

type ProviderHealth struct {
	Provider string
	State    string
	Message  string
}

type Sender interface {
	Provider() string
	Send(context.Context, Message) error
	Health(context.Context) ProviderHealth
}

type Config struct {
	Environment string
	Provider    string

	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPTimeout  time.Duration
	SMTPStartTLS bool
}

func NewSender(config Config) (Sender, error) {
	provider := strings.ToLower(strings.TrimSpace(config.Provider))
	if provider == "" {
		provider = "fake"
	}
	environment := strings.ToLower(strings.TrimSpace(config.Environment))
	if environment == "" {
		environment = "development"
	}
	switch provider {
	case "fake":
		if environment != "development" && environment != "test" {
			return nil, errors.New("fake email provider is allowed only in development or test")
		}
		return NewFakeSender(), nil
	case "smtp":
		return newSMTPSender(config)
	default:
		return nil, fmt.Errorf("unsupported email provider %q", provider)
	}
}

// FakeSender intentionally keeps messages in process memory only. It does not
// log a verification code or expose a local HTTP inbox. Tests use its delivery
// count; real local verification should use an isolated SMTP test service.
type FakeSender struct {
	mu        sync.Mutex
	delivered map[string]struct{}
}

func NewFakeSender() *FakeSender {
	return &FakeSender{delivered: make(map[string]struct{})}
}

func (s *FakeSender) Provider() string { return "fake" }

func (s *FakeSender) Send(ctx context.Context, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validMessage(message) {
		return errors.New("invalid email message")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.delivered[message.IdempotencyKey]; exists {
		return nil
	}
	s.delivered[message.IdempotencyKey] = struct{}{}
	return nil
}

func (s *FakeSender) Health(context.Context) ProviderHealth {
	return ProviderHealth{Provider: s.Provider(), State: "healthy", Message: "local test provider is configured"}
}

func (s *FakeSender) DeliveryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.delivered)
}

type smtpSender struct {
	host     string
	port     int
	username string
	password string
	from     string
	timeout  time.Duration
	startTLS bool

	mu        sync.Mutex
	delivered map[string]struct{}
}

func newSMTPSender(config Config) (Sender, error) {
	host := strings.TrimSpace(config.SMTPHost)
	from := strings.TrimSpace(config.SMTPFrom)
	if host == "" || from == "" {
		return nil, errors.New("SMTP host and from address are required")
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return nil, errors.New("SMTP from address is invalid")
	}
	if config.SMTPPort < 1 || config.SMTPPort > 65535 {
		return nil, errors.New("SMTP port is invalid")
	}
	timeout := config.SMTPTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &smtpSender{
		host:      host,
		port:      config.SMTPPort,
		username:  strings.TrimSpace(config.SMTPUsername),
		password:  config.SMTPPassword,
		from:      from,
		timeout:   timeout,
		startTLS:  config.SMTPStartTLS,
		delivered: make(map[string]struct{}),
	}, nil
}

func (s *smtpSender) Provider() string { return "smtp" }

func (s *smtpSender) Send(ctx context.Context, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validMessage(message) {
		return errors.New("invalid email message")
	}

	// A local successful-send cache cannot make generic SMTP exactly once, but
	// it avoids duplicate sends during a live process and preserves the stable
	// message ID for providers that implement their own idempotency handling.
	s.mu.Lock()
	if _, exists := s.delivered[message.IdempotencyKey]; exists {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	dialer := net.Dialer{Timeout: s.timeout}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(s.host, strconv.Itoa(s.port)))
	if err != nil {
		return wrapSMTPFailure("connect", err)
	}
	client, err := smtp.NewClient(connection, s.host)
	if err != nil {
		_ = connection.Close()
		return wrapSMTPFailure("session", err)
	}
	defer client.Close()
	if s.startTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return wrapSMTPFailure("starttls", errors.New("provider does not advertise STARTTLS"))
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}); err != nil {
			return wrapSMTPFailure("starttls", err)
		}
	}
	if s.username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return wrapSMTPFailure("authenticate", err)
		}
	}
	if err := client.Mail(s.from); err != nil {
		return wrapSMTPFailure("sender", err)
	}
	if err := client.Rcpt(message.To); err != nil {
		return wrapSMTPFailure("recipient", err)
	}
	writer, err := client.Data()
	if err != nil {
		return wrapSMTPFailure("message", err)
	}
	if _, err := io.WriteString(writer, formatMessage(s.from, message)); err != nil {
		_ = writer.Close()
		return wrapSMTPFailure("message", err)
	}
	if err := writer.Close(); err != nil {
		return wrapSMTPFailure("message", err)
	}
	if err := client.Quit(); err != nil {
		return wrapSMTPFailure("quit", err)
	}
	s.mu.Lock()
	s.delivered[message.IdempotencyKey] = struct{}{}
	s.mu.Unlock()
	return nil
}

func (s *smtpSender) Health(context.Context) ProviderHealth {
	return ProviderHealth{Provider: s.Provider(), State: "healthy", Message: "SMTP provider is configured"}
}

func validMessage(message Message) bool {
	if strings.TrimSpace(message.IdempotencyKey) == "" || strings.ContainsAny(message.Subject, "\r\n") || strings.ContainsAny(message.To, "\r\n") {
		return false
	}
	if strings.TrimSpace(message.Text) == "" {
		return false
	}
	_, err := mail.ParseAddress(strings.TrimSpace(message.To))
	return err == nil
}

func formatMessage(from string, message Message) string {
	return "From: " + from + "\r\n" +
		"To: " + message.To + "\r\n" +
		"Subject: " + mime.QEncoding.Encode("UTF-8", message.Subject) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"X-CampusOS-Delivery-ID: " + message.IdempotencyKey + "\r\n\r\n" +
		message.Text
}
