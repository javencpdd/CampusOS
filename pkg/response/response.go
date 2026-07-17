package response

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response 统一响应体
type Response struct {
	Code      int         `json:"code"`
	Msg       string      `json:"msg"`
	Data      interface{} `json:"data,omitempty"`
	Error     *ErrorInfo  `json:"error,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
}

// ErrorInfo is the v0.6 machine-readable error extension. The legacy numeric
// code and msg fields remain at the top level for existing clients.
type ErrorInfo struct {
	Code      string      `json:"code"`
	Message   string      `json:"message"`
	Details   interface{} `json:"details,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
	Retryable bool        `json:"retryable"`
}

// ListResponse 列表响应
type ListResponse struct {
	Items      interface{} `json:"items"`
	Pagination *Pagination `json:"pagination"`
}

// Pagination 分页信息
type Pagination struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

// Success 成功响应
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:      0,
		Msg:       "success",
		Data:      data,
		RequestID: requestID(c),
	})
}

// Created 创建成功
func Created(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, Response{
		Code:      0,
		Msg:       "created",
		Data:      data,
		RequestID: requestID(c),
	})
}

// Error 错误响应
func Error(c *gin.Context, httpStatus int, code int, msg string) {
	ErrorWithDetails(c, httpStatus, code, msg, nil)
}

func ErrorWithDetails(c *gin.Context, httpStatus int, code int, msg string, details interface{}) {
	id := requestID(c)
	c.JSON(httpStatus, Response{
		Code: code,
		Msg:  msg,
		Error: &ErrorInfo{
			Code:      machineErrorCode(httpStatus, code),
			Message:   msg,
			Details:   details,
			RequestID: id,
			Retryable: httpStatus == http.StatusTooManyRequests || httpStatus == http.StatusServiceUnavailable || httpStatus >= 500,
		},
		RequestID: id,
	})
}

// List 列表响应
func List(c *gin.Context, items interface{}, pagination *Pagination) {
	Success(c, ListResponse{
		Items:      items,
		Pagination: pagination,
	})
}

// NoContent 无内容响应
func NoContent(c *gin.Context) {
	if id := requestID(c); id != "" {
		c.Header("X-Request-ID", id)
	}
	c.Status(http.StatusNoContent)
}

func requestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if value, ok := c.Get("trace_id"); ok {
		if id, ok := value.(string); ok {
			return id
		}
	}
	return c.Writer.Header().Get("X-Trace-ID")
}

func machineErrorCode(httpStatus, legacyCode int) string {
	if value, ok := stableErrorCodes[legacyCode]; ok {
		return value
	}
	switch httpStatus {
	case http.StatusBadRequest:
		return "request.invalid"
	case http.StatusUnauthorized:
		return "auth.unauthorized"
	case http.StatusForbidden:
		return "permission.denied"
	case http.StatusNotFound:
		return "resource.not_found"
	case http.StatusConflict:
		return "resource.conflict"
	case http.StatusTooManyRequests:
		return "request.rate_limited"
	default:
		return fmt.Sprintf("campusos.%d", legacyCode)
	}
}

var stableErrorCodes = map[int]string{
	10001: "request.invalid",
	10004: "resource.conflict",
	10006: "internal.error",
	10008: "identity.verification_required",
	10009: "identity.registration_verification_invalid",
	10010: "identity.registration_verification_rate_limited",
	20001: "auth.required",
	20002: "auth.invalid_token",
	20004: "permission.denied",
	20005: "auth.invalid_api_key",
	30004: "resource.not_found",
	40003: "thread.not_found",
	40004: "post.not_found",
	50002: "category.not_found",
	60003: "plugin.not_found",
	60005: "plugin.package_invalid",
	71002: "moderation.action_denied",
	73001: "richtext.permission_denied",
}
