package platformlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Sources(c *gin.Context) {
	response.Success(c, h.svc.Sources())
}

func (h *Handler) Stream(c *gin.Context) {
	source := c.DefaultQuery("source", "api")
	lines, _ := strconv.Atoi(c.DefaultQuery("lines", "200"))
	follow := c.DefaultQuery("follow", "true") != "false"
	if _, ok := h.svc.source(source); !ok {
		response.Error(c, http.StatusBadRequest, 10001, "unknown platform log source")
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		response.Error(c, http.StatusInternalServerError, 10006, "streaming is not supported")
		return
	}

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	emit := func(line Line) error {
		payload, err := json.Marshal(line)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(c.Writer, "event: line\ndata: %s\n\n", payload); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	if err := h.svc.Stream(c.Request.Context(), source, lines, follow, emit); err != nil {
		if errors.Is(err, ErrUnknownSource) {
			c.Status(http.StatusBadRequest)
			return
		}
		_ = emit(Line{Source: source, Line: "读取日志失败：" + err.Error()})
	}
}
