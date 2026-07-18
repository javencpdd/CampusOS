package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var (
	ErrChallengeRateLimited = errors.New("email challenge request is rate limited")
	ErrChallengeInvalid     = errors.New("email challenge is invalid, expired, or no longer usable")
	ErrChallengeTicket      = errors.New("email challenge ticket is invalid, expired, or already consumed")
)

// ChallengeConfig contains process-only secret material. Neither this config
// nor values derived from it are returned through the HTTP/Admin APIs.
type ChallengeConfig struct {
	ActiveKeyID  string
	HMACKeys     map[string]string
	IPHashSecret string
	CodeTTL      time.Duration
	TicketTTL    time.Duration
	MaxAttempts  int
	Clock        func() time.Time
	Random       io.Reader
}

type ChallengeService struct {
	store       repository.ChallengeRepository
	policy      ChallengePolicyReader
	reliable    *reliability.Service
	activeKey   string
	keys        map[string][]byte
	ipKey       []byte
	codeTTL     time.Duration
	ticketTTL   time.Duration
	maxAttempts int
	clock       func() time.Time
	random      io.Reader
}

type ChallengePolicyReader interface {
	GetChallengePolicy(context.Context) (*domain.ChallengePolicy, error)
}

func NewChallengeService(store repository.ChallengeRepository, config ChallengeConfig) (*ChallengeService, error) {
	if store == nil {
		return nil, errors.New("challenge repository is required")
	}
	active := strings.TrimSpace(config.ActiveKeyID)
	if active == "" {
		return nil, errors.New("active challenge HMAC key id is required")
	}
	keys := make(map[string][]byte, len(config.HMACKeys))
	for keyID, secret := range config.HMACKeys {
		keyID = strings.TrimSpace(keyID)
		if keyID == "" || strings.TrimSpace(secret) == "" {
			continue
		}
		keys[keyID] = []byte(secret)
	}
	if len(keys[active]) == 0 {
		return nil, errors.New("active challenge HMAC key is unavailable")
	}
	if strings.TrimSpace(config.IPHashSecret) == "" {
		return nil, errors.New("challenge IP hash secret is required")
	}
	if config.CodeTTL <= 0 {
		config.CodeTTL = 10 * time.Minute
	}
	if config.TicketTTL <= 0 {
		config.TicketTTL = 15 * time.Minute
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 5
	}
	if config.MaxAttempts > 10 {
		return nil, errors.New("challenge max attempts must not exceed 10")
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	return &ChallengeService{
		store:       store,
		activeKey:   active,
		keys:        keys,
		ipKey:       []byte(config.IPHashSecret),
		codeTTL:     config.CodeTTL,
		ticketTTL:   config.TicketTTL,
		maxAttempts: config.MaxAttempts,
		clock:       config.Clock,
		random:      config.Random,
	}, nil
}

func (s *ChallengeService) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable != nil {
		if snapshotter, ok := s.store.(transaction.Snapshotter); ok {
			reliable.RegisterMemorySnapshotters(snapshotter)
		}
	}
}

func (s *ChallengeService) SetPolicyReader(policy ChallengePolicyReader) {
	s.policy = policy
}

// Request records a rate-limited challenge and a durable event containing
// only its opaque ID. SMTP delivery is intentionally implemented in A4.
func (s *ChallengeService) Request(ctx context.Context, request domain.ChallengeRequest) (*domain.ChallengeReceipt, error) {
	publicID, err := s.randomToken(24)
	if err != nil {
		return nil, err
	}
	var challenge *domain.EmailChallenge
	var event reliability.Event
	if err := s.execute(ctx, reliability.Command{
		Code:          "identity.email.challenge.request",
		ActorType:     "anonymous",
		ResourceType:  "identity_email_challenge",
		ResourceID:    publicID,
		OperationCode: "identity.email.challenge.request",
		EventFactory: func() (reliability.Event, error) {
			return event, nil
		},
	}, func(commandCtx context.Context) error {
		var requestErr error
		challenge, event, requestErr = s.requestForCommand(commandCtx, request, publicID)
		if challenge != nil {
			// The event idempotency key is set after the transaction action has
			// created its opaque challenge id; no raw email/code is included.
			event.IdempotencyKey = "identity.email.challenge.request:" + challenge.ID
		}
		return requestErr
	}); err != nil {
		return nil, err
	}
	if challenge == nil {
		return nil, ErrChallengeInvalid
	}
	return &domain.ChallengeReceipt{PublicID: challenge.PublicID, Purpose: challenge.Purpose, ExpiresAt: challenge.ExpiresAt}, nil
}

