package space

import (
	"errors"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
)

const styleReadJSONMaxBytes = 1 << 20

func (h *Handler) checkedStyleReads(c *gin.Context) (AuthenticatedSpaceStyleReads, bool) {
	if _, ok := currentUserID(c); !ok {
		response.Error(c, 401, 20001, "unauthorized")
		return nil, false
	}
	reads, ok := h.styles.(AuthenticatedSpaceStyleReads)
	if !ok || corestorage.NilImageAdapter(reads) {
		writeSpaceError(c, ErrSpaceFactsUnavailable)
		return nil, false
	}
	return reads, true
}
func bindStyleReadJSON(c *gin.Context, dst any, optional bool, limit int64) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	var err error
	if optional {
		err = requestutil.BindJSONStrictOptional(c, dst)
	} else {
		err = requestutil.BindJSONStrict(c, dst)
	}
	if err == nil {
		return true
	}
	var size *http.MaxBytesError
	if errors.As(err, &size) {
		response.Error(c, 413, 10001, "样式请求超过允许的大小。")
	} else {
		response.Error(c, 400, 10001, "invalid request: "+err.Error())
	}
	return false
}
func styleReadNoBody(c *gin.Context) bool {
	if data, err := io.ReadAll(io.LimitReader(c.Request.Body, 1)); err != nil || len(data) > 0 {
		response.Error(c, 400, 10001, "样式读取不接受请求正文。")
		return false
	}
	return true
}
