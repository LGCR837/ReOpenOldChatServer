-- OldChat Server Database Schema

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(32) PRIMARY KEY,
    uid VARCHAR(32) NOT NULL UNIQUE,
    uid_changed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    email VARCHAR(255) NOT NULL DEFAULT '',
    username VARCHAR(64) NOT NULL UNIQUE,
    display_name VARCHAR(64) NOT NULL DEFAULT '',
    user_title VARCHAR(64) NOT NULL DEFAULT '',
    user_title_price INTEGER NOT NULL DEFAULT 0,
    avatar_url VARCHAR(1024) NOT NULL DEFAULT '',
    signature VARCHAR(255) NOT NULL DEFAULT '',
    cover_url VARCHAR(1024) NOT NULL DEFAULT '',
    password_hash VARCHAR(255) NOT NULL,
    token_version INTEGER NOT NULL DEFAULT 0,
    coin_balance INTEGER NOT NULL DEFAULT 10,
    reputation_score INTEGER NOT NULL DEFAULT 100,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);
CREATE INDEX IF NOT EXISTS idx_users_username ON users (username);
CREATE INDEX IF NOT EXISTS idx_users_created ON users (created_at);

-- Direct threads table
CREATE TABLE IF NOT EXISTS direct_threads (
    id VARCHAR(32) PRIMARY KEY,
    user_a_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_b_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_a_id, user_b_id)
);

CREATE INDEX IF NOT EXISTS idx_direct_threads_user_a ON direct_threads (user_a_id);
CREATE INDEX IF NOT EXISTS idx_direct_threads_user_b ON direct_threads (user_b_id);

-- Direct messages table
CREATE TABLE IF NOT EXISTS direct_messages (
    id VARCHAR(32) PRIMARY KEY,
    thread_id VARCHAR(32) NOT NULL REFERENCES direct_threads(id) ON DELETE CASCADE,
    sender_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    body TEXT NOT NULL,
    msg_type TEXT NOT NULL DEFAULT 'text',
    media_url TEXT NOT NULL DEFAULT '',
    thumb_url TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    delivered_at DATETIME NULL,
    read_at DATETIME NULL
);

CREATE INDEX IF NOT EXISTS idx_direct_messages_thread ON direct_messages (thread_id);
CREATE INDEX IF NOT EXISTS idx_direct_messages_sender ON direct_messages (sender_id);
CREATE INDEX IF NOT EXISTS idx_direct_messages_created ON direct_messages (created_at);
CREATE INDEX IF NOT EXISTS idx_direct_messages_read ON direct_messages (read_at);

-- Groups table
CREATE TABLE IF NOT EXISTS groups (
    id VARCHAR(32) PRIMARY KEY,
    name VARCHAR(64) NOT NULL,
    avatar_url VARCHAR(1024) NOT NULL DEFAULT '',
    owner_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    join_approval INTEGER NOT NULL DEFAULT 0,
    global_mute INTEGER NOT NULL DEFAULT 0,
    announcement TEXT NOT NULL DEFAULT '',
    announcement_mode INTEGER NOT NULL DEFAULT 0,
    announcement_updated_at DATETIME NOT NULL DEFAULT '1970-01-01 00:00:00',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_groups_owner ON groups (owner_id);
CREATE INDEX IF NOT EXISTS idx_groups_created ON groups (created_at);

-- Group members table
CREATE TABLE IF NOT EXISTS group_members (
    group_id VARCHAR(32) NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role INTEGER NOT NULL DEFAULT 0,
    joined_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_read_at DATETIME NULL,
    announcement_read_at DATETIME NULL,
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members (user_id);

-- Group messages table
CREATE TABLE IF NOT EXISTS group_messages (
    id VARCHAR(32) PRIMARY KEY,
    group_id VARCHAR(32) NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    sender_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    body TEXT NOT NULL,
    msg_type TEXT NOT NULL DEFAULT 'text',
    media_url TEXT NOT NULL DEFAULT '',
    thumb_url TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_group_messages_group ON group_messages (group_id);
CREATE INDEX IF NOT EXISTS idx_group_messages_sender ON group_messages (sender_id);
CREATE INDEX IF NOT EXISTS idx_group_messages_created ON group_messages (created_at);

-- Group join requests table
CREATE TABLE IF NOT EXISTS group_join_requests (
    id VARCHAR(32) PRIMARY KEY,
    group_id VARCHAR(32) NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    responded_at DATETIME NULL
);

CREATE INDEX IF NOT EXISTS idx_group_join_requests_group ON group_join_requests (group_id, status);
CREATE INDEX IF NOT EXISTS idx_group_join_requests_user ON group_join_requests (user_id, status);

-- Friends table
CREATE TABLE IF NOT EXISTS friends (
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    friend_user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    remark_name VARCHAR(64) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, friend_user_id)
);

CREATE INDEX IF NOT EXISTS idx_friends_friend ON friends (friend_user_id);

-- Friend requests table
CREATE TABLE IF NOT EXISTS friend_requests (
    id VARCHAR(32) PRIMARY KEY,
    from_user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    to_user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    responded_at DATETIME NULL
);

CREATE INDEX IF NOT EXISTS idx_friend_requests_from ON friend_requests (from_user_id, status);
CREATE INDEX IF NOT EXISTS idx_friend_requests_to ON friend_requests (to_user_id, status);

-- Moments table
CREATE TABLE IF NOT EXISTS moments (
    id VARCHAR(32) PRIMARY KEY,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    image_url VARCHAR(1024) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_moments_user ON moments (user_id);
CREATE INDEX IF NOT EXISTS idx_moments_created ON moments (created_at);

-- Moment comments table
CREATE TABLE IF NOT EXISTS moment_comments (
    id VARCHAR(32) PRIMARY KEY,
    moment_id VARCHAR(32) NOT NULL REFERENCES moments(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_moment_comments_moment ON moment_comments (moment_id);
CREATE INDEX IF NOT EXISTS idx_moment_comments_user ON moment_comments (user_id, created_at);

-- Moment likes table
CREATE TABLE IF NOT EXISTS moment_likes (
    moment_id VARCHAR(32) NOT NULL REFERENCES moments(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (moment_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_moment_likes_moment ON moment_likes (moment_id);

-- Refresh tokens table
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id VARCHAR(32) PRIMARY KEY,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    revoked_at DATETIME NULL,
    replaced_by VARCHAR(32) NULL
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens (user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_hash ON refresh_tokens (token_hash);

-- System user
INSERT OR IGNORE INTO users (id, uid, username, display_name, email, password_hash, created_at)
VALUES ('SYSTEM', 'SYSTEM', '系统通知', '系统通知', 'system@localhost', '', CURRENT_TIMESTAMP);
