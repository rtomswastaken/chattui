-- Schema migration for chatTUI
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT UNIQUE NOT NULL COLLATE NOCASE,
    display_name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    user_color TEXT NOT NULL DEFAULT 'Cyan',
    custom_status TEXT NOT NULL DEFAULT '',
    presence_state TEXT NOT NULL DEFAULT 'offline',
    last_seen_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS rooms (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL COLLATE NOCASE,
    description TEXT NOT NULL DEFAULT '',
    is_private INTEGER NOT NULL DEFAULT 0,
    password_hash TEXT,
    owner_id TEXT NOT NULL REFERENCES users(id),
    is_temporary INTEGER NOT NULL DEFAULT 0,
    expires_at DATETIME,
    created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS room_members (
    room_id TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    joined_at DATETIME NOT NULL,
    PRIMARY KEY (room_id, user_id)
);

CREATE TABLE IF NOT EXISTS room_bans (
    room_id TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    banned_by TEXT NOT NULL REFERENCES users(id),
    reason TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    PRIMARY KEY (room_id, user_id)
);

CREATE TABLE IF NOT EXISTS room_mutes (
    room_id TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    muted_by TEXT NOT NULL REFERENCES users(id),
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL,
    PRIMARY KEY (room_id, user_id)
);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    sender_id TEXT NOT NULL REFERENCES users(id),
    target_type TEXT NOT NULL, -- 'room' or 'dm'
    room_id TEXT REFERENCES rooms(id) ON DELETE CASCADE,
    recipient_id TEXT REFERENCES users(id),
    content TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    is_system INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS read_states (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    last_read_message_id TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL,
    PRIMARY KEY (user_id, target_type, target_id)
);

CREATE TABLE IF NOT EXISTS notifications (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    is_read INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL
);

-- Indexes for lightning fast lookups
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at);
CREATE INDEX IF NOT EXISTS idx_messages_room ON messages(room_id, created_at);
CREATE INDEX IF NOT EXISTS idx_messages_dm ON messages(sender_id, recipient_id, created_at);
CREATE INDEX IF NOT EXISTS idx_rooms_expires ON rooms(is_temporary, expires_at);
CREATE INDEX IF NOT EXISTS idx_room_members_user ON room_members(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id, is_read, created_at);
