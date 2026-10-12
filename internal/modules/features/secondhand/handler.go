package secondhand

import (
	"context"
	"errors"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/auth"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/pkg/apperror"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service  *Service
	verifier identityport.SessionVerifier
	strength identityport.SessionStrengthReader
}

func (h *Handler) SetSessionProof(verifier identityport.SessionVerifier, strength identityport.SessionStrengthReader) {
	h.verifier, h.strength = verifier, strength
}
func (h *Handler) readPrincipal(c *gin.Context) identityport.SessionPrincipalResolver {
	claims := &auth.JWTClaims{UserID: c.GetString("user_id"), SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, h.verifier, h.strength, claims)
	}
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Status(c *gin.Context) {
	response.Success(c, h.service.Status())
}

func (h *Handler) ListPublic(c *gin.Context) {
	page, pageSize, ok := response.ParsePagination(c, 20, 100)
	if !ok {
		return
	}
	items, total, err := h.service.ListPublicChecked(c.Request.Context(), domain.ThreadListFilter{
		Page:       page,
		PageSize:   pageSize,
		CategoryID: strings.TrimSpace(c.Query("category_id")),
		Keyword:    strings.TrimSpace(c.Query("keyword")),
		Tag:        strings.TrimSpace(c.Query("tag")),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	totalPages := int(total) / pageSize
	if int(total)%pageSize != 0 {
		totalPages++
	}
	response.List(c, items, &response.Pagination{Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages})
}

func (h *Handler) GetPublic(c *gin.Context) {
	result, err := h.service.GetForPrincipal(c.Request.Context(), c.Param("id"), nil)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) Create(c *gin.Context) {
	userID, displayName, ok := currentUser(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	var req CreateRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	result, err := h.service.Create(c.Request.Context(), userID, displayName, req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Created(c, result)
}

func (h *Handler) GetMine(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	_ = userID
	result, err := h.service.GetForPrincipal(c.Request.Context(), c.Param("id"), h.readPrincipal(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) Update(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	var req UpdateRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	result, err := h.service.Update(c.Request.Context(), c.Param("id"), userID, req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) UpdateStatus(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	var req UpdateStatusRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	result, err := h.service.UpdateStatus(c.Request.Context(), c.Param("id"), userID, req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, result)
}

func currentUser(c *gin.Context) (string, string, bool) {
	rawID, ok := c.Get("user_id")
	if !ok {
		return "", "", false
	}
	userID, ok := rawID.(string)
	if !ok || strings.TrimSpace(userID) == "" {
		return "", "", false
	}
	nickname, _ := c.Get("nickname")
	name, _ := nickname.(string)
	if strings.TrimSpace(name) == "" {
		username, _ := c.Get("username")
		name, _ = username.(string)
	}
	return userID, name, true
}

func writeError(c *gin.Context, err error) {
	if errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable) {
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
		return
	}
	if errors.Is(err, identityport.ErrPolicyFactsChanged) {
		response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
		return
	}

	response.WriteError(c, errorTranslator.Translate(err))
}
