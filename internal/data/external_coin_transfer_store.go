package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

var ErrInsufficientBalance = errors.New("insufficient balance")
var ErrClientOrderConflict = errors.New("client order conflict")

type ExternalCoinTransferStore struct {
	db *sqlx.DB
}

type ExternalCoinTransfer struct {
	ID            string    `db:"id"`
	ClientOrderNo string    `db:"client_order_no"`
	PayerID       string    `db:"payer_id"`
	PayeeID       string    `db:"payee_id"`
	Amount        int       `db:"amount"`
	Remark        string    `db:"remark"`
	Status        string    `db:"status"`
	CreatedAt     time.Time `db:"created_at"`
	PayerUID      string    `db:"payer_uid"`
	PayeeUID      string    `db:"payee_uid"`
}

func NewExternalCoinTransferStore(db *sqlx.DB) *ExternalCoinTransferStore {
	return &ExternalCoinTransferStore{db: db}
}

func (s *ExternalCoinTransferStore) Transfer(ctx context.Context, payerID, payeeID string, amount int,
	clientOrderNo, remark string) (*ExternalCoinTransfer, int, bool, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, 0, false, err
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()

	if clientOrderNo != "" {
		existing, exErr := getByPayerAndClientOrderTx(ctx, tx, payerID, clientOrderNo)
		if exErr == nil {
			if existing.PayeeID != payeeID || existing.Amount != amount {
				return nil, 0, false, ErrClientOrderConflict
			}
			balance, bErr := readCoinBalanceTx(ctx, tx, payerID)
			if bErr != nil {
				return nil, 0, false, bErr
			}
			if err := tx.Commit(); err != nil {
				return nil, 0, false, err
			}
			rollback = false
			return existing, balance, false, nil
		}
		if !errors.Is(exErr, ErrNotFound) {
			return nil, 0, false, exErr
		}
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE users SET coin_balance = coin_balance - $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2 AND coin_balance >= $1`, amount, payerID)
	if err != nil {
		return nil, 0, false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, 0, false, err
	}
	if rows == 0 {
		return nil, 0, false, ErrInsufficientBalance
	}

	if _, err = tx.ExecContext(ctx,
		`UPDATE users SET coin_balance = coin_balance + $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2`, amount, payeeID); err != nil {
		return nil, 0, false, err
	}

	transferID := NewID()
	if _, err = tx.ExecContext(ctx, `
INSERT INTO external_coin_transfers
(id, client_order_no, payer_id, payee_id, amount, remark, status)
VALUES ($1, $2, $3, $4, $5, $6, 'paid')`,
		transferID, clientOrderNo, payerID, payeeID, amount, remark); err != nil {
		if clientOrderNo != "" {
			existing, exErr := s.getByPayerAndClientOrder(ctx, payerID, clientOrderNo)
			if exErr == nil {
				if existing.PayeeID != payeeID || existing.Amount != amount {
					return nil, 0, false, ErrClientOrderConflict
				}
				balance, bErr := s.readCoinBalance(ctx, payerID)
				if bErr != nil {
					return nil, 0, false, bErr
				}
				return existing, balance, false, nil
			}
		}
		return nil, 0, false, err
	}

	transfer, err := getByIDTx(ctx, tx, transferID)
	if err != nil {
		return nil, 0, false, err
	}
	balance, err := readCoinBalanceTx(ctx, tx, payerID)
	if err != nil {
		return nil, 0, false, err
	}

	if err := tx.Commit(); err != nil {
		return nil, 0, false, err
	}
	rollback = false
	return transfer, balance, true, nil
}

func (s *ExternalCoinTransferStore) GetByIDForPayee(ctx context.Context, payeeID, transferID string) (*ExternalCoinTransfer, error) {
	const q = `
SELECT t.id, t.client_order_no, t.payer_id, t.payee_id, t.amount, t.remark, t.status, t.created_at,
       pu.uid AS payer_uid,
       tu.uid AS payee_uid
FROM external_coin_transfers t
JOIN users pu ON pu.id = t.payer_id
JOIN users tu ON tu.id = t.payee_id
WHERE t.id = $1 AND t.payee_id = $2
LIMIT 1`
	var out ExternalCoinTransfer
	if err := s.db.GetContext(ctx, &out, q, transferID, payeeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

func (s *ExternalCoinTransferStore) GetLatestForPayeeByClientOrder(ctx context.Context,
	payeeID, clientOrderNo, payerID string) (*ExternalCoinTransfer, error) {
	if payerID != "" {
		const q = `
SELECT t.id, t.client_order_no, t.payer_id, t.payee_id, t.amount, t.remark, t.status, t.created_at,
       pu.uid AS payer_uid,
       tu.uid AS payee_uid
FROM external_coin_transfers t
JOIN users pu ON pu.id = t.payer_id
JOIN users tu ON tu.id = t.payee_id
WHERE t.payee_id = $1 AND t.client_order_no = $2 AND t.payer_id = $3
ORDER BY t.created_at DESC
LIMIT 1`
		var out ExternalCoinTransfer
		if err := s.db.GetContext(ctx, &out, q, payeeID, clientOrderNo, payerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		return &out, nil
	}

	const q = `
SELECT t.id, t.client_order_no, t.payer_id, t.payee_id, t.amount, t.remark, t.status, t.created_at,
       pu.uid AS payer_uid,
       tu.uid AS payee_uid
FROM external_coin_transfers t
JOIN users pu ON pu.id = t.payer_id
JOIN users tu ON tu.id = t.payee_id
WHERE t.payee_id = $1 AND t.client_order_no = $2
ORDER BY t.created_at DESC
LIMIT 1`
	var out ExternalCoinTransfer
	if err := s.db.GetContext(ctx, &out, q, payeeID, clientOrderNo); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

func (s *ExternalCoinTransferStore) MarkVerifiedForPayee(ctx context.Context, payeeID, transferID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE external_coin_transfers
SET verified_at = CURRENT_TIMESTAMP
WHERE id = $1 AND payee_id = $2 AND verified_at IS NULL`,
		transferID,
		payeeID,
	)
	return err
}

