package idgen

import (
	"sync/atomic"
	"time"
)

var lastID int64

// New returns a process-local, monotonically increasing int64 ID.
func New() int64 {
	for {
		now := time.Now().UnixNano()
		prev := atomic.LoadInt64(&lastID)
		next := now
		if next <= prev {
			next = prev + 1
		}
		if atomic.CompareAndSwapInt64(&lastID, prev, next) {
			return next
		}
	}
}
