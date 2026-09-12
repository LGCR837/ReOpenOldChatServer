package data

import "time"

type FriendRequest struct {
	ID          string     `db:"id"`
	FromUserID  string     `db:"from_user_id"`
	ToUserID    string     `db:"to_user_id"`
	Status      int16      `db:"status"`
	CreatedAt   time.Time  `db:"created_at"`
	RespondedAt *time.Time `db:"responded_at"`
}

type FriendUser struct {
	ID            string    `db:"id"`
	UID           string    `db:"uid"`
	Username      string    `db:"username"`
	DisplayName   string    `db:"display_name"`
	RemarkName    string    `db:"remark_name"`
	UserTitle     string    `db:"user_title"`
	AvatarURL     string    `db:"avatar_url"`
	FriendAddedAt time.Time `db:"friend_created_at"`
}

type FriendRequestEntry struct {
	ID              string     `db:"id"`
	Status          int16      `db:"status"`
	FromUserID      string     `db:"from_user_id"`
	FromUID         string     `db:"from_uid"`
	FromUsername    string     `db:"from_username"`
	FromDisplayName string     `db:"from_display_name"`
	FromAvatarURL   string     `db:"from_avatar_url"`
	FromTitle       string     `db:"from_user_title"`
	CreatedAt       time.Time  `db:"created_at"`
	RespondedAt     *time.Time `db:"responded_at"`
}
