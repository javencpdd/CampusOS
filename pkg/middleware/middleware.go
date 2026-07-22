package middleware

import (
	"log"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// TraceID 注入请求追踪 ID
func TraceID() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := c.GetHeader("X-Trace-ID")
		if traceID == "" {
			traceID = uuid.New().String()
		}
		c.Set("trace_id", traceID)
		c.Header("X-Trace-ID", traceID)
		c.Header("X-Request-ID", traceID)
		c.Next()
	}
}

// CORS 跨域资源共享
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if isLocalDevelopmentOrigin(origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin")
		} else if origin == "" {
			// Same-origin API calls need no CORS response. Keep the legacy
			// wildcard only for credential-free tools without an Origin header.
			c.Header("Access-Control-Allow-Origin", "*")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Trace-ID, X-CSRF-Token, X-Device-ID, X-Device-Name, X-Device-Type")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

func isLocalDevelopmentOrigin(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Logger 请求日志
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		traceID, _ := c.Get("trace_id")

		log.Printf("[HTTP] %s | %3d | %13v | %15s | %s",
			traceID,
			status,
			latency,
			c.ClientIP(),
			path,
		)
	}
}

// Recovery panic 恢复
func Recovery(collectors ...*observability.Collector) gin.HandlerFunc {
	var collector *observability.Collector
	if len(collectors) > 0 {
		collector = collectors[0]
	}
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if collector != nil {
					operation := c.GetString(observability.RouteOperationContextKey)
					if operation == "" {
						operation = c.FullPath()
					}
					collector.RecordPanic(operation)
				}
				log.Printf("[PANIC] request_id=%s method=%s path=%s panic=%v\n%s",
					c.GetString("trace_id"), c.Request.Method, c.Request.URL.Path, recovered, debug.Stack())
				if !c.Writer.Written() {
					response.ErrorDescriptor(c, apperror.InternalError, nil)
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}
