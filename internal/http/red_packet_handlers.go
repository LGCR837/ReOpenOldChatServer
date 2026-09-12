package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"math/rand"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aidarkhanov/nanoid"
	"github.com/go-chi/chi/v5"

	"metrochat/internal/data"
)

const redPacketDefaultTitle = ""
const redPacketTitleMaxRunes = 20

func init() {
	rand.Seed(time.Now().UnixNano())
}

type redPacketSendRequest struct {
	ToUID       string `json:"to_uid"`
	GroupID     string `json:"group_id"`
	Title       string `json:"title"`
	TotalAmount int    `json:"total_amount"`
	TotalCount  int    `json:"total_count"`
	CoverURL    string `json:"cover_url,omitempty"`
}

type redPacketClaimRequest struct {
	PacketID string `json:"packet_id"`
}

type redPacketClaimResponse struct {
	PacketID        string `json:"packet_id"`
	Amount          int    `json:"amount"`
	Balance         int    `json:"balance"`
	RemainingAmount int    `json:"remaining_amount"`
	RemainingCount  int    `json:"remaining_count"`
}

type redPacketDetailResponse struct {
	ID              string               `json:"id"`
	Title           string               `json:"title"`
	CreatorUID      string               `json:"creator_uid"`
	TotalAmount     int                  `json:"total_amount"`
	TotalCount      int                  `json:"total_count"`
	RemainingAmount int                  `json:"remaining_amount"`
	RemainingCount  int                  `json:"remaining_count"`
	ClaimedAmount   int                  `json:"claimed_amount"`
	ClaimedCount    int                  `json:"claimed_count"`
	Status          string               `json:"status"`
	CreatedAt       int64                `json:"created_at"`
	MyClaimAmount   int                  `json:"my_claim_amount"`
	CanClaim        bool                 `json:"can_claim"`
	Claims          []redPacketClaimItem `json:"claims"`
	CoverURL        string               `json:"cover_url,omitempty"`
}

type redPacketClaimItem struct {
	UID         string `json:"uid"`
	DisplayName string `json:"display_name"`
	Amount      int    `json:"amount"`
	CreatedAt   int64  `json:"created_at"`
}

type redPacketRow struct {
	ID              string         `db:"id"`
	CreatorID       string         `db:"creator_id"`
	CreatorUID      string         `db:"creator_uid"`
	GroupID         sql.NullString `db:"group_id"`
	ToUserID        sql.NullString `db:"to_user_id"`
	Title           string         `db:"title"`
	CoverURL        string         `db:"cover_url"`
	TotalAmount     int            `db:"total_amount"`
	TotalCount      int            `db:"total_count"`
	RemainingAmount int            `db:"remaining_amount"`
	RemainingCount  int            `db:"remaining_count"`
	Status          string         `db:"status"`
	CreatedAt       time.Time      `db:"created_at"`
}

