package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response 统一响应体
type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
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
		Code: 0,
		Msg:  "success",
		Data: data,
	})
}

// Created 创建成功
func Created(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, Response{
		Code: 0,
		Msg:  "created",
		Data: data,
	})
}

// Error 错误响应
func Error(c *gin.Context, httpStatus int, code int, msg string) {
	c.JSON(httpStatus, Response{
		Code: code,
		Msg:  msg,
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
	c.Status(http.StatusNoContent)
}
