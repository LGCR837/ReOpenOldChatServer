package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"metrochat/internal/data"
)

const (
	externalCoinMaxAmount      = 1000000
	externalCoinMaxOrderNoLen  = 64
	externalCoinMaxRemarkRunes = 120
)

type externalCoinPayRequest struct {
	externalAuthRequest
	ToAccount     string `json:"to_account"`
	ToUserID      string `json:"to_user_id"`
	ToUID         string `json:"to_uid"`
	Amount        int    `json:"amount"`
	ClientOrderNo string `json:"client_order_no"`
	Remark        string `json:"remark"`
}

type externalCoinPayResponse struct {
	TransferID    string `json:"transfer_id"`
	ClientOrderNo string `json:"client_order_no,omitempty"`
	FromUID       string `json:"from_uid"`
	ToUID         string `json:"to_uid"`
	Amount        int    `json:"amount"`
	Remark        string `json:"remark,omitempty"`
	Status        string `json:"status"`
	AlreadyPaid   bool   `json:"already_paid"`
	CreatedAt     int64  `json:"created_at"`
	PayerBalance  int    `json:"payer_balance"`
}

type externalCoinVerifyRequest struct {
	externalAuthRequest
	TransferID    string `json:"transfer_id"`
	ClientOrderNo string `json:"client_order_no"`
	FromAccount   string `json:"from_account"`
	FromUserID    string `json:"from_user_id"`
	FromUID       string `json:"from_uid"`
}

type externalCoinVerifyResponse struct {
	Received      bool   `json:"received"`
	Status        string `json:"status"`
	TransferID    string `json:"transfer_id,omitempty"`
	ClientOrderNo string `json:"client_order_no,omitempty"`
	FromUID       string `json:"from_uid,omitempty"`
	ToUID         string `json:"to_uid,omitempty"`
	Amount        int    `json:"amount,omitempty"`
	Remark        string `json:"remark,omitempty"`
	CreatedAt     int64  `json:"created_at,omitempty"`
}

