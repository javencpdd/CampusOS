package mutualaid

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/gin-gonic/gin"
)

func TestWriteErrorDoesNotExposeUnexpectedFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	internal := errors.New("ERROR: relation mutual_aid_details is unavailable for account=1001")

	writeError(context, internal)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Code  int    `json:"code"`
		Msg   string `json:"msg"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != apperror.InternalError.LegacyCode || payload.Msg != apperror.InternalError.Message || payload.Error.Message != apperror.InternalError.Message {
		t.Fatalf("unexpected internal error payload: %#v", payload)
	}
	if strings.Contains(recorder.Body.String(), internal.Error()) || strings.Contains(recorder.Body.String(), "mutual_aid_details") {
		t.Fatalf("internal error leaked through response: %s", recorder.Body.String())
	}
}

func TestWriteErrorMapsTrashedThreadToConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	writeError(context, ErrThreadNotEditable)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != 40009 {
		t.Fatalf("unexpected conflict payload: %#v", payload)
	}
}
