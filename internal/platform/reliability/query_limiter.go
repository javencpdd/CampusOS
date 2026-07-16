package reliability

import (
	"sync"
	"time"
)

type queryWindow struct {
	startedAt time.Time
	count     int
}

const defaultQueryLimiterIdentityCapacity = 4096

// queryLimiter protects the administrator-only reliability queries from
// accidental polling storms. It is deliberately process-local; a shared
// limiter remains a multi-instance deployment concern.
type queryLimiter struct {
	mu       sync.Mutex
	maximum  int
	capacity int
	window   time.Duration
	now      func() time.Time
	windows  map[string]queryWindow
}

func newQueryLimiter(maximum int, window time.Duration) *queryLimiter {
	if maximum <= 0 {
		maximum = 120
	}
	if window <= 0 {
		window = time.Minute
	}
	return &queryLimiter{
		maximum:  maximum,
		capacity: defaultQueryLimiterIdentityCapacity,
		window:   window,
		now:      time.Now,
		windows:  make(map[string]queryWindow),
	}
}

func (l *queryLimiter) Allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	now := l.now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()

	window, ok := l.windows[key]
	if !ok {
		l.prune(now)
		l.windows[key] = queryWindow{startedAt: now, count: 1}
		return true, 0
	}
	if now.Sub(window.startedAt) >= l.window {
		l.windows[key] = queryWindow{startedAt: now, count: 1}
		return true, 0
	}
	if window.count >= l.maximum {
		retryAfter := window.startedAt.Add(l.window).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return false, retryAfter
	}
	window.count++
	l.windows[key] = window
	return true, 0
}

func (l *queryLimiter) prune(now time.Time) {
	if l.capacity <= 0 {
		l.capacity = defaultQueryLimiterIdentityCapacity
	}
	if len(l.windows) < l.capacity {
		return
	}
	for key, window := range l.windows {
		if now.Sub(window.startedAt) >= l.window {
			delete(l.windows, key)
		}
	}
	for len(l.windows) >= l.capacity {
		oldestKey := ""
		var oldestAt time.Time
		for key, window := range l.windows {
			if oldestKey == "" || window.startedAt.Before(oldestAt) ||
				(window.startedAt.Equal(oldestAt) && key < oldestKey) {
				oldestKey = key
				oldestAt = window.startedAt
			}
		}
		if oldestKey == "" {
			return
		}
		delete(l.windows, oldestKey)
	}
}
