package ai

import (
	"context"
	"sync"
	"time"
)

type CallStatus string

const (
	CallStatusSuccess     CallStatus = "success"
	CallStatusError       CallStatus = "error"
	CallStatusRateLimited CallStatus = "rate_limited"
)

type CallLog struct {
	Timestamp time.Time
	Provider  string
	Model     string
	Source    string
	Status    CallStatus
	Duration  time.Duration
	Usage     TokenUsage
	Error     string
}

type CallLogger interface {
	Log(ctx context.Context, entry CallLog) error
}

type MemoryCallLogger struct {
	mu   sync.Mutex
	logs []CallLog
}

func NewMemoryCallLogger() *MemoryCallLogger {
	return &MemoryCallLogger{}
}

func (l *MemoryCallLogger) Log(_ context.Context, entry CallLog) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, entry)
	return nil
}

func (l *MemoryCallLogger) List(limit int) []CallLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit <= 0 || limit > len(l.logs) {
		limit = len(l.logs)
	}
	start := len(l.logs) - limit
	result := make([]CallLog, limit)
	copy(result, l.logs[start:])
	return result
}