func (a *API) handleRedPacketSend(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req redPacketSendRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	toUID := strings.ToUpper(strings.TrimSpace(req.ToUID))
	groupID := strings.ToUpper(strings.TrimSpace(req.GroupID))
	title := strings.TrimSpace(req.Title)
	coverURL := strings.TrimSpace(req.CoverURL)
	totalAmount := req.TotalAmount
	totalCount := req.TotalCount
	if utf8.RuneCountInString(title) > redPacketTitleMaxRunes {
		writeError(w, http.StatusBadRequest, "red_packet_title_too_long", "title too long")
		return
	}

	if title == "" {
		title = redPacketDefaultTitle
	}
	if !isValidImageURL(coverURL) {
		writeError(w, http.StatusBadRequest, "invalid_cover_url", "invalid cover url")
		return
	}

	if groupID != "" && toUID != "" {
		writeError(w, http.StatusBadRequest, "red_packet_invalid", "invalid target")
		return
	}

	isGroup := groupID != ""
	if isGroup {
		if !isValidGroupID(groupID) {
			writeError(w, http.StatusBadRequest, "invalid_group_id", "invalid group id")
			return
		}
		if totalCount < 2 {
			writeError(w, http.StatusBadRequest, "red_packet_count_invalid", "invalid count")
			return
		}
	} else {
		if !isValidUID(toUID) {
			writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
			return
		}
		if totalCount <= 0 {
			totalCount = 1
		}
		if totalCount != 1 {
			writeError(w, http.StatusBadRequest, "red_packet_count_invalid", "invalid count")
			return
		}
	}

	if totalAmount <= 0 {
		writeError(w, http.StatusBadRequest, "red_packet_amount_invalid", "invalid amount")
		return
	}
	if totalAmount < totalCount {
		writeError(w, http.StatusBadRequest, "red_packet_amount_too_small", "amount too small")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	currentUser, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	var targetUser *data.User
	var threadID string
	if isGroup {
		group, err := a.groups.GetByID(ctx, groupID)
		if err != nil {
			if err == data.ErrNotFound {
				writeError(w, http.StatusNotFound, "group_not_found", "group not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		role, err := a.groups.GetRole(ctx, groupID, claims.Subject)
		if err != nil {
			if err == data.ErrNotFound {
				writeError(w, http.StatusForbidden, "red_packet_no_permission", "not a member")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		if group.GlobalMute && role < data.GroupRoleAdmin {
			writeError(w, http.StatusForbidden, "group_muted", "group is muted")
			return
		}
	} else {
		if currentUser.UID == toUID {
			writeError(w, http.StatusBadRequest, "invalid_uid", "cannot send to self")
			return
		}
		targetUser, err = a.users.GetByUID(ctx, toUID)
		if err != nil {
			if err == data.ErrNotFound {
				writeError(w, http.StatusNotFound, "user_not_found", "user not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		areFriends, err := a.friends.AreFriends(ctx, currentUser.ID, targetUser.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		if !areFriends {
			writeError(w, http.StatusForbidden, "not_friends", "not friends")
			return
		}
		threadID, err = a.direct.GetThreadID(ctx, currentUser.ID, targetUser.ID)
		if err != nil {
			if err == data.ErrNotFound {
				newID := nanoid.New()
				threadID, err = a.direct.GetOrCreateThread(ctx, currentUser.ID, targetUser.ID, newID)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "db_error", "internal error")
					return
				}
			} else {
				writeError(w, http.StatusInternalServerError, "db_error", "internal error")
				return
			}
		}
	}

	tx, err := a.db.BeginTxx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `
UPDATE users
SET coin_balance = coin_balance - $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2 AND coin_balance >= $1
`, totalAmount, currentUser.ID)
	if err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		_ = tx.Rollback()
		writeError(w, http.StatusConflict, "red_packet_insufficient", "insufficient balance")
		return
	}

	packetID := nanoid.New()
	msgID := nanoid.New()
	body := buildRedPacketBody(title, packetID, totalAmount, totalCount, coverURL)

	var groupIDValue interface{}
	var toUserIDValue interface{}
	if isGroup {
		groupIDValue = groupID
		toUserIDValue = nil
	} else {
		groupIDValue = nil
		toUserIDValue = targetUser.ID
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO red_packets (
    id, creator_id, group_id, to_user_id, title, cover_url,
    total_amount, total_count, remaining_amount, remaining_count, status, created_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $7, $8, 'active', CURRENT_TIMESTAMP
)
`, packetID, currentUser.ID, groupIDValue, toUserIDValue, title, coverURL, totalAmount, totalCount)
	if err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if isGroup {
		_, err = tx.ExecContext(ctx, `
INSERT INTO group_messages (id, group_id, sender_id, body, msg_type, media_url, thumb_url, duration_ms, created_at)
VALUES ($1, $2, $3, $4, $5, '', '', 0, CURRENT_TIMESTAMP)
`, msgID, groupID, currentUser.ID, body, "red_packet")
	} else {
		_, err = tx.ExecContext(ctx, `
INSERT INTO direct_messages (id, thread_id, sender_id, body, msg_type, media_url, thumb_url, duration_ms, created_at)
VALUES ($1, $2, $3, $4, $5, '', '', 0, CURRENT_TIMESTAMP)
`, msgID, threadID, currentUser.ID, body, "red_packet")
	}
	if err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if isGroup {
		resp := groupMessageResponse{
			ID:        msgID,
			GroupID:   groupID,
			FromUID:   currentUser.UID,
			Body:      body,
			MsgType:   "red_packet",
			CreatedAt: time.Now().Unix(),
		}
		writeJSON(w, http.StatusCreated, resp)
		chatLogf("%s GRP %s | %s: %s", time.Now().Format("15:04:05"), groupID, claims.UID, formatChatPreview("red_packet", body))
		env := wsEnvelope{
			Type: "group_message",
			Data: resp,
		}
		payload, err := json.Marshal(env)
		if err != nil {
			return
		}
		memberIDs, err := a.groups.ListMemberIDs(ctx, groupID)
		if err != nil {
			return
		}
		for _, memberID := range memberIDs {
			if memberID == claims.Subject {
				continue
			}
			a.wsHub.BroadcastToUser(memberID, payload)
		}
		return
	}

	resp := directMessageResponse{
		ID:        msgID,
		ThreadID:  threadID,
		FromUID:   currentUser.UID,
		Body:      body,
		MsgType:   "red_packet",
		CreatedAt: time.Now().Unix(),
	}
	writeJSON(w, http.StatusCreated, resp)
	chatLogf("%s DM %s -> %s | %s", time.Now().Format("15:04:05"), currentUser.UID, targetUser.UID, formatChatPreview("red_packet", body))
	env := wsEnvelope{
		Type: "direct_message",
		Data: resp,
	}
	payload, err := json.Marshal(env)
	if err == nil {
		a.wsHub.BroadcastToUser(targetUser.ID, payload)
	}
}

func (a *API) handleRedPacketClaim(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req redPacketClaimRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	packetID := strings.TrimSpace(req.PacketID)
	if packetID == "" {
		writeError(w, http.StatusBadRequest, "red_packet_invalid", "invalid packet")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var packet redPacketRow
	err := a.db.GetContext(ctx, &packet, `
SELECT rp.id, rp.creator_id, u.uid AS creator_uid, rp.group_id, rp.to_user_id,
       rp.title, rp.cover_url, rp.total_amount, rp.total_count, rp.remaining_amount, rp.remaining_count,
       rp.status, rp.created_at
FROM red_packets rp
JOIN users u ON u.id = rp.creator_id
WHERE rp.id = $1
LIMIT 1
`, packetID)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "red_packet_invalid", "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if packet.CreatorID == claims.Subject {
		writeError(w, http.StatusForbidden, "red_packet_no_permission", "no permission")
		return
	}

	if packet.GroupID.Valid {
		if _, err := a.groups.GetRole(ctx, packet.GroupID.String, claims.Subject); err != nil {
			if err == data.ErrNotFound {
				writeError(w, http.StatusForbidden, "red_packet_no_permission", "not a member")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
	} else {
		if !packet.ToUserID.Valid || packet.ToUserID.String != claims.Subject {
			writeError(w, http.StatusForbidden, "red_packet_no_permission", "no permission")
			return
		}
	}

	tx, err := a.db.BeginTxx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var claimedAmount int
	err = tx.GetContext(ctx, &claimedAmount, `
SELECT amount
FROM red_packet_claims
WHERE packet_id = $1 AND user_id = $2
LIMIT 1
`, packetID, claims.Subject)
	if err == nil {
		_ = tx.Rollback()
		writeError(w, http.StatusConflict, "red_packet_already_claimed", "already claimed")
		return
	}
	if err != nil && err != sql.ErrNoRows {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if packet.Status != "active" || packet.RemainingCount <= 0 || packet.RemainingAmount <= 0 {
		_ = tx.Rollback()
		writeError(w, http.StatusConflict, "red_packet_empty", "empty")
		return
	}

	amount := calcRedPacketAmount(packet.RemainingAmount, packet.RemainingCount)
	newRemainingCount := packet.RemainingCount - 1
	newRemainingAmount := packet.RemainingAmount - amount
	status := packet.Status
	if newRemainingCount <= 0 || newRemainingAmount <= 0 {
		newRemainingCount = 0
		newRemainingAmount = 0
		status = "done"
	}

	res, err := tx.ExecContext(ctx, `
UPDATE red_packets
SET remaining_amount = $1, remaining_count = $2, status = $3
WHERE id = $4 AND remaining_amount = $5 AND remaining_count = $6 AND status = $7
`, newRemainingAmount, newRemainingCount, status, packet.ID, packet.RemainingAmount, packet.RemainingCount, packet.Status)
	if err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		_ = tx.Rollback()
		writeError(w, http.StatusConflict, "red_packet_empty", "empty")
		return
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO red_packet_claims (id, packet_id, user_id, amount, created_at)
VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
`, nanoid.New(), packetID, claims.Subject, amount)
	if err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	_, err = tx.ExecContext(ctx, `
UPDATE users
SET coin_balance = coin_balance + $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, amount, claims.Subject)
	if err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	var balance int
	if err = tx.GetContext(ctx, &balance, `SELECT coin_balance FROM users WHERE id = $1`, claims.Subject); err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, redPacketClaimResponse{
		PacketID:        packetID,
		Amount:          amount,
		Balance:         balance,
		RemainingAmount: newRemainingAmount,
		RemainingCount:  newRemainingCount,
	})
}

func (a *API) handleRedPacketDetail(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	packetID := strings.TrimSpace(chiURLParam(r, "packetID"))
	if packetID == "" {
		writeError(w, http.StatusBadRequest, "red_packet_invalid", "invalid packet")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var packet redPacketRow
	err := a.db.GetContext(ctx, &packet, `
SELECT rp.id, rp.creator_id, u.uid AS creator_uid, rp.group_id, rp.to_user_id,
       rp.title, rp.cover_url, rp.total_amount, rp.total_count, rp.remaining_amount, rp.remaining_count,
       rp.status, rp.created_at
FROM red_packets rp
JOIN users u ON u.id = rp.creator_id
WHERE rp.id = $1
LIMIT 1
`, packetID)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "red_packet_invalid", "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if packet.GroupID.Valid {
		if _, err := a.groups.GetRole(ctx, packet.GroupID.String, claims.Subject); err != nil {
			if err == data.ErrNotFound {
				writeError(w, http.StatusForbidden, "red_packet_no_permission", "not a member")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
	} else {
		if packet.CreatorID != claims.Subject && (!packet.ToUserID.Valid || packet.ToUserID.String != claims.Subject) {
			writeError(w, http.StatusForbidden, "red_packet_no_permission", "no permission")
			return
		}
	}

	myClaimAmount := 0
	if err := a.db.GetContext(ctx, &myClaimAmount, `
SELECT amount
FROM red_packet_claims
WHERE packet_id = $1 AND user_id = $2
LIMIT 1
`, packetID, claims.Subject); err != nil && err != sql.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	canClaim := true
	if packet.CreatorID == claims.Subject {
		canClaim = false
	}
	if myClaimAmount > 0 {
		canClaim = false
	}
	if packet.Status != "active" || packet.RemainingCount <= 0 || packet.RemainingAmount <= 0 {
		canClaim = false
	}
	if !packet.GroupID.Valid && (!packet.ToUserID.Valid || packet.ToUserID.String != claims.Subject) {
		canClaim = false
	}

	claimsList := []redPacketClaimItem{}
	showClaims := packet.GroupID.Valid ||
		packet.CreatorID == claims.Subject ||
		(packet.ToUserID.Valid && packet.ToUserID.String == claims.Subject)
	if showClaims {
		rows := []struct {
			UID         string    `db:"uid"`
			DisplayName string    `db:"display_name"`
			Username    string    `db:"username"`
			Amount      int       `db:"amount"`
			CreatedAt   time.Time `db:"created_at"`
		}{}
		err = a.db.SelectContext(ctx, &rows, `
SELECT u.uid, u.display_name, u.username, c.amount, c.created_at
FROM red_packet_claims c
JOIN users u ON u.id = c.user_id
WHERE c.packet_id = $1
ORDER BY c.created_at ASC
`, packetID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		for _, row := range rows {
			name := strings.TrimSpace(row.DisplayName)
			if name == "" {
				name = row.UID
			}
			if name == "" {
				name = row.Username
			}
			claimsList = append(claimsList, redPacketClaimItem{
				UID:         row.UID,
				DisplayName: name,
				Amount:      row.Amount,
				CreatedAt:   row.CreatedAt.Unix(),
			})
		}
	}

	claimedAmount := packet.TotalAmount - packet.RemainingAmount
	claimedCount := packet.TotalCount - packet.RemainingCount
	if claimedAmount < 0 {
		claimedAmount = 0
	}
	if claimedCount < 0 {
		claimedCount = 0
	}

	title := strings.TrimSpace(packet.Title)
	if title == "" {
		title = redPacketDefaultTitle
	}

	writeJSON(w, http.StatusOK, redPacketDetailResponse{
		ID:              packet.ID,
		Title:           title,
		CreatorUID:      packet.CreatorUID,
		TotalAmount:     packet.TotalAmount,
		TotalCount:      packet.TotalCount,
		RemainingAmount: packet.RemainingAmount,
		RemainingCount:  packet.RemainingCount,
		ClaimedAmount:   claimedAmount,
		ClaimedCount:    claimedCount,
		Status:          packet.Status,
		CreatedAt:       packet.CreatedAt.Unix(),
		MyClaimAmount:   myClaimAmount,
		CanClaim:        canClaim,
		Claims:          claimsList,
		CoverURL:        strings.TrimSpace(packet.CoverURL),
	})
}

func buildRedPacketBody(title, packetID string, totalAmount, totalCount int, coverURL string) string {
	if title == "" {
		title = redPacketDefaultTitle
	}
	payload := map[string]interface{}{
		"v":            1,
		"text":         title,
		"packet_id":    packetID,
		"total_amount": totalAmount,
		"total_count":  totalCount,
	}
	if coverURL != "" {
		payload["cover_url"] = coverURL
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return title
	}
	return string(b)
}

func calcRedPacketAmount(remainingAmount, remainingCount int) int {
	if remainingCount <= 1 {
		return remainingAmount
	}
	if remainingAmount <= 0 {
		return 0
	}
	avg := remainingAmount / remainingCount
	if avg <= 0 {
		return 1
	}
	min := 1
	if avg > 1 {
		min = avg / 2
		if min < 1 {
			min = 1
		}
	}
	maxPossible := remainingAmount - (remainingCount - 1)
	if maxPossible < 1 {
		return 1
	}
	if min > maxPossible {
		min = maxPossible
	}
	upper := avg * 2
	if upper < min {
		upper = min
	}
	if upper > maxPossible {
		upper = maxPossible
	}
	if upper <= min {
		return min
	}
	return rand.Intn(upper-min+1) + min
}

func chiURLParam(r *http.Request, key string) string {
	return strings.TrimSpace(chi.URLParam(r, key))
}
