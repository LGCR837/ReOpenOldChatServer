package httpapi

import (
	"net/http"
	"strings"

	"github.com/gorilla/websocket"

	"metrochat/internal/auth"
	"metrochat/internal/ws"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (a *API) handleWS(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticateForWS(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	sessionID := strings.TrimSpace(r.URL.Query().Get("sid"))
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing_session", "missing session")
		return
	}
	keys, ok := a.sessions.Get(sessionID)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_session", "invalid session")
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := ws.NewClient(a.wsHub, conn, claims.Subject, keys.EncKey, keys.MacKey)
	client.Start()
}

func (a *API) authenticateForWS(r *http.Request) (*auth.AccessClaims, bool) {
	if claims, ok := a.authenticateFromHeader(r); ok {
		if !a.validateTokenVersion(r.Context(), claims.Subject, claims.Version) {
			return nil, false
		}
		return claims, true
	}
	queryToken := strings.TrimSpace(r.URL.Query().Get("token"))
	if queryToken == "" {
		return nil, false
	}
	claims, ok := a.authenticateFromToken(queryToken)
	if !ok {
		return nil, false
	}
	if !a.validateTokenVersion(r.Context(), claims.Subject, claims.Version) {
		return nil, false
	}
	return claims, true
}
