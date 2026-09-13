package data

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"time"
)

const scratchSlotCount = 5

// 每日刮刮乐 5 个槽位各自独立掷点，权重合计 100。
// 下标一一对应：0 旧币 40%、1 旧币 30%、5 旧币 15%、10 旧币 10%、20 旧币 5%。
var (
	scratchRewards = []int{0, 1, 5, 10, 20}
	scratchWeights = []int{40, 30, 15, 10, 5}
)

type UserDailyScratchResult struct {
	AlreadyScratched bool
	ScratchDate      string
	Slots            []int
	TotalReward      int
	CoinBalance      int
}

// DailyScratch 开今日刮刮乐。与签到同构：UNIQUE(user_id, scratch_date) + ON CONFLICT DO NOTHING
// 保证每天只发一次奖；重复调用返回首次结果而非重新掷点。
func (s *UserStore) DailyScratch(ctx context.Context, userID string) (*UserDailyScratchResult, error) {
	if userID == "" {
		return nil, ErrNotFound
	}
	today := time.Now().Local().Format("2006-01-02")

	slots := rollScratchSlots()
	total := 0
	for _, v := range slots {
		total += v
	}
	slotsJSON, err := json.Marshal(slots)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `
INSERT INTO user_daily_scratches (id, user_id, scratch_date, slots, total_reward, created_at)
VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
ON CONFLICT(user_id, scratch_date) DO NOTHING
`, NewID(), userID, today, string(slotsJSON), total)
	if err != nil {
		return nil, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}

	// rows==0 说明今天已开过，本次掷点作废，稍后按库中记录回放
	already := rows == 0
	if !already {
		if _, err = tx.ExecContext(ctx, `
UPDATE users
SET coin_balance = coin_balance + $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, total, userID); err != nil {
			return nil, err
		}
	}

	out, err := scanDailyScratch(tx.QueryRowContext(ctx, dailyScratchQuery, userID, today))
	if err != nil {
		return nil, err
	}
	out.AlreadyScratched = already

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return out, nil
}

// GetDailyScratch 查今日刮刮乐状态，未开过则返回未开状态（不掷点）。
func (s *UserStore) GetDailyScratch(ctx context.Context, userID string) (*UserDailyScratchResult, error) {
	if userID == "" {
		return nil, ErrNotFound
	}
	today := time.Now().Local().Format("2006-01-02")

	out, err := scanDailyScratch(s.db.QueryRowContext(ctx, dailyScratchQuery, userID, today))
	if err == nil {
		out.AlreadyScratched = true
		return out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	var balance int
	if err := s.db.QueryRowContext(ctx, `SELECT coin_balance FROM users WHERE id = $1`, userID).Scan(&balance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &UserDailyScratchResult{
		AlreadyScratched: false,
		ScratchDate:      today,
		Slots:            []int{},
		CoinBalance:      balance,
	}, nil
}

const dailyScratchQuery = `
SELECT s.slots, s.total_reward, u.coin_balance
FROM user_daily_scratches s
JOIN users u ON u.id = s.user_id
WHERE s.user_id = $1 AND s.scratch_date = $2`

func scanDailyScratch(row *sql.Row) (*UserDailyScratchResult, error) {
	var (
		raw     string
		total   int
		balance int
	)
	if err := row.Scan(&raw, &total, &balance); err != nil {
		return nil, err
	}
	out := &UserDailyScratchResult{TotalReward: total, CoinBalance: balance, Slots: []int{}}
	if err := json.Unmarshal([]byte(raw), &out.Slots); err != nil {
		out.Slots = []int{}
	}
	return out, nil
}

func rollScratchSlots() []int {
	out := make([]int, 0, scratchSlotCount)
	for i := 0; i < scratchSlotCount; i++ {
		out = append(out, drawScratchReward())
	}
	return out
}

// drawScratchReward 按权重掷一个槽位。用 crypto/rand 而非 math/rand，
// 避免奖励序列可被客户端按时间种子预测。
func drawScratchReward() int {
	total := 0
	for _, w := range scratchWeights {
		total += w
	}
	if total <= 0 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(total)))
	if err != nil {
		return 0
	}
	roll := int(n.Int64())
	for i, w := range scratchWeights {
		if roll < w {
			return scratchRewards[i]
		}
		roll -= w
	}
	return 0
}

// GroupInviteReject 查是否拒绝接收群邀请。
func (s *UserStore) GroupInviteReject(ctx context.Context, userID string) (bool, error) {
	var reject int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(group_invite_reject, 0) FROM users WHERE id = $1`, userID).Scan(&reject)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	return reject != 0, nil
}

// SetGroupInviteReject 设置群邀请偏好。
func (s *UserStore) SetGroupInviteReject(ctx context.Context, userID string, reject bool) error {
	v := 0
	if reject {
		v = 1
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE users
SET group_invite_reject = $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, v, userID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
