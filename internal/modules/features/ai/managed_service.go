package ai

import (
	"context"
	"errors"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func (s *ManagedService) Invoke(ctx context.Context, resolve identityport.SessionPrincipalResolver, id string, expectedBinding int64, request ChatRequest) (ManagedResult, error) {
	return s.invoke(ctx, resolve, id, expectedBinding, request, "", nil)
}

func (s *ManagedService) invoke(ctx context.Context, resolve identityport.SessionPrincipalResolver, id string, expectedBinding int64, request ChatRequest, requiredProfile string, guard func(context.Context) error) (ManagedResult, error) {
	ctx, overallCancel := context.WithTimeout(ctx, 65*time.Second)
	defer overallCancel()
	var empty ManagedResult
	if e := s.check(ctx); e != nil {
		return empty, e
	}
	if !managedIDValid("ar_", id) || expectedBinding < 1 || request.Model != "" || request.Source != "" || len(request.Metadata) != 0 {
		return empty, ErrManagedDenied
	}
	if request.validate() != nil {
		return empty, newGatewayError(ErrInvalidRequest, "invalid_request", nil)
	}
	principal, bound, e := managedPrincipal(ctx, resolve)
	if e != nil {
		return empty, ErrManagedDenied
	}
	b, e := s.binding(ctx, false)
	if e != nil {
		return empty, ErrManagedUnavailable
	}
	if b.Revision != expectedBinding {
		return empty, ErrManagedConflict
	}
	p, e := s.profile(ctx, b.ProfileID, false)
	if e != nil || !p.Enabled {
		return empty, ErrManagedUnavailable
	}
	if requiredProfile != "" && (p.ID != requiredProfile || p.Definition != "builtin.local-openai.text") {
		return empty, ErrManagedDenied
	}
	checkPolicy := func(c context.Context) error {
		if guard != nil && guard(c) != nil {
			return ErrManagedConflict
		}
		return nil
	}
	if e = checkPolicy(ctx); e != nil {
		return empty, e
	}
	if request.MaxTokens == 0 {
		request.MaxTokens = p.MaxTokens
	}
	if request.MaxTokens > p.MaxTokens {
		return empty, ErrManagedBudget
	}
	request.Messages = append([]Message(nil), request.Messages...)
	if request.Temperature != nil {
		v := *request.Temperature
		request.Temperature = &v
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return empty, ErrManagedUnavailable
	}
	s.calls.Add(1)
	s.mu.Unlock()
	defer s.calls.Done()
	instance := s.instance(p, b)
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutMS)*time.Millisecond)
	defer cancel()
	stopInstance := context.AfterFunc(instance.ctx, cancel)
	defer stopInstance()
	run := ManagedRun{Contract: ManagedContractVersion, ID: id, Owner: principal.Actor.ID, ProfileID: p.ID, ProfileRevision: p.Revision, BindingRevision: b.Revision, InstanceID: instance.ID, State: "reserved", Lease: managedNow() + p.TimeoutMS + 5000}
	e = s.command(callCtx, "call.reserve", "user", principal.Actor.ID, id, func(tx context.Context) error {
		if e := checkPolicy(tx); e != nil {
			return e
		}
		if _, e := bound(tx); e != nil {
			return ErrManagedDenied
		}
		if e := s.lockedSnapshot(tx, p, b); e != nil {
			return e
		}
		db := transaction.ExecutorFor(tx, s.pool)
		tag, e := db.Exec(tx, `UPDATE ai_model_budgets SET reserved=reserved+1 WHERE profile_id=$1 AND charged+reserved<quota AND reserved<$2`, p.ID, p.MaxConcurrent)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrManagedBudget
		}
		tag, e = db.Exec(tx, `INSERT INTO ai_model_runs(id,owner_id,profile_id,profile_revision,binding_revision,instance_id,state,lease_until_ms) VALUES($1,$2,$3,$4,$5,$6,'reserved',$7) ON CONFLICT(id) DO NOTHING`, run.ID, run.Owner, run.ProfileID, run.ProfileRevision, run.BindingRevision, run.InstanceID, run.Lease)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrManagedReplay
		}
		if _, e = db.Exec(tx, `INSERT INTO ai_model_run_events(run_id,ordinal,kind) VALUES($1,1,'started')`, id); e != nil {
			return e
		}
		if _, e = bound(tx); e != nil {
			return ErrManagedDenied
		}
		if e := s.check(tx); e != nil {
			return e
		}
		return checkPolicy(tx)
	})
	if e != nil {
		return empty, managedPublicError(e)
	}
	before := func(c context.Context) error {
		if _, e := bound(c); e != nil {
			return ErrManagedDenied
		}
		if e := s.current(c, p, b); e != nil {
			return e
		}
		return checkPolicy(c)
	}
	// A known protection failure is still pre-dispatch: release its reservation.
	// Re-open at the transport boundary as well; this probe grants no reusable
	// authority and never stores the decrypted value on the instance.
	secret, protectionErr := s.secret(callCtx, p)
	clear(secret)
	e = protectionErr
	if e == nil {
		e = s.command(callCtx, "call.dispatch", "user", principal.Actor.ID, id, func(tx context.Context) error {
			if e := checkPolicy(tx); e != nil {
				return e
			}
			if _, e := bound(tx); e != nil {
				return ErrManagedDenied
			}
			if e := s.lockedSnapshot(tx, p, b); e != nil {
				return e
			}
			r, e := s.run(tx, id, principal.Actor.ID, true)
			if e != nil {
				return e
			}
			if r.State != "reserved" || r.Lease <= managedNow() {
				return ErrManagedConflict
			}
			_, e = transaction.ExecutorFor(tx, s.pool).Exec(tx, `UPDATE ai_model_runs SET state='dispatched' WHERE id=$1`, id)
			if e != nil {
				return e
			}
			if _, e = bound(tx); e != nil {
				return ErrManagedDenied
			}
			if e := s.check(tx); e != nil {
				return e
			}
			return checkPolicy(tx)
		})
	}
	var response *ChatResponse
	if e == nil {
		run.State = "dispatched"
		watchCtx, stopWatch := context.WithCancel(callCtx)
		watchDone := make(chan struct{})
		reason := make(chan error, 1)
		go func() {
			defer close(watchDone)
			monitorManagedProof(watchCtx, before, reason, cancel)
		}()
		response, e = s.execute(callCtx, p, b, request, before)
		stopWatch()
		<-watchDone
		select {
		case cause := <-reason:
			response, e = nil, cause
		default:
		}
	}
	if e == nil {
		e = before(callCtx)
	}
	state, code, usage := "completed", "", TokenUsage{}
	if e != nil {
		state = "failed"
		code = safeManagedCode(e)
		if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
			state = "cancelled"
		}
	} else {
		usage = response.Usage
	}
	// Required accounting/audit must finish even when the User cancels or is
	// revoked. A failed commit exposes no buffered text; the lease worker retries
	// metadata settlement only and never replays the provider request.
	settleCtx, settleCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer settleCancel()
	settled, settleErr := s.settle(settleCtx, id, state, code, usage)
	if settleErr != nil {
		return ManagedResult{Run: run}, ErrManagedUnavailable
	}
	empty.Run = settled
	if e != nil {
		return empty, managedPublicError(e)
	}
	if settled.State != "completed" {
		return empty, ErrManagedConflict
	}
	if e = before(callCtx); e != nil {
		return empty, managedPublicError(e)
	}
	empty.Response = *response
	return empty, nil
}
func managedPublicError(e error) error {
	if e == nil {
		return nil
	}
	for _, known := range []error{ErrManagedMFA, ErrManagedDenied, ErrManagedConflict, ErrManagedBudget, ErrManagedReplay, ErrManagedUnavailable, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(e, known) {
			return known
		}
	}
	var gateway *GatewayError
	if errors.As(e, &gateway) {
		return safeGatewayError(e)
	}
	return ErrManagedUnavailable
}
func (s *ManagedService) Run(ctx context.Context, resolve identityport.SessionPrincipalResolver, id string) (ManagedRun, error) {
	var zero ManagedRun
	if !managedIDValid("ar_", id) || s == nil || s.pool == nil {
		return zero, ErrManagedDenied
	}
	p, bound, e := managedPrincipal(ctx, resolve)
	if e != nil {
		return zero, ErrManagedDenied
	}
	r, e := s.run(ctx, id, p.Actor.ID, false)
	if e != nil {
		return zero, ErrManagedDenied
	}
	r.Events, e = s.events(ctx, id)
	if e != nil {
		return zero, ErrManagedUnavailable
	}
	if _, e = bound(ctx); e != nil {
		return zero, ErrManagedDenied
	}
	return r, nil
}
func (s *ManagedService) start(ctx context.Context) error {
	s.mu.Lock()
	s.stopped = false
	if s.done != nil {
		s.mu.Unlock()
		return nil
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()
	if s.configurationErr == nil {
		if e := s.provision(ctx); e != nil {
			s.configurationErr = ErrManagedUnavailable
		}
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				if s.check(workerCtx) != nil {
					s.invalidate()
				} else {
					s.mu.Lock()
					instances := make([]*managedInstance, 0, len(s.instances))
					for _, v := range s.instances {
						instances = append(instances, v)
					}
					s.mu.Unlock()
					for _, i := range instances {
						b, e := s.binding(workerCtx, false)
						if e != nil || b.Revision != i.BindingRevision || b.ProfileID != i.ProfileID {
							i.cancel()
							continue
						}
						p, e := s.profile(workerCtx, i.ProfileID, false)
						if e != nil || !p.Enabled || p.Revision != i.ProfileRevision {
							i.cancel()
						}
					}
				}
				sweep, stop := context.WithTimeout(workerCtx, 3*time.Second)
				s.reconcile(sweep)
				stop()
			}
		}
	}()
	return nil
}
func (s *ManagedService) stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	s.invalidate()
	if cancel != nil {
		cancel()
		<-done
	}
	s.calls.Wait()
	s.mu.Lock()
	s.done = nil
	s.cancel = nil
	s.mu.Unlock()
}
func (s *ManagedService) reconcile(ctx context.Context) {
	rows, e := s.pool.Query(ctx, `SELECT id FROM ai_model_runs WHERE state IN ('reserved','dispatched') AND lease_until_ms<=$1 ORDER BY lease_until_ms,id LIMIT 16`, managedNow())
	if e != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		_, _ = s.settle(ctx, id, "unknown", "expired", TokenUsage{})
	}
}

// Cancelling the observer after a successful transport must not turn its
// interrupted current-fact read into a fictitious authority revocation. Parent
// cancellation is already checked by execute and all final return boundaries.
func monitorManagedProof(ctx context.Context, before func(context.Context) error, reason chan<- error, cancel context.CancelFunc) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := before(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				reason <- err
				cancel()
				return
			}
		}
	}
}
