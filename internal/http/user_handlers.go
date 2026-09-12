package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"metrochat/internal/data"
)

func (a *API) handleUserProfile(w http.ResponseWriter, r *http.Request) {
	_, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	uid := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("uid")))
	if uid == "" || !isValidUID(uid) {
		writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	user, err := a.users.GetByUID(ctx, uid)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, toUserResponse(user))
}
