package richtext

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

const (
	maxContentStreams               = 64
	maxUserContentStreams           = 20
	maxUserContentRequestsPerMinute = 1200
	maxContentAdmissionUsers        = 4096
)

type contentUser struct {
	since            time.Time
	requests, active int
}
type contentAdmission struct {
	mu     sync.Mutex
	users  map[string]*contentUser
	active int
}

func (l *contentAdmission) acquire(user string, now time.Time) (func(), int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.users == nil {
		l.users = make(map[string]*contentUser)
	}
	entry := l.users[user]
	if entry == nil {
		if len(l.users) >= maxContentAdmissionUsers {
			for id, item := range l.users {
				if item.active == 0 && now.Sub(item.since) >= time.Minute {
					delete(l.users, id)
				}
			}
			if len(l.users) >= maxContentAdmissionUsers {
				return nil, 1
			}
		}
		entry = &contentUser{since: now}
		l.users[user] = entry
	}
	if now.Sub(entry.since) >= time.Minute {
		entry.since, entry.requests = now, 0
	}
	if entry.requests >= maxUserContentRequestsPerMinute {
		return nil, int(time.Minute-(now.Sub(entry.since)))/int(time.Second) + 1
	}
	if l.active >= maxContentStreams || entry.active >= maxUserContentStreams {
		return nil, 1
	}
	entry.requests++
	entry.active++
	l.active++
	var once sync.Once
	return func() { once.Do(func() { l.mu.Lock(); defer l.mu.Unlock(); entry.active--; l.active-- }) }, 0
}

func (h *Handler) admitContent(c *gin.Context, user string) (func(), bool) {
	release, retry := h.contentAdmission.acquire(user, time.Now())
	if release != nil {
		return release, true
	}
	c.Header("Retry-After", strconv.Itoa(retry))
	response.ErrorWithDetails(c, http.StatusTooManyRequests, 73002, "附件读取请求过于频繁：每位用户最多同时读取 20 路、每分钟 1200 次，全站同时最多 64 路。请关闭多余预览并稍后重试。", gin.H{"max_user_streams": maxUserContentStreams, "max_total_streams": maxContentStreams, "max_requests_per_minute": maxUserContentRequestsPerMinute, "retry_after_seconds": retry})
	return nil, false
}
