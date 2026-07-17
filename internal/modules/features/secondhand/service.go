package secondhand

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type Service struct {
	store     Store
	community communityport.ContentGateway
	query     communityport.ContentQuery
	enabled   func() bool
	reliable  *reliability.Service
}

func NewService(store Store, community communityport.ContentGateway, query communityport.ContentQuery) *Service {
	return &Service{
		store:     store,
		community: community,
		query:     query,
		enabled:   func() bool { return true },
	}
}

func (s *Service) SetEnabledChecker(checker func() bool) {
	if checker == nil {
		s.enabled = func() bool { return true }
		return
	}
	s.enabled = checker
}

func (s *Service) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable == nil {
		return
	}
	if snapshotter, ok := s.store.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
}

func (s *Service) Status() StatusResult {
	return StatusResult{Enabled: s.enabled == nil || s.enabled()}
}

func (s *Service) Create(ctx context.Context, authorID, authorName string, req CreateRequest) (*Result, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if err := validateCreateRequest(&req); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	detail := &Detail{
		PriceMinor:    req.PriceMinor,
		Currency:      normalizeCurrency(req.Currency),
		ItemCondition: normalizeCondition(req.ItemCondition),
		TradeMethod:   normalizeMethod(req.TradeMethod),
		TradeStatus:   TradeStatusAvailable,
		LocationScope: strings.TrimSpace(req.LocationScope),
		Version:       1,
		CreatedBy:     strings.TrimSpace(authorID),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	thread, err := s.community.CreateStructuredThread(ctx, authorID, authorName, domain.CreateThreadRequest{
		Title:      strings.TrimSpace(req.Title),
		Content:    strings.TrimSpace(req.Content),
		CategoryID: strings.TrimSpace(req.CategoryID),
		Tags:       req.Tags,
	}, communityport.ThreadCreateOptions{
		Status:        domain.ThreadStatusPublished,
		ContentFormat: ContentFormat,
		ThreadType:    domain.ThreadTypeSecondhand,
		CommandCode:   "feature.secondhand.create",
		EventType:     "secondhand.created",
	}, createParticipant{store: s.store, detail: detail})
	if err != nil {
		return nil, normalizeCommunityError(err)
	}
	return &Result{Thread: thread, Detail: cloneDetail(detail)}, nil
}

func (s *Service) GetPublic(ctx context.Context, threadID string) (*Result, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	thread, err := s.query.GetPublicThread(ctx, strings.TrimSpace(threadID))
	if err != nil {
		return nil, normalizeCommunityError(err)
	}
	if thread.ThreadType != domain.ThreadTypeSecondhand {
		return nil, ErrNotFound
	}
	detail, err := s.store.Get(ctx, thread.ID)
	if err != nil {
		return nil, normalizeStoreError(err)
	}
	return &Result{Thread: thread, Detail: detail}, nil
}

func (s *Service) GetMine(ctx context.Context, threadID, userID string) (*Result, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	thread, detail, err := s.authorThread(ctx, threadID, userID)
	if err != nil {
		return nil, err
	}
	return &Result{Thread: thread, Detail: detail}, nil
}

func (s *Service) ListPublic(ctx context.Context, filter domain.ThreadListFilter) ([]*Result, int64, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, 0, err
	}
	filter.ThreadType = domain.ThreadTypeSecondhand
	threads, total, err := s.query.ListPublicThreads(ctx, filter)
	if err != nil {
		return nil, 0, normalizeCommunityError(err)
	}
	results := make([]*Result, 0, len(threads))
	for _, thread := range threads {
		detail, detailErr := s.store.Get(ctx, thread.ID)
		if detailErr != nil {
			// A visible typed thread without its feature detail violates the
			// migration contract. Do not leak a partial record to callers.
			return nil, 0, fmt.Errorf("load secondhand detail for thread %s: %w", thread.ID, normalizeStoreError(detailErr))
		}
		results = append(results, &Result{Thread: thread, Detail: detail})
	}
	return results, total, nil
}