func (a *API) handleExternalCoinPay(w http.ResponseWriter, r *http.Request) {
	var req externalCoinPayRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	req.applyQuery(r)
	req.normalize()

	payer, ok := a.authenticateExternal(w, r, &req.externalAuthRequest)
	if !ok {
		return
	}

	if req.Amount <= 0 || req.Amount > externalCoinMaxAmount {
		writeError(w, http.StatusBadRequest, "invalid_amount", "invalid amount")
		return
	}
	if req.ClientOrderNo != "" && len(req.ClientOrderNo) > externalCoinMaxOrderNoLen {
		writeError(w, http.StatusBadRequest, "invalid_order_no", "invalid order no")
		return
	}
	if runeLen(req.Remark) > externalCoinMaxRemarkRunes {
		writeError(w, http.StatusBadRequest, "remark_too_long", "remark too long")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	payee, err := a.resolveExternalTargetUser(ctx, req.ToUserID, req.ToUID, req.ToAccount)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusNotFound, "target_not_found", "target not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if payee.ID == payer.ID {
		writeError(w, http.StatusBadRequest, "self_transfer_not_allowed", "cannot transfer to self")
		return
	}

	if banned, bErr := a.devices.IsUserBanned(ctx, payee.ID); bErr == nil && banned {
		writeError(w, http.StatusForbidden, "target_banned", "target user banned")
		return
	}

	transfer, payerBalance, created, err := a.coinTransfers.Transfer(
		ctx,
		payer.ID,
		payee.ID,
		req.Amount,
		req.ClientOrderNo,
		req.Remark,
	)
	if err != nil {
		if errors.Is(err, data.ErrInsufficientBalance) {
			writeError(w, http.StatusConflict, "insufficient_balance", "insufficient balance")
			return
		}
		if errors.Is(err, data.ErrClientOrderConflict) {
			writeError(w, http.StatusConflict, "order_conflict", "client order conflict")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := externalCoinPayResponse{
		TransferID:    transfer.ID,
		ClientOrderNo: transfer.ClientOrderNo,
		FromUID:       transfer.PayerUID,
		ToUID:         transfer.PayeeUID,
		Amount:        transfer.Amount,
		Remark:        transfer.Remark,
		Status:        "paid",
		AlreadyPaid:   !created,
		CreatedAt:     transfer.CreatedAt.Unix(),
		PayerBalance:  payerBalance,
	}
	if created {
		writeJSON(w, http.StatusCreated, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleExternalCoinVerify(w http.ResponseWriter, r *http.Request) {
	var req externalCoinVerifyRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	req.applyQuery(r)
	req.normalize()

	receiver, ok := a.authenticateExternal(w, r, &req.externalAuthRequest)
	if !ok {
		return
	}
	if req.TransferID == "" && req.ClientOrderNo == "" {
		writeError(w, http.StatusBadRequest, "invalid_query", "missing transfer_id or client_order_no")
		return
	}
	if req.ClientOrderNo != "" && len(req.ClientOrderNo) > externalCoinMaxOrderNoLen {
		writeError(w, http.StatusBadRequest, "invalid_order_no", "invalid order no")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	filterFromID := ""
	if req.FromUserID != "" || req.FromUID != "" || req.FromAccount != "" {
		payer, err := a.resolveExternalTargetUser(ctx, req.FromUserID, req.FromUID, req.FromAccount)
		if err != nil {
			if errors.Is(err, data.ErrNotFound) {
				writeJSON(w, http.StatusOK, externalCoinVerifyResponse{Received: false, Status: "not_found"})
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		filterFromID = payer.ID
	}

	var transfer *data.ExternalCoinTransfer
	var err error
	if req.TransferID != "" {
		transfer, err = a.coinTransfers.GetByIDForPayee(ctx, receiver.ID, req.TransferID)
		if err != nil {
			if errors.Is(err, data.ErrNotFound) {
				writeJSON(w, http.StatusOK, externalCoinVerifyResponse{Received: false, Status: "not_found"})
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		if req.ClientOrderNo != "" && transfer.ClientOrderNo != req.ClientOrderNo {
			writeJSON(w, http.StatusOK, externalCoinVerifyResponse{Received: false, Status: "not_found"})
			return
		}
		if filterFromID != "" && transfer.PayerID != filterFromID {
			writeJSON(w, http.StatusOK, externalCoinVerifyResponse{Received: false, Status: "not_found"})
			return
		}
	} else {
		transfer, err = a.coinTransfers.GetLatestForPayeeByClientOrder(ctx, receiver.ID, req.ClientOrderNo, filterFromID)
		if err != nil {
			if errors.Is(err, data.ErrNotFound) {
				writeJSON(w, http.StatusOK, externalCoinVerifyResponse{Received: false, Status: "not_found"})
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
	}

	if err := a.coinTransfers.MarkVerifiedForPayee(ctx, receiver.ID, transfer.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, externalCoinVerifyResponse{
		Received:      true,
		Status:        transfer.Status,
		TransferID:    transfer.ID,
		ClientOrderNo: transfer.ClientOrderNo,
		FromUID:       transfer.PayerUID,
		ToUID:         transfer.PayeeUID,
		Amount:        transfer.Amount,
		Remark:        transfer.Remark,
		CreatedAt:     transfer.CreatedAt.Unix(),
	})
}

func (r *externalCoinPayRequest) applyQuery(req *http.Request) {
	r.externalAuthRequest.applyQuery(req)
	q := req.URL.Query()
	if r.ToAccount == "" {
		r.ToAccount = q.Get("to_account")
	}
	if r.ToUserID == "" {
		r.ToUserID = q.Get("to_user_id")
	}
	if r.ToUID == "" {
		r.ToUID = q.Get("to_uid")
	}
	if r.ClientOrderNo == "" {
		r.ClientOrderNo = q.Get("client_order_no")
	}
	if r.Remark == "" {
		r.Remark = q.Get("remark")
	}
	if r.Amount <= 0 {
		if amountText := strings.TrimSpace(q.Get("amount")); amountText != "" {
			if amount, err := strconv.Atoi(amountText); err == nil {
				r.Amount = amount
			}
		}
	}
}

func (r *externalCoinPayRequest) normalize() {
	r.externalAuthRequest.normalize()
	r.ToAccount = strings.TrimSpace(r.ToAccount)
	r.ToUserID = strings.TrimSpace(r.ToUserID)
	r.ToUID = strings.ToUpper(strings.TrimSpace(r.ToUID))
	r.ClientOrderNo = strings.TrimSpace(r.ClientOrderNo)
	r.Remark = strings.TrimSpace(r.Remark)
}

func (r *externalCoinVerifyRequest) applyQuery(req *http.Request) {
	r.externalAuthRequest.applyQuery(req)
	q := req.URL.Query()
	if r.TransferID == "" {
		r.TransferID = q.Get("transfer_id")
	}
	if r.ClientOrderNo == "" {
		r.ClientOrderNo = q.Get("client_order_no")
	}
	if r.FromAccount == "" {
		r.FromAccount = q.Get("from_account")
	}
	if r.FromUserID == "" {
		r.FromUserID = q.Get("from_user_id")
	}
	if r.FromUID == "" {
		r.FromUID = q.Get("from_uid")
	}
}

func (r *externalCoinVerifyRequest) normalize() {
	r.externalAuthRequest.normalize()
	r.TransferID = strings.TrimSpace(r.TransferID)
	r.ClientOrderNo = strings.TrimSpace(r.ClientOrderNo)
	r.FromAccount = strings.TrimSpace(r.FromAccount)
	r.FromUserID = strings.TrimSpace(r.FromUserID)
	r.FromUID = strings.ToUpper(strings.TrimSpace(r.FromUID))
}

func (a *API) resolveExternalTargetUser(ctx context.Context, userID, uid, account string) (*data.User, error) {
	req := &externalAuthRequest{
		Account: strings.TrimSpace(account),
		UserID:  strings.TrimSpace(userID),
		UID:     strings.ToUpper(strings.TrimSpace(uid)),
	}
	req.normalize()
	return a.resolveExternalUser(ctx, req)
}

func runeLen(s string) int {
	count := 0
	for range s {
		count++
	}
	return count
}
