package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func approvalTestDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func approvalTestPrepare(t *testing.T, ledger *ragApprovalLedger, owner, review string) string {
	t.Helper()
	id, expires, err := ledger.prepare(owner, review)
	if err != nil || !validRAGApprovalID(id) || expires.IsZero() {
		t.Fatal("review preparation failed", id, expires, err)
	}
	return id
}

func approvalTestDenied(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrRAGConsent) {
		t.Fatal("expected safe consent rejection", err)
	}
}

func TestRAGApprovalExactReviewAndIdentity(t *testing.T) {
	ledger := newRAGApprovalLedger()
	owner, review := approvalTestDigest("complete current User principal"), approvalTestDigest("exact reviewed envelope")
	otherOwner, otherReview := approvalTestDigest("other session"), approvalTestDigest("changed question or source or endpoint")
	id := approvalTestPrepare(t, ledger, owner, review)
	approvalTestDenied(t, ledger.current(id, owner, review))
	approvalTestDenied(t, ledger.consume(id, otherOwner, review, func() {}))
	approvalTestDenied(t, ledger.consume(id, owner, otherReview, func() {}))
	approvalTestDenied(t, ledger.consume(id, owner, review, nil))
	approvalTestDenied(t, ledger.revoke(id, otherOwner))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ledger.consume(id, owner, review, cancel); err != nil {
		t.Fatal("invalid attempts consumed legitimate review", err)
	}
	if err := ledger.current(id, owner, review); err != nil {
		t.Fatal("active exact review rejected", err)
	}
	approvalTestDenied(t, ledger.current(id, otherOwner, review))
	approvalTestDenied(t, ledger.current(id, owner, otherReview))
	approvalTestDenied(t, ledger.consume(id, owner, review, func() {}))
	approvalTestDenied(t, ledger.revoke(id, otherOwner))
	if ctx.Err() != nil {
		t.Fatal("unauthorized request cancelled valid invocation")
	}
	ledger.finish(id)
	if ctx.Err() != nil {
		t.Fatal("successful finish caused revocation")
	}
	approvalTestDenied(t, ledger.current(id, owner, review))
	approvalTestDenied(t, ledger.consume(id, owner, review, func() {}))
}

func TestRAGApprovalRejectsMalformedInputs(t *testing.T) {
	ledger := newRAGApprovalLedger()
	owner, review := approvalTestDigest("owner"), approvalTestDigest("review")
	id := approvalTestPrepare(t, ledger, owner, review)
	invalid := []string{"", "owner", strings.Repeat("0", 63), strings.Repeat("0", 65), strings.Repeat("g", 64), strings.ToUpper(review)}
	for _, value := range invalid {
		if _, _, err := ledger.prepare(value, review); !errors.Is(err, ErrRAGConsent) {
			t.Fatal("malformed owner accepted", err)
		}
		if _, _, err := ledger.prepare(owner, value); !errors.Is(err, ErrRAGConsent) {
			t.Fatal("malformed review accepted", err)
		}
		approvalTestDenied(t, ledger.consume(id, value, review, func() {}))
		approvalTestDenied(t, ledger.consume(id, owner, value, func() {}))
		approvalTestDenied(t, ledger.current(id, value, review))
		approvalTestDenied(t, ledger.current(id, owner, value))
		approvalTestDenied(t, ledger.revoke(id, value))
	}
	for _, invalidID := range []string{"", "kr_", "xx_" + strings.Repeat("a", 32), "kr_" + strings.Repeat("A", 32), "kr_" + strings.Repeat("g", 32), id + "0"} {
		approvalTestDenied(t, ledger.consume(invalidID, owner, review, func() {}))
		approvalTestDenied(t, ledger.current(invalidID, owner, review))
		approvalTestDenied(t, ledger.revoke(invalidID, owner))
		ledger.finish(invalidID)
	}
	if len(ledger.entries) != 1 {
		t.Fatal("malformed operation changed inventory")
	}
	var absent *ragApprovalLedger
	if _, _, err := absent.prepare(owner, review); !errors.Is(err, ErrRAGConsent) {
		t.Fatal("absent ledger accepted preparation")
	}
	approvalTestDenied(t, absent.consume(id, owner, review, func() {}))
	approvalTestDenied(t, absent.current(id, owner, review))
	approvalTestDenied(t, absent.revoke(id, owner))
	absent.finish(id)
	absent.reset()
	absent.stop()
}

func TestRAGApprovalConcurrentConsumeExactlyOnce(t *testing.T) {
	ledger := newRAGApprovalLedger()
	owner, review := approvalTestDigest("owner"), approvalTestDigest("review")
	id := approvalTestPrepare(t, ledger, owner, review)
	var successes, cancelled atomic.Int32
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			if err := ledger.consume(id, owner, review, func() { cancelled.Add(1) }); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrRAGConsent) {
				t.Error("unsafe failure", err)
			}
		}()
	}
	close(start)
	workers.Wait()
	if successes.Load() != 1 || cancelled.Load() != 0 {
		t.Fatal("review was not one use", successes.Load(), cancelled.Load())
	}
	if err := ledger.revoke(id, owner); err != nil {
		t.Fatal(err)
	}
	if cancelled.Load() != 1 {
		t.Fatal("revoke did not cancel the sole admitted call", cancelled.Load())
	}
}