func (s *Service) Update(ctx context.Context, threadID, userID string, req UpdateRequest) (*Result, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if err := validateUpdateRequest(&req); err != nil {
		return nil, err
	}
	var result *Result
	err := s.execute(ctx, userID, "feature.secondhand.update", threadID, "secondhand.updated", func() any {
		return result
	}, func(commandCtx context.Context) error {
		thread, detail, commandErr := s.authorThread(commandCtx, threadID, userID)
		if commandErr != nil {
			return commandErr
		}
		if detail.Version != req.Version {
			return ErrVersionConflict
		}
		thread.Title = strings.TrimSpace(req.Title)
		thread.Content = strings.TrimSpace(req.Content)
		thread.Tags = append([]string(nil), req.Tags...)
		thread.ContentFormat = ContentFormat
		savedThread, commandErr := s.community.SaveFeatureThread(commandCtx, thread, userID, "secondhand_update")
		if commandErr != nil {
			return normalizeCommunityError(commandErr)
		}
		detail.PriceMinor = req.PriceMinor
		detail.Currency = normalizeCurrency(req.Currency)
		detail.ItemCondition = normalizeCondition(req.ItemCondition)
		detail.TradeMethod = normalizeMethod(req.TradeMethod)
		detail.LocationScope = strings.TrimSpace(req.LocationScope)
		detail.UpdatedAt = time.Now().UTC()
		if commandErr := s.store.Update(commandCtx, detail, req.Version); commandErr != nil {
			return normalizeStoreError(commandErr)
		}
		result = &Result{Thread: savedThread, Detail: cloneDetail(detail)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) UpdateStatus(ctx context.Context, threadID, userID string, req UpdateStatusRequest) (*Result, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	target := normalizeStatus(req.TradeStatus)
	if !validStatus(target) || req.Version < 1 {
		return nil, fmt.Errorf("%w: valid status and version are required", ErrInvalidInput)
	}
	var result *Result
	err := s.execute(ctx, userID, "feature.secondhand.status_update", threadID, "secondhand.status_updated", func() any {
		return result
	}, func(commandCtx context.Context) error {
		thread, detail, commandErr := s.authorThread(commandCtx, threadID, userID)
		if commandErr != nil {
			return commandErr
		}
		if detail.Version != req.Version {
			return ErrVersionConflict
		}
		if !canTransition(detail.TradeStatus, target) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, detail.TradeStatus, target)
		}
		detail.TradeStatus = target
		detail.UpdatedAt = time.Now().UTC()
		if commandErr := s.store.Update(commandCtx, detail, req.Version); commandErr != nil {
			return normalizeStoreError(commandErr)
		}
		result = &Result{Thread: thread, Detail: cloneDetail(detail)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type createParticipant struct {
	store  Store
	detail *Detail
}

func (p createParticipant) ThreadType() domain.ThreadType {
	return domain.ThreadTypeSecondhand
}

func (p createParticipant) PersistThreadDetail(ctx context.Context, thread *domain.Thread) error {
	if p.store == nil || p.detail == nil || thread == nil || strings.TrimSpace(thread.ID) == "" {
		return errors.New("secondhand detail participant is unavailable")
	}
	p.detail.ThreadID = thread.ID
	if err := p.store.Create(ctx, p.detail); err != nil {
		return fmt.Errorf("create secondhand detail: %w", err)
	}
	return nil
}

func (s *Service) authorThread(ctx context.Context, threadID, userID string) (*domain.Thread, *Detail, error) {
	thread, err := s.community.GetThread(ctx, strings.TrimSpace(threadID))
	if err != nil {
		return nil, nil, normalizeCommunityError(err)
	}
	if thread.ThreadType != domain.ThreadTypeSecondhand {
		return nil, nil, ErrNotFound
	}
	if strings.TrimSpace(userID) == "" || thread.AuthorID != strings.TrimSpace(userID) {
		return nil, nil, ErrForbidden
	}
	detail, err := s.store.Get(ctx, thread.ID)
	if err != nil {
		return nil, nil, normalizeStoreError(err)
	}
	return thread, detail, nil
}

func (s *Service) execute(ctx context.Context, actorID, code, threadID, eventType string, payload func() any, action func(context.Context) error) error {
	if s.reliable == nil || transaction.Active(ctx) {
		err := action(ctx)
		if err == nil && !transaction.Active(ctx) {
			s.community.InvalidateThreadList(ctx)
		}
		return err
	}
	err := s.reliable.Execute(ctx, reliability.Command{
		Code:          code,
		ActorID:       strings.TrimSpace(actorID),
		ActorType:     "user",
		ResourceType:  "secondhand",
		ResourceID:    strings.TrimSpace(threadID),
		OperationCode: code,
		EventFactory: func() (reliability.Event, error) {
			value := any(map[string]string{"thread_id": strings.TrimSpace(threadID), "action": code})
			if payload != nil {
				value = payload()
			}
			return reliability.NewEvent(eventType, "secondhand", strings.TrimSpace(threadID), value)
		},
	}, action)
	if err == nil {
		s.community.InvalidateThreadList(ctx)
	}
	return err
}

func (s *Service) ensureEnabled() error {
	if s.enabled != nil && !s.enabled() {
		return ErrFeatureDisabled
	}
	return nil
}

func validateCreateRequest(req *CreateRequest) error {
	if req == nil || strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Content) == "" || strings.TrimSpace(req.CategoryID) == "" {
		return fmt.Errorf("%w: title, content and category_id are required", ErrInvalidInput)
	}
	return validateDetailInput(req.PriceMinor, req.Currency, req.ItemCondition, req.TradeMethod, req.LocationScope)
}

func validateUpdateRequest(req *UpdateRequest) error {
	if req == nil || strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Content) == "" || req.Version < 1 {
		return fmt.Errorf("%w: title, content and version are required", ErrInvalidInput)
	}
	return validateDetailInput(req.PriceMinor, req.Currency, req.ItemCondition, req.TradeMethod, req.LocationScope)
}

func validateDetailInput(priceMinor int64, currency string, condition ItemCondition, method TradeMethod, location string) error {
	if priceMinor < 0 {
		return fmt.Errorf("%w: price_minor must not be negative", ErrInvalidInput)
	}
	if normalizeCurrency(currency) != currencyCNY {
		return fmt.Errorf("%w: only CNY currency is supported", ErrInvalidInput)
	}
	if !validCondition(condition) {
		return fmt.Errorf("%w: unsupported item_condition", ErrInvalidInput)
	}
	if !validMethod(method) {
		return fmt.Errorf("%w: unsupported trade_method", ErrInvalidInput)
	}
	if len([]rune(strings.TrimSpace(location))) > maxLocationLen {
		return fmt.Errorf("%w: location_scope exceeds %d characters", ErrInvalidInput, maxLocationLen)
	}
	return nil
}

func canTransition(from, to TradeStatus) bool {
	from = normalizeStatus(from)
	to = normalizeStatus(to)
	switch from {
	case TradeStatusAvailable:
		return to == TradeStatusReserved || to == TradeStatusSold || to == TradeStatusClosed
	case TradeStatusReserved:
		return to == TradeStatusAvailable || to == TradeStatusSold || to == TradeStatusClosed
	default:
		return false
	}
}

func normalizeCommunityError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, communityport.ErrThreadNotFound) {
		return ErrNotFound
	}
	return err
}

func normalizeStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return err
}
