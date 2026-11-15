package handler

import (
	"errors"
	"net/http"

	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *AdminDomainHandler) ChangePassword(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if !h.requireBrowserCSRF(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if request.BindJSONStrict(c, &input) != nil || input.CurrentPassword == "" || input.NewPassword == "" {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	if h.currentSelf == nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	err := h.currentSelf.ChangePassword(c.Request.Context(), h.currentAdminResolver(c), input.CurrentPassword, input.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAdminPasswordCurrent):
			response.Error(c, http.StatusBadRequest, 10001, "当前密码不正确")
		case errors.Is(err, service.ErrAdminPasswordInvalid):
			response.Error(c, http.StatusBadRequest, 10001, "新密码须为6–64字节，且不能与当前密码相同")
		case errors.Is(err, service.ErrAdminPasswordMFA):
			response.ErrorDescriptor(c, apperror.IdentityMFAStepUpRequired, nil)
		default:
			writeAdminSelfError(c, err)
		}
		return
	}
	h.clearBrowserCookies(c)
	response.Success(c, gin.H{"changed": true, "reauthenticate": true})
}