func TestRAGApprovalExpiryAndCancellationOutsideLock(t *testing.T) {
	for _, operation := range []string{"expiry", "revoke", "reset", "stop"} {
		t.Run(operation, func(t *testing.T) {
			ledger := newRAGApprovalLedger()
			now := time.Date(2026, time.October, 24, 1, 0, 0, 0, time.UTC)
			ledger.now = func() time.Time { return now }
			owner, review := approvalTestDigest("owner"), approvalTestDigest("review")
			id, expires, err := ledger.prepare(owner, review)
			if err != nil || expires != now.Add(2*time.Minute) {
				t.Fatal("wrong exact expiry", expires, err)
			}
			var cancelled int
			cancel := func() {
				cancelled++
				if !ledger.mu.TryLock() {
					t.Error("cancellation callback executed under ledger mutex")
					return
				}
				ledger.mu.Unlock()
				// Reentrant checks are safe after the record is invalidated.
				approvalTestDenied(t, ledger.current(id, owner, review))
			}
			if err := ledger.consume(id, owner, review, cancel); err != nil {
				t.Fatal(err)
			}
			now = expires.Add(-time.Nanosecond)
			if err := ledger.current(id, owner, review); err != nil {
				t.Fatal("expired before precise boundary", err)
			}
			switch operation {
			case "expiry":
				now = expires
				approvalTestDenied(t, ledger.current(id, owner, review))
			case "revoke":
				if err := ledger.revoke(id, owner); err != nil {
					t.Fatal(err)
				}
			case "reset":
				ledger.reset()
			case "stop":
				ledger.stop()
			}
			if cancelled != 1 || len(ledger.entries) != 0 {
				t.Fatal("invalidation did not cancel and remove active review", cancelled, len(ledger.entries))
			}
			approvalTestDenied(t, ledger.current(id, owner, review))
			approvalTestDenied(t, ledger.consume(id, owner, review, func() {}))
			ledger.stop()
			if _, _, err := ledger.prepare(owner, review); !errors.Is(err, ErrRAGConsent) {
				t.Fatal("stopped ledger admitted preparation", err)
			}
			ledger.reset()
			newID := approvalTestPrepare(t, ledger, owner, review)
			if newID == id {
				t.Fatal("new lifecycle reused review identifier")
			}
			approvalTestDenied(t, ledger.consume(id, owner, review, func() {}))
			if cancelled != 1 {
				t.Fatal("cancel callback retained after invalidation")
			}
		})
	}
}

func TestRAGApprovalBoundsDoNotEvictAnotherOwner(t *testing.T) {
	ledger := newRAGApprovalLedger()
	now := time.Date(2026, time.October, 24, 1, 0, 0, 0, time.UTC)
	ledger.now = func() time.Time { return now }
	review := approvalTestDigest("review")
	firstOwner := approvalTestDigest("owner 0")
	firstID := approvalTestPrepare(t, ledger, firstOwner, review)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ledger.consume(firstID, firstOwner, review, cancel); err != nil {
		t.Fatal(err)
	}
	for ownerIndex := range 8 {
		owner := approvalTestDigest(fmt.Sprintf("owner %d", ownerIndex))
		start := 0
		if ownerIndex == 0 {
			start = 1
		}
		for index := start; index < ragApprovalPerOwnerLimit; index++ {
			approvalTestPrepare(t, ledger, owner, review)
		}
		if _, _, err := ledger.prepare(owner, review); !errors.Is(err, ErrRAGConsent) {
			t.Fatal("owner bound exceeded", ownerIndex, err)
		}
	}
	if len(ledger.entries) != ragApprovalGlobalLimit {
		t.Fatal("incorrect global inventory", len(ledger.entries))
	}
	newOwner := approvalTestDigest("owner 8")
	if _, _, err := ledger.prepare(newOwner, review); !errors.Is(err, ErrRAGConsent) {
		t.Fatal("global bound exceeded", err)
	}
	if err := ledger.current(firstID, firstOwner, review); err != nil || ctx.Err() != nil {
		t.Fatal("bounded new preparation evicted another owner's active review", err, ctx.Err())
	}
	ledger.finish(firstID)
	approvalTestPrepare(t, ledger, newOwner, review)
	if len(ledger.entries) != ragApprovalGlobalLimit {
		t.Fatal("finish did not release one inventory slot")
	}
	now = now.Add(ragApprovalTTL)
	approvalTestPrepare(t, ledger, newOwner, review)
	if len(ledger.entries) != 1 {
		t.Fatal("lazy expiry did not reclaim bounded inventory", len(ledger.entries))
	}
}
