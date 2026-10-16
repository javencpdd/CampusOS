package handler

import (
	"context"
	"net/http"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *ThreadHandler) AdminPermissionsPreview(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	var req struct {
		ThreadIDs []string `json:"thread_ids"`
	}
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, 400, 10001, "请输入至多20个不同帖子ID，不接受授权事实。")
		return
	}
	rows, err := h.svc.PreviewAdminThreadPermissions(c.Request.Context(), req.ThreadIDs, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveAdminPrincipal(ctx, c)
	})
	if err != nil {
		writeAdminMetadataError(c, err)
		return
	}
	response.Success(c, gin.H{"items": rows})
}