func (s *ExternalCoinTransferStore) getByPayerAndClientOrder(ctx context.Context, payerID, clientOrderNo string) (*ExternalCoinTransfer, error) {
	const q = `
SELECT t.id, t.client_order_no, t.payer_id, t.payee_id, t.amount, t.remark, t.status, t.created_at,
       pu.uid AS payer_uid,
       tu.uid AS payee_uid
FROM external_coin_transfers t
JOIN users pu ON pu.id = t.payer_id
JOIN users tu ON tu.id = t.payee_id
WHERE t.payer_id = $1 AND t.client_order_no = $2
LIMIT 1`
	var out ExternalCoinTransfer
	if err := s.db.GetContext(ctx, &out, q, payerID, clientOrderNo); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

func (s *ExternalCoinTransferStore) readCoinBalance(ctx context.Context, userID string) (int, error) {
	var balance int
	if err := s.db.GetContext(ctx, &balance, `SELECT coin_balance FROM users WHERE id = $1`, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return balance, nil
}

func getByPayerAndClientOrderTx(ctx context.Context, tx *sqlx.Tx, payerID, clientOrderNo string) (*ExternalCoinTransfer, error) {
	const q = `
SELECT t.id, t.client_order_no, t.payer_id, t.payee_id, t.amount, t.remark, t.status, t.created_at,
       pu.uid AS payer_uid,
       tu.uid AS payee_uid
FROM external_coin_transfers t
JOIN users pu ON pu.id = t.payer_id
JOIN users tu ON tu.id = t.payee_id
WHERE t.payer_id = $1 AND t.client_order_no = $2
LIMIT 1`
	var out ExternalCoinTransfer
	if err := tx.GetContext(ctx, &out, q, payerID, clientOrderNo); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

func getByIDTx(ctx context.Context, tx *sqlx.Tx, transferID string) (*ExternalCoinTransfer, error) {
	const q = `
SELECT t.id, t.client_order_no, t.payer_id, t.payee_id, t.amount, t.remark, t.status, t.created_at,
       pu.uid AS payer_uid,
       tu.uid AS payee_uid
FROM external_coin_transfers t
JOIN users pu ON pu.id = t.payer_id
JOIN users tu ON tu.id = t.payee_id
WHERE t.id = $1
LIMIT 1`
	var out ExternalCoinTransfer
	if err := tx.GetContext(ctx, &out, q, transferID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

func readCoinBalanceTx(ctx context.Context, tx *sqlx.Tx, userID string) (int, error) {
	var balance int
	if err := tx.GetContext(ctx, &balance, `SELECT coin_balance FROM users WHERE id = $1`, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return balance, nil
}
