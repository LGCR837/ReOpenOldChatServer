package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"metrochat/internal/data"
)

func (a *API) handleNotificationList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	var before time.Time
	if v := r.URL.Query().Get("before"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			before = time.UnixMilli(ts)
		}
	}

	notifications, err := a.notifications.List(ctx, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", "failed to list notifications")
		return
	}

	type item struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		Important bool   `json:"important"`
		CreatedAt int64  `json:"created_at"`
	}

	items := make([]item, len(notifications))
	for i, n := range notifications {
		items[i] = item{
			ID:        n.ID,
			Title:     n.Title,
			Body:      n.Body,
			Important: n.Important,
			CreatedAt: n.CreatedAt.UnixMilli(),
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"notifications": items,
	})
}

func (a *API) handleAdminNotificationSend(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	body := strings.TrimSpace(r.FormValue("body"))
	important := r.FormValue("important") == "1" || r.FormValue("important") == "true"

	if body == "" {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	n := &data.SystemNotification{
		ID:        data.NewID(),
		Title:     title,
		Body:      body,
		Important: important,
	}

	if err := a.notifications.Create(ctx, n); err != nil {
		log.Printf("admin notification create failed: %v", err)
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}

	// Broadcast to all connected clients
	env := wsEnvelope{
		Type: "system_notification",
		Data: map[string]interface{}{
			"id":         n.ID,
			"title":      n.Title,
			"body":       n.Body,
			"important":  n.Important,
			"created_at": time.Now().UnixMilli(),
		},
	}
	if msg, err := json.Marshal(env); err == nil {
		a.wsHub.Broadcast(msg)
	} else {
		log.Printf("admin notification marshal failed: %v", err)
	}

	http.Redirect(w, r, "/admins", http.StatusSeeOther)
}

func (a *API) handleAdminNotificationDelete(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}

	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	_ = a.notifications.Delete(ctx, id)
	http.Redirect(w, r, "/admins", http.StatusSeeOther)
}
