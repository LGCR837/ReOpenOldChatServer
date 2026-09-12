package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"metrochat/internal/data"
)

const (
	dailyCheckInCoinReward       = 10
	dailyCheckInReputationReward = 50
)

func (a *API) handleMeCheckIn(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	result, err := a.users.DailyCheckIn(ctx, claims.Subject, dailyCheckInCoinReward, dailyCheckInReputationReward)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"already_checked":   result.AlreadyChecked,
		"checkin_date":      result.CheckinDate,
		"coin_reward":       result.CoinReward,
		"reputation_reward": result.ReputationReward,
		"coin_balance":      result.CoinBalance,
		"reputation_score":  result.ReputationScore,
	})
}
