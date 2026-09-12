package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"metrochat/internal/data"
)

type voiceMessageTranscribeResponse struct {
	Text   string `json:"text"`
	Cached bool   `json:"cached"`
}

type voiceTranscribeHTTPError struct {
	Status  int
	Code    string
	Message string
}

func (e voiceTranscribeHTTPError) Error() string {
	return e.Message
}

func (a *API) handleDirectMessageVoiceTranscribe(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	messageID := strings.TrimSpace(chi.URLParam(r, "messageID"))
	if messageID == "" {
		writeError(w, http.StatusBadRequest, "invalid_message_id", "invalid message id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	msg, err := a.direct.GetMessageByID(ctx, messageID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "message_not_found", "message not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	thread, err := a.direct.GetThreadByID(ctx, msg.ThreadID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "message_not_found", "message not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if claims.Subject != thread.UserAID && claims.Subject != thread.UserBID {
		writeError(w, http.StatusNotFound, "message_not_found", "message not found")
		return
	}
	if !isVoiceMessageType(msg.MsgType) {
		writeError(w, http.StatusBadRequest, "invalid_message_type", "message is not voice type")
		return
	}

	if cachedText := extractVoiceTextFromBody(msg.Body); cachedText != "" {
		writeJSON(w, http.StatusOK, voiceMessageTranscribeResponse{
			Text:   cachedText,
			Cached: true,
		})
		return
	}

	text, transcribeErr := a.transcribeVoiceMessageURL(msg.MediaURL)
	if transcribeErr != nil {
		writeVoiceTranscribeError(w, transcribeErr)
		return
	}
	updatedBody, err := mergeVoiceTextIntoBody(msg.Body, text)
	if err != nil {
		writeVoiceTranscribeError(w, err)
		return
	}

	saveCtx, saveCancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer saveCancel()
	if err := a.direct.UpdateMessageBody(saveCtx, msg.ID, updatedBody); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "message_not_found", "message not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, voiceMessageTranscribeResponse{
		Text:   text,
		Cached: false,
	})
}

func (a *API) handleGroupMessageVoiceTranscribe(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	messageID := strings.TrimSpace(chi.URLParam(r, "messageID"))
	if messageID == "" {
		writeError(w, http.StatusBadRequest, "invalid_message_id", "invalid message id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	msg, err := a.groupMsgs.GetByID(ctx, messageID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "message_not_found", "message not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if _, err := a.groups.GetRole(ctx, msg.GroupID, claims.Subject); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusForbidden, "not_member", "not a member")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if !isVoiceMessageType(msg.MsgType) {
		writeError(w, http.StatusBadRequest, "invalid_message_type", "message is not voice type")
		return
	}

	if cachedText := extractVoiceTextFromBody(msg.Body); cachedText != "" {
		writeJSON(w, http.StatusOK, voiceMessageTranscribeResponse{
			Text:   cachedText,
			Cached: true,
		})
		return
	}

	text, transcribeErr := a.transcribeVoiceMessageURL(msg.MediaURL)
	if transcribeErr != nil {
		writeVoiceTranscribeError(w, transcribeErr)
		return
	}
	updatedBody, err := mergeVoiceTextIntoBody(msg.Body, text)
	if err != nil {
		writeVoiceTranscribeError(w, err)
		return
	}

	saveCtx, saveCancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer saveCancel()
	if err := a.groupMsgs.UpdateMessageBody(saveCtx, msg.ID, updatedBody); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "message_not_found", "message not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, voiceMessageTranscribeResponse{
		Text:   text,
		Cached: false,
	})
}

func writeVoiceTranscribeError(w http.ResponseWriter, err error) {
	var httpErr voiceTranscribeHTTPError
	if e, ok := err.(voiceTranscribeHTTPError); ok {
		httpErr = e
	} else {
		httpErr = voiceTranscribeHTTPError{
			Status:  http.StatusInternalServerError,
			Code:    "asr_internal_error",
			Message: "internal error",
		}
	}
	writeError(w, httpErr.Status, httpErr.Code, httpErr.Message)
}
