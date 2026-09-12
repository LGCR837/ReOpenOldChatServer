package data

import "time"

type User struct {
	ID              string    `db:"id"`
	UID             string    `db:"uid"`
	UIDChangedAt    time.Time `db:"uid_changed_at"`
	Email           string    `db:"email"`
	Username        string    `db:"username"`
	DisplayName     string    `db:"display_name"`
	UserTitle       string    `db:"user_title"`
	UserTitlePrice  int       `db:"user_title_price"`
	AvatarURL       string    `db:"avatar_url"`
	Signature       string    `db:"signature"`
	CoverURL        string    `db:"cover_url"`
	PasswordHash    string    `db:"password_hash"`
	TokenVersion    int       `db:"token_version"`
	CoinBalance     int       `db:"coin_balance"`
	ReputationScore int       `db:"reputation_score"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}
