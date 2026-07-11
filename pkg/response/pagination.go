package response

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	DefaultPage     = 1
	DefaultPageSize = 20
	MaximumPageSize = 100
)

// ParsePagination validates the shared page/page_size contract and writes a
// compatible structured 400 response when either value is invalid.
func ParsePagination(c *gin.Context, defaultSize, maximumSize int) (page, pageSize int, ok bool) {
	if defaultSize <= 0 {
		defaultSize = DefaultPageSize
	}
	if maximumSize <= 0 {
		maximumSize = MaximumPageSize
	}
	page, err := parsePositiveInt(c.DefaultQuery("page", strconv.Itoa(DefaultPage)), "page")
	if err != nil {
		ErrorWithDetails(c, http.StatusBadRequest, 10001, err.Error(), gin.H{"parameter": "page", "minimum": 1})
		return 0, 0, false
	}
	pageSize, err = parsePositiveInt(c.DefaultQuery("page_size", strconv.Itoa(defaultSize)), "page_size")
	if err != nil || pageSize > maximumSize {
		message := fmt.Sprintf("page_size must be an integer between 1 and %d", maximumSize)
		ErrorWithDetails(c, http.StatusBadRequest, 10001, message, gin.H{"parameter": "page_size", "minimum": 1, "maximum": maximumSize})
		return 0, 0, false
	}
	return page, pageSize, true
}

func parsePositiveInt(raw, name string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}
