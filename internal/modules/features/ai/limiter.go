package ai

import (
	"context"
	"sync"
	"time"
)

type Limiter interface {
	Acquire(ctx context.Context) (release func(), err error)
}

type InMemoryLimiter struct {
	maxRequestsPerMinute int
	sem                  chan struct{}
	mu                   sync.Mutex
	requests             []time.Time
	now                  func() time.Time
}

func NewInMemoryLimiter(maxConcurrent, maxRequestsPerMinute int) *InMemoryLimiter {
	var sem chan struct{}
	if maxConcurrent > 0 {
		sem = make(chan struct{}, maxConcurrent)
	}
	return &InMemoryLimiter{
		maxRequestsPerMinute: maxRequestsPerMinute,
		sem:                  sem,
		now:                  time.Now,
	}
}

func (l *InMemoryLimiter) Acquire(ctx context.Context) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	acquiredSemaphore := false
	if l.sem != nil {
		select {
		case l.sem <- struct{}{}:
			acquiredSemaphore = true
		case <-ctx.Done():
			return nil, contextGatewayError(ctx.Err())
		default:
			return nil, newGatewayError(ErrRateLimited, "max concurrent requests reached", nil)
		}
	}
	if l.maxRequestsPerMinute > 0 {
		now := l.now()
		l.mu.Lock()
		cutoff := now.Add(-time.Minute)
		kept := l.requests[:0]
		for _, ts := range l.requests {
			if ts.After(cutoff) {
				kept = append(kept, ts)
			}
		}
		l.requests = kept
		if len(l.requests) >= l.maxRequestsPerMinute {
			l.mu.Unlock()
			if acquiredSemaphore {
				<-l.sem
			}
			return nil, newGatewayError(ErrRateLimited, "requests per minute exceeded", nil)
		}
		l.requests = append(l.requests, now)
		l.mu.Unlock()
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			if acquiredSemaphore {
				<-l.sem
			}
		})
	}, nil
}

func contextGatewayError(err error) error {
	if err == nil {
		return newGatewayError(ErrContextCancelled, "context cancelled", nil)
	}
	if err == context.DeadlineExceeded {
		return newGatewayError(ErrContextDeadline, "context deadline exceeded", err)
	}
	return newGatewayError(ErrContextCancelled, err.Error(), err)
}