// RequestForCommand creates a rate-limited Challenge in the caller's active
// Core command transaction and returns the minimal email-delivery event. It
// opens no nested Reliability command, allowing recovery-case creation to be
// atomic with its Challenge and required audit/outbox record.
func (s *ChallengeService) RequestForCommand(ctx context.Context, request domain.ChallengeRequest) (*domain.EmailChallenge, reliability.Event, error) {
	return s.requestForCommand(ctx, request, "")
}

func (s *ChallengeService) requestForCommand(ctx context.Context, request domain.ChallengeRequest, publicID string) (*domain.EmailChallenge, reliability.Event, error) {
	if !request.Purpose.Valid() {
		return nil, reliability.Event{}, ErrChallengeInvalid
	}
	email := domain.NormalizeEmail(request.Email)
	if email == "" || domain.IsReservedEmail(email) {
		return nil, reliability.Event{}, ErrChallengeInvalid
	}
	now := s.now()
	if publicID == "" {
		var err error
		publicID, err = s.randomToken(24)
		if err != nil {
			return nil, reliability.Event{}, err
		}
	}
	nonce, err := s.randomToken(18)
	if err != nil {
		return nil, reliability.Event{}, err
	}
	challenge := &domain.EmailChallenge{
		ID:              strconv.FormatInt(idgen.New(), 10),
		PublicID:        publicID,
		Purpose:         request.Purpose,
		EmailNormalized: email,
		AccountID:       strings.TrimSpace(request.AccountID),
		KeyID:           s.activeKey,
		Nonce:           nonce,
		ExpiresAt:       now.Add(s.codeTTL),
		MaxAttempts:     s.maxAttempts,
		RequestedIPHash: s.keyedDigest("ip:\x00" + strings.TrimSpace(request.ClientIP)),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	windows, err := s.rateWindows(ctx, now, email, request.ClientIP)
	if err != nil {
		return nil, reliability.Event{}, err
	}
	ok, err := s.store.TryConsumeRate(ctx, windows)
	if err != nil {
		return nil, reliability.Event{}, err
	}
	if !ok {
		return nil, reliability.Event{}, ErrChallengeRateLimited
	}
	if err := s.store.CreateChallenge(ctx, challenge); err != nil {
		return nil, reliability.Event{}, err
	}
	event, err := reliability.NewEvent("identity.email.challenge.requested.v1", "identity_email_challenge", challenge.ID, struct {
		ChallengeID string `json:"challenge_id"`
	}{ChallengeID: challenge.ID})
	if err != nil {
		return nil, reliability.Event{}, err
	}
	return challenge, event, nil
}

func (s *ChallengeService) Verify(ctx context.Context, request domain.ChallengeVerificationRequest) (*domain.ChallengeTicket, error) {
	if !request.Purpose.Valid() || strings.TrimSpace(request.PublicID) == "" {
		return nil, ErrChallengeInvalid
	}
	var ticket *domain.ChallengeTicket
	var resultErr error
	err := s.execute(ctx, reliability.Command{
		Code:          "identity.email.challenge.verify",
		ActorType:     "anonymous",
		ResourceType:  "identity_email_challenge",
		ResourceID:    strings.TrimSpace(request.PublicID),
		OperationCode: "identity.email.challenge.verify",
	}, func(commandCtx context.Context) error {
		challenge, err := s.store.GetChallengeForUpdate(commandCtx, strings.TrimSpace(request.PublicID))
		if errors.Is(err, repository.ErrChallengeNotFound) {
			resultErr = ErrChallengeInvalid
			return nil
		}
		if err != nil {
			return err
		}
		now := s.now()
		if !s.codeUsable(challenge, request.Purpose, now) {
			if challenge.InvalidatedAt == nil && (now.After(challenge.ExpiresAt) || challenge.AttemptCount >= challenge.MaxAttempts) {
				challenge.InvalidatedAt = &now
				challenge.UpdatedAt = now
				if err := s.store.UpdateChallenge(commandCtx, challenge); err != nil {
					return err
				}
			}
			resultErr = ErrChallengeInvalid
			return nil
		}
		expected, err := s.codeFor(challenge)
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(strings.TrimSpace(request.Code))) != 1 {
			challenge.AttemptCount++
			if challenge.AttemptCount >= challenge.MaxAttempts {
				challenge.InvalidatedAt = &now
			}
			challenge.UpdatedAt = now
			if err := s.store.UpdateChallenge(commandCtx, challenge); err != nil {
				return err
			}
			resultErr = ErrChallengeInvalid
			return nil
		}
		rawTicket, err := s.randomToken(32)
		if err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(rawTicket))
		expiresAt := now.Add(s.ticketTTL)
		challenge.VerifiedAt = &now
		challenge.TicketDigest = hex.EncodeToString(digest[:])
		challenge.TicketExpiresAt = &expiresAt
		challenge.UpdatedAt = now
		if err := s.store.UpdateChallenge(commandCtx, challenge); err != nil {
			return err
		}
		ticket = &domain.ChallengeTicket{PublicID: challenge.PublicID, Purpose: challenge.Purpose, Ticket: rawTicket, ExpiresAt: expiresAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if resultErr != nil {
		return nil, resultErr
	}
	if ticket == nil {
		return nil, ErrChallengeInvalid
	}
	return ticket, nil
}

// ConsumeTicket wraps standalone consumers in a reliable command. Compound
// identity commands must use ConsumeTicketForCommand so Ticket consumption is
// committed or rolled back with their own user/account changes.
func (s *ChallengeService) ConsumeTicket(ctx context.Context, request domain.ChallengeTicketConsumption) (*domain.EmailChallenge, error) {
	var consumed *domain.EmailChallenge
	var resultErr error
	err := s.execute(ctx, reliability.Command{
		Code:          "identity.email.challenge.consume_ticket",
		ActorType:     "system",
		ResourceType:  "identity_email_challenge",
		ResourceID:    strings.TrimSpace(request.PublicID),
		OperationCode: "identity.email.challenge.consume_ticket",
	}, func(commandCtx context.Context) error {
		value, err := s.ConsumeTicketForCommand(commandCtx, request)
		if err != nil {
			if errors.Is(err, ErrChallengeTicket) {
				// A standalone consume persists expiration invalidation but still
				// returns the generic Ticket error after the command commits.
				resultErr = err
				return nil
			}
			return err
		}
		consumed = value
		return nil
	})
	if err != nil {
		return nil, err
	}
	if resultErr != nil {
		return nil, resultErr
	}
	if consumed == nil {
		return nil, ErrChallengeTicket
	}
	return consumed, nil
}

// ConsumeTicketForCommand consumes a Ticket using the transaction already
// carried by ctx. It deliberately does not open a nested Reliability command:
// registration, binding and reset own the required audit and outbox record.
func (s *ChallengeService) ConsumeTicketForCommand(ctx context.Context, request domain.ChallengeTicketConsumption) (*domain.EmailChallenge, error) {
	if !request.Purpose.Valid() || strings.TrimSpace(request.PublicID) == "" || strings.TrimSpace(request.Ticket) == "" {
		return nil, ErrChallengeTicket
	}
	email := domain.NormalizeEmail(request.Email)
	var consumed *domain.EmailChallenge
	challenge, err := s.store.GetChallengeForUpdate(ctx, strings.TrimSpace(request.PublicID))
	if errors.Is(err, repository.ErrChallengeNotFound) {
		return nil, ErrChallengeTicket
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	if challenge.Purpose != request.Purpose || challenge.EmailNormalized != email || challenge.ConsumedAt != nil ||
		challenge.InvalidatedAt != nil || challenge.TicketExpiresAt == nil || now.After(*challenge.TicketExpiresAt) || challenge.TicketDigest == "" {
		if challenge.InvalidatedAt == nil && challenge.TicketExpiresAt != nil && now.After(*challenge.TicketExpiresAt) {
			challenge.InvalidatedAt = &now
			challenge.UpdatedAt = now
			if err := s.store.UpdateChallenge(ctx, challenge); err != nil {
				return nil, err
			}
		}
		return nil, ErrChallengeTicket
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(request.Ticket)))
	if subtle.ConstantTimeCompare([]byte(challenge.TicketDigest), []byte(hex.EncodeToString(digest[:]))) != 1 {
		return nil, ErrChallengeTicket
	}
	challenge.ConsumedAt = &now
	challenge.UpdatedAt = now
	if err := s.store.UpdateChallenge(ctx, challenge); err != nil {
		return nil, err
	}
	copy := *challenge
	consumed = &copy
	if consumed == nil {
		return nil, ErrChallengeTicket
	}
	return consumed, nil
}

// LookupForCommand is a narrowly scoped internal lookup for compound Core
// commands such as administrator-assisted recovery. It keeps handlers and
// external modules away from Challenge persistence while preserving the same
// locked row used by the eventual Ticket consumption.
func (s *ChallengeService) LookupForCommand(ctx context.Context, publicID string, purpose domain.ChallengePurpose) (*domain.EmailChallenge, error) {
	if !purpose.Valid() || strings.TrimSpace(publicID) == "" {
		return nil, ErrChallengeTicket
	}
	challenge, err := s.store.GetChallengeForUpdate(ctx, strings.TrimSpace(publicID))
	if errors.Is(err, repository.ErrChallengeNotFound) {
		return nil, ErrChallengeTicket
	}
	if err != nil {
		return nil, err
	}
	if challenge.Purpose != purpose {
		return nil, ErrChallengeTicket
	}
	return challenge, nil
}

// Dispatch returns the code only in process memory for the Core email module.
// Its caller must never log, cache, serialize, or include it in an outbox row.
func (s *ChallengeService) Dispatch(ctx context.Context, challengeID string) (*domain.ChallengeDispatch, error) {
	challenge, err := s.store.GetChallengeByID(ctx, strings.TrimSpace(challengeID))
	if errors.Is(err, repository.ErrChallengeNotFound) {
		return nil, ErrChallengeInvalid
	}
	if err != nil {
		return nil, err
	}
	if !s.codeUsable(challenge, challenge.Purpose, s.now()) {
		return nil, ErrChallengeInvalid
	}
	code, err := s.codeFor(challenge)
	if err != nil {
		return nil, err
	}
	return &domain.ChallengeDispatch{
		ChallengeID: challenge.ID,
		PublicID:    challenge.PublicID,
		Purpose:     challenge.Purpose,
		Email:       challenge.EmailNormalized,
		Code:        code,
		ExpiresAt:   challenge.ExpiresAt,
	}, nil
}

func (s *ChallengeService) codeUsable(challenge *domain.EmailChallenge, purpose domain.ChallengePurpose, now time.Time) bool {
	return challenge != nil && challenge.Purpose == purpose && challenge.VerifiedAt == nil && challenge.ConsumedAt == nil &&
		challenge.InvalidatedAt == nil && challenge.AttemptCount < challenge.MaxAttempts && !now.After(challenge.ExpiresAt)
}

func (s *ChallengeService) codeFor(challenge *domain.EmailChallenge) (string, error) {
	if challenge == nil {
		return "", ErrChallengeInvalid
	}
	key, exists := s.keys[challenge.KeyID]
	if !exists || len(key) == 0 {
		return "", fmt.Errorf("challenge HMAC key %q is unavailable", challenge.KeyID)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(challenge.PublicID))
	_, _ = mac.Write([]byte("\x00"))
	_, _ = mac.Write([]byte(challenge.Nonce))
	_, _ = mac.Write([]byte("\x00"))
	_, _ = mac.Write([]byte(challenge.Purpose))
	sum := mac.Sum(nil)
	value := uint32(sum[0])<<24 | uint32(sum[1])<<16 | uint32(sum[2])<<8 | uint32(sum[3])
	return fmt.Sprintf("%06d", value%1_000_000), nil
}

func (s *ChallengeService) rateWindows(ctx context.Context, now time.Time, email, clientIP string) ([]domain.ChallengeRateWindow, error) {
	policy := domain.DefaultChallengePolicy()
	if s.policy != nil {
		configured, err := s.policy.GetChallengePolicy(ctx)
		if err != nil {
			return nil, fmt.Errorf("load challenge policy: %w", err)
		}
		if err := ValidateChallengePolicy(configured); err != nil {
			return nil, fmt.Errorf("load challenge policy: %w", err)
		}
		policy = *configured
	}
	observedAt := now.UTC().Truncate(time.Second)
	emailDigest := s.keyedDigest("email:\x00" + email)
	ipDigest := s.keyedDigest("ip:\x00" + strings.TrimSpace(clientIP))
	return []domain.ChallengeRateWindow{
		{Scope: "email_window", SubjectDigest: emailDigest, ObservedAt: observedAt, Duration: time.Duration(policy.EmailWindowMinutes) * time.Minute, Limit: policy.EmailMaxRequests},
		{Scope: "ip_window", SubjectDigest: ipDigest, ObservedAt: observedAt, Duration: time.Duration(policy.IPWindowMinutes) * time.Minute, Limit: policy.IPMaxRequests},
	}, nil
}

func (s *ChallengeService) keyedDigest(value string) string {
	mac := hmac.New(sha256.New, s.ipKey)
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *ChallengeService) randomToken(bytes int) (string, error) {
	if bytes < 16 {
		return "", errors.New("secure random token must contain at least 128 bits")
	}
	buffer := make([]byte, bytes)
	if _, err := io.ReadFull(s.random, buffer); err != nil {
		return "", fmt.Errorf("read secure random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func (s *ChallengeService) now() time.Time { return s.clock().UTC() }

func (s *ChallengeService) execute(ctx context.Context, command reliability.Command, action func(context.Context) error) error {
	if s.reliable != nil {
		return s.reliable.Execute(ctx, command, action)
	}
	return action(ctx)
}
