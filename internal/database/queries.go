package database

import (
	"database/sql"
	"errors"
	"time"

	"github.com/rtoms/chattui/internal/models"
)

// User operations

func (db *DB) CreateUser(u *models.User) error {
	_, err := db.Exec(`
		INSERT INTO users (id, username, display_name, password_hash, user_color, custom_status, presence_state, last_seen_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, u.ID, u.Username, u.DisplayName, u.PasswordHash, u.UserColor, u.CustomStatus, u.Presence, u.LastSeenAt, u.CreatedAt)
	return err
}

func (db *DB) GetUserByID(id string) (*models.User, error) {
	row := db.QueryRow(`
		SELECT id, username, display_name, password_hash, user_color, custom_status, presence_state, last_seen_at, created_at
		FROM users WHERE id = ?
	`, id)
	var u models.User
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.UserColor, &u.CustomStatus, &u.Presence, &u.LastSeenAt, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (db *DB) GetUserByUsername(username string) (*models.User, error) {
	row := db.QueryRow(`
		SELECT id, username, display_name, password_hash, user_color, custom_status, presence_state, last_seen_at, created_at
		FROM users WHERE username = ? COLLATE NOCASE
	`, username)
	var u models.User
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.UserColor, &u.CustomStatus, &u.Presence, &u.LastSeenAt, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (db *DB) UpdateUserProfile(id string, displayName, userColor, customStatus, presence *string) error {
	query := "UPDATE users SET "
	args := []interface{}{}
	first := true

	if displayName != nil {
		query += "display_name = ?"
		args = append(args, *displayName)
		first = false
	}
	if userColor != nil {
		if !first {
			query += ", "
		}
		query += "user_color = ?"
		args = append(args, *userColor)
		first = false
	}
	if customStatus != nil {
		if !first {
			query += ", "
		}
		query += "custom_status = ?"
		args = append(args, *customStatus)
		first = false
	}
	if presence != nil {
		if !first {
			query += ", "
		}
		query += "presence_state = ?"
		args = append(args, *presence)
		first = false
	}

	if first {
		return nil // Nothing to update
	}

	query += " WHERE id = ?"
	args = append(args, id)

	_, err := db.Exec(query, args...)
	return err
}

func (db *DB) UpdateUserPresence(id string, presence string, lastSeen time.Time) error {
	_, err := db.Exec(`
		UPDATE users SET presence_state = ?, last_seen_at = ? WHERE id = ?
	`, presence, lastSeen, id)
	return err
}

func (db *DB) ListUsers() ([]models.UserProfile, error) {
	rows, err := db.Query(`
		SELECT id, username, display_name, user_color, custom_status, presence_state, last_seen_at, created_at
		FROM users WHERE id != ? ORDER BY username ASC
	`, SystemUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.UserProfile
	for rows.Next() {
		var u models.UserProfile
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.UserColor, &u.CustomStatus, &u.Presence, &u.LastSeenAt, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

// Session operations

func (db *DB) CreateSession(sess *models.Session) error {
	_, err := db.Exec(`
		INSERT INTO sessions (token, user_id, created_at, expires_at)
		VALUES (?, ?, ?, ?)
	`, sess.Token, sess.UserID, sess.CreatedAt, sess.ExpiresAt)
	return err
}

func (db *DB) GetSession(token string) (*models.Session, error) {
	row := db.QueryRow(`
		SELECT token, user_id, created_at, expires_at
		FROM sessions WHERE token = ? AND expires_at > ?
	`, token, time.Now().UTC())
	var s models.Session
	if err := row.Scan(&s.Token, &s.UserID, &s.CreatedAt, &s.ExpiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

func (db *DB) DeleteSession(token string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// Room operations

func (db *DB) CreateRoom(r *models.Room) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO rooms (id, name, description, is_private, password_hash, owner_id, is_temporary, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.ID, r.Name, r.Description, r.IsPrivate, r.PasswordHash, r.OwnerID, r.IsTemporary, r.ExpiresAt, r.CreatedAt)
	if err != nil {
		return err
	}

	// Owner automatically becomes a member with role 'owner'
	_, err = tx.Exec(`
		INSERT INTO room_members (room_id, user_id, role, joined_at)
		VALUES (?, ?, ?, ?)
	`, r.ID, r.OwnerID, models.RoleOwner, r.CreatedAt)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (db *DB) GetRoomByID(id string) (*models.Room, error) {
	row := db.QueryRow(`
		SELECT r.id, r.name, r.description, r.is_private, r.password_hash, r.owner_id,
		       COALESCE(u.username, '') as owner_username,
		       r.is_temporary, r.expires_at, r.created_at,
		       (SELECT COUNT(*) FROM room_members rm WHERE rm.room_id = r.id) as member_count
		FROM rooms r
		LEFT JOIN users u ON r.owner_id = u.id
		WHERE r.id = ?
	`, id)

	var r models.Room
	if err := row.Scan(&r.ID, &r.Name, &r.Description, &r.IsPrivate, &r.PasswordHash, &r.OwnerID, &r.OwnerUsername, &r.IsTemporary, &r.ExpiresAt, &r.CreatedAt, &r.MemberCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.HasPassword = r.PasswordHash != nil && *r.PasswordHash != ""
	return &r, nil
}

func (db *DB) GetRoomByName(name string) (*models.Room, error) {
	row := db.QueryRow(`
		SELECT r.id, r.name, r.description, r.is_private, r.password_hash, r.owner_id,
		       COALESCE(u.username, '') as owner_username,
		       r.is_temporary, r.expires_at, r.created_at,
		       (SELECT COUNT(*) FROM room_members rm WHERE rm.room_id = r.id) as member_count
		FROM rooms r
		LEFT JOIN users u ON r.owner_id = u.id
		WHERE r.name = ? COLLATE NOCASE
	`, name)

	var r models.Room
	if err := row.Scan(&r.ID, &r.Name, &r.Description, &r.IsPrivate, &r.PasswordHash, &r.OwnerID, &r.OwnerUsername, &r.IsTemporary, &r.ExpiresAt, &r.CreatedAt, &r.MemberCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.HasPassword = r.PasswordHash != nil && *r.PasswordHash != ""
	return &r, nil
}

func (db *DB) ListRooms(userID string, includePrivateJoined bool) ([]models.Room, error) {
	var query string
	var args []interface{}

	if includePrivateJoined {
		// Include public rooms AND private rooms that the user is a member of
		query = `
			SELECT r.id, r.name, r.description, r.is_private, r.password_hash, r.owner_id,
			       COALESCE(u.username, '') as owner_username,
			       r.is_temporary, r.expires_at, r.created_at,
			       (SELECT COUNT(*) FROM room_members rm WHERE rm.room_id = r.id) as member_count,
			       COALESCE(cur_rm.role, '') as user_role
			FROM rooms r
			LEFT JOIN users u ON r.owner_id = u.id
			LEFT JOIN room_members cur_rm ON cur_rm.room_id = r.id AND cur_rm.user_id = ?
			WHERE r.is_private = 0 OR cur_rm.user_id IS NOT NULL
			ORDER BY r.name ASC
		`
		args = append(args, userID)
	} else {
		// Public rooms only (for public room browser)
		query = `
			SELECT r.id, r.name, r.description, r.is_private, r.password_hash, r.owner_id,
			       COALESCE(u.username, '') as owner_username,
			       r.is_temporary, r.expires_at, r.created_at,
			       (SELECT COUNT(*) FROM room_members rm WHERE rm.room_id = r.id) as member_count,
			       COALESCE(cur_rm.role, '') as user_role
			FROM rooms r
			LEFT JOIN users u ON r.owner_id = u.id
			LEFT JOIN room_members cur_rm ON cur_rm.room_id = r.id AND cur_rm.user_id = ?
			WHERE r.is_private = 0
			ORDER BY r.name ASC
		`
		args = append(args, userID)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rooms []models.Room
	for rows.Next() {
		var r models.Room
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.IsPrivate, &r.PasswordHash, &r.OwnerID, &r.OwnerUsername, &r.IsTemporary, &r.ExpiresAt, &r.CreatedAt, &r.MemberCount, &r.UserRole); err != nil {
			return nil, err
		}
		r.HasPassword = r.PasswordHash != nil && *r.PasswordHash != ""
		rooms = append(rooms, r)
	}
	return rooms, nil
}

func (db *DB) DeleteRoom(roomID string) error {
	_, err := db.Exec(`DELETE FROM rooms WHERE id = ?`, roomID)
	return err
}

func (db *DB) UpdateRoomOwner(roomID, newOwnerID string, oldOwnerRole string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var oldOwnerID string
	err = tx.QueryRow(`SELECT owner_id FROM rooms WHERE id = ?`, roomID).Scan(&oldOwnerID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`UPDATE rooms SET owner_id = ? WHERE id = ?`, newOwnerID, roomID)
	if err != nil {
		return err
	}

	// Set new owner role in room_members
	_, err = tx.Exec(`
		INSERT INTO room_members (room_id, user_id, role, joined_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(room_id, user_id) DO UPDATE SET role = ?
	`, roomID, newOwnerID, models.RoleOwner, time.Now().UTC(), models.RoleOwner)
	if err != nil {
		return err
	}

	// Downgrade old owner to specified role (e.g. Admin)
	_, err = tx.Exec(`
		UPDATE room_members SET role = ? WHERE room_id = ? AND user_id = ?
	`, oldOwnerRole, roomID, oldOwnerID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// Room Membership & Moderation

func (db *DB) AddRoomMember(roomID, userID, role string) error {
	_, err := db.Exec(`
		INSERT INTO room_members (room_id, user_id, role, joined_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(room_id, user_id) DO UPDATE SET role = ?
	`, roomID, userID, role, time.Now().UTC(), role)
	return err
}

func (db *DB) RemoveRoomMember(roomID, userID string) error {
	_, err := db.Exec(`DELETE FROM room_members WHERE room_id = ? AND user_id = ?`, roomID, userID)
	return err
}

func (db *DB) GetRoomMember(roomID, userID string) (*models.RoomMember, error) {
	row := db.QueryRow(`
		SELECT rm.room_id, rm.user_id, rm.role, rm.joined_at, u.username, u.display_name, u.user_color, u.presence_state
		FROM room_members rm
		JOIN users u ON rm.user_id = u.id
		WHERE rm.room_id = ? AND rm.user_id = ?
	`, roomID, userID)

	var m models.RoomMember
	if err := row.Scan(&m.RoomID, &m.UserID, &m.Role, &m.JoinedAt, &m.Username, &m.DisplayName, &m.UserColor, &m.Presence); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func (db *DB) GetRoomMembers(roomID string) ([]models.RoomMember, error) {
	rows, err := db.Query(`
		SELECT rm.room_id, rm.user_id, rm.role, rm.joined_at, u.username, u.display_name, u.user_color, u.presence_state
		FROM room_members rm
		JOIN users u ON rm.user_id = u.id
		WHERE rm.room_id = ?
		ORDER BY 
			CASE rm.role
				WHEN 'owner' THEN 1
				WHEN 'admin' THEN 2
				WHEN 'moderator' THEN 3
				ELSE 4
			END,
			u.username ASC
	`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []models.RoomMember
	for rows.Next() {
		var m models.RoomMember
		if err := rows.Scan(&m.RoomID, &m.UserID, &m.Role, &m.JoinedAt, &m.Username, &m.DisplayName, &m.UserColor, &m.Presence); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, nil
}

func (db *DB) SetMemberRole(roomID, userID, role string) error {
	_, err := db.Exec(`UPDATE room_members SET role = ? WHERE room_id = ? AND user_id = ?`, role, roomID, userID)
	return err
}

func (db *DB) BanUser(b *models.RoomBan) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Remove from room_members if present
	_, err = tx.Exec(`DELETE FROM room_members WHERE room_id = ? AND user_id = ?`, b.RoomID, b.UserID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO room_bans (room_id, user_id, banned_by, reason, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(room_id, user_id) DO UPDATE SET banned_by = ?, reason = ?, created_at = ?
	`, b.RoomID, b.UserID, b.BannedBy, b.Reason, b.CreatedAt, b.BannedBy, b.Reason, b.CreatedAt)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (db *DB) UnbanUser(roomID, userID string) error {
	_, err := db.Exec(`DELETE FROM room_bans WHERE room_id = ? AND user_id = ?`, roomID, userID)
	return err
}

func (db *DB) IsUserBanned(roomID, userID string) (bool, error) {
	var exists int
	err := db.QueryRow(`SELECT 1 FROM room_bans WHERE room_id = ? AND user_id = ?`, roomID, userID).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (db *DB) MuteUser(m *models.RoomMute) error {
	_, err := db.Exec(`
		INSERT INTO room_mutes (room_id, user_id, muted_by, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(room_id, user_id) DO UPDATE SET muted_by = ?, expires_at = ?, created_at = ?
	`, m.RoomID, m.UserID, m.MutedBy, m.ExpiresAt, m.CreatedAt, m.MutedBy, m.ExpiresAt, m.CreatedAt)
	return err
}

func (db *DB) UnmuteUser(roomID, userID string) error {
	_, err := db.Exec(`DELETE FROM room_mutes WHERE room_id = ? AND user_id = ?`, roomID, userID)
	return err
}

func (db *DB) IsUserMuted(roomID, userID string) (bool, error) {
	var expiresAt time.Time
	err := db.QueryRow(`
		SELECT expires_at FROM room_mutes WHERE room_id = ? AND user_id = ?
	`, roomID, userID).Scan(&expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	if time.Now().UTC().After(expiresAt) {
		// Mute expired, clean it up
		_ = db.UnmuteUser(roomID, userID)
		return false, nil
	}
	return true, nil
}

// Messages & 24h retention

func (db *DB) SaveMessage(m *models.Message) error {
	_, err := db.Exec(`
		INSERT INTO messages (id, sender_id, target_type, room_id, recipient_id, content, created_at, is_system)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, m.ID, m.SenderID, m.TargetType, m.RoomID, m.RecipientID, m.Content, m.CreatedAt, m.IsSystem)
	return err
}

func (db *DB) GetRoomMessages(roomID string, since time.Time, limit int) ([]models.Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := db.Query(`
		SELECT m.id, m.sender_id, m.target_type, m.room_id, m.recipient_id, m.content, m.created_at, m.is_system,
		       u.username, u.display_name, u.user_color
		FROM (
			SELECT id, sender_id, target_type, room_id, recipient_id, content, created_at, is_system
			FROM messages
			WHERE room_id = ? AND created_at >= ?
			ORDER BY created_at DESC
			LIMIT ?
		) m
		JOIN users u ON m.sender_id = u.id
		ORDER BY m.created_at ASC
	`, roomID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var msg models.Message
		if err := rows.Scan(&msg.ID, &msg.SenderID, &msg.TargetType, &msg.RoomID, &msg.RecipientID, &msg.Content, &msg.CreatedAt, &msg.IsSystem, &msg.SenderUsername, &msg.SenderDisplayName, &msg.SenderColor); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func (db *DB) GetDMMessages(userA, userB string, since time.Time, limit int) ([]models.Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := db.Query(`
		SELECT m.id, m.sender_id, m.target_type, m.room_id, m.recipient_id, m.content, m.created_at, m.is_system,
		       u.username, u.display_name, u.user_color
		FROM (
			SELECT id, sender_id, target_type, room_id, recipient_id, content, created_at, is_system
			FROM messages
			WHERE target_type = 'dm' AND created_at >= ?
			  AND ((sender_id = ? AND recipient_id = ?) OR (sender_id = ? AND recipient_id = ?))
			ORDER BY created_at DESC
			LIMIT ?
		) m
		JOIN users u ON m.sender_id = u.id
		ORDER BY m.created_at ASC
	`, since, userA, userB, userB, userA, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var msg models.Message
		if err := rows.Scan(&msg.ID, &msg.SenderID, &msg.TargetType, &msg.RoomID, &msg.RecipientID, &msg.Content, &msg.CreatedAt, &msg.IsSystem, &msg.SenderUsername, &msg.SenderDisplayName, &msg.SenderColor); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

// PruneExpiredMessages removes messages older than 24 hours
func (db *DB) PruneExpiredMessages(cutoff time.Time) (int64, error) {
	res, err := db.Exec(`DELETE FROM messages WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GetExpiredRooms finds temporary rooms whose expires_at is in the past
func (db *DB) GetExpiredRooms(now time.Time) ([]models.Room, error) {
	rows, err := db.Query(`
		SELECT id, name, description, is_private, password_hash, owner_id, is_temporary, expires_at, created_at
		FROM rooms
		WHERE is_temporary = 1 AND expires_at IS NOT NULL AND expires_at <= ?
	`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var expired []models.Room
	for rows.Next() {
		var r models.Room
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.IsPrivate, &r.PasswordHash, &r.OwnerID, &r.IsTemporary, &r.ExpiresAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		expired = append(expired, r)
	}
	return expired, nil
}

// Read states & Unread counts

func (db *DB) UpdateReadState(userID, targetType, targetID, lastMsgID string) error {
	_, err := db.Exec(`
		INSERT INTO read_states (user_id, target_type, target_id, last_read_message_id, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, target_type, target_id) DO UPDATE SET
			last_read_message_id = ?,
			updated_at = ?
	`, userID, targetType, targetID, lastMsgID, time.Now().UTC(), lastMsgID, time.Now().UTC())
	return err
}

func (db *DB) GetUnreadStates(userID string, since time.Time) ([]models.ReadState, error) {
	// For rooms: count messages created after the last_read message's created_at (or all in 24h if none read)
	// For DMs: count incoming messages from that sender created after last_read
	query := `
		SELECT 
			'room' as target_type,
			r.id as target_id,
			COALESCE(rs.last_read_message_id, '') as last_read_message_id,
			COALESCE(rs.updated_at, '1970-01-01') as updated_at,
			(
				SELECT COUNT(*) FROM messages m
				WHERE m.room_id = r.id AND m.created_at >= ?
				  AND m.created_at > COALESCE((SELECT created_at FROM messages WHERE id = rs.last_read_message_id), '1970-01-01')
				  AND m.sender_id != ?
			) as unread_count
		FROM rooms r
		LEFT JOIN room_members rm ON rm.room_id = r.id AND rm.user_id = ?
		LEFT JOIN read_states rs ON rs.user_id = ? AND rs.target_type = 'room' AND rs.target_id = r.id
		WHERE r.id = 'room_global' OR rm.user_id IS NOT NULL

		UNION ALL

		SELECT
			'dm' as target_type,
			u.id as target_id,
			COALESCE(rs.last_read_message_id, '') as last_read_message_id,
			COALESCE(rs.updated_at, '1970-01-01') as updated_at,
			(
				SELECT COUNT(*) FROM messages m
				WHERE m.target_type = 'dm' AND m.sender_id = u.id AND m.recipient_id = ? AND m.created_at >= ?
				  AND m.created_at > COALESCE((SELECT created_at FROM messages WHERE id = rs.last_read_message_id), '1970-01-01')
			) as unread_count
		FROM users u
		LEFT JOIN read_states rs ON rs.user_id = ? AND rs.target_type = 'dm' AND rs.target_id = u.id
		WHERE u.id != ? AND u.id != 'user_system'
		  AND EXISTS (
			  SELECT 1 FROM messages m
			  WHERE m.target_type = 'dm' AND m.created_at >= ?
			    AND ((m.sender_id = ? AND m.recipient_id = u.id) OR (m.sender_id = u.id AND m.recipient_id = ?))
		  )
	`

	rows, err := db.Query(query, since, userID, userID, userID, userID, since, userID, userID, since, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []models.ReadState
	for rows.Next() {
		var s models.ReadState
		s.UserID = userID
		if err := rows.Scan(&s.TargetType, &s.TargetID, &s.LastReadMessageID, &s.UpdatedAt, &s.UnreadCount); err != nil {
			return nil, err
		}
		states = append(states, s)
	}
	return states, nil
}

// Notifications

func (db *DB) CreateNotification(n *models.Notification) error {
	_, err := db.Exec(`
		INSERT INTO notifications (id, user_id, type, title, body, is_read, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, n.ID, n.UserID, n.Type, n.Title, n.Body, n.IsRead, n.CreatedAt)
	return err
}

func (db *DB) GetUnreadNotifications(userID string) ([]models.Notification, error) {
	rows, err := db.Query(`
		SELECT id, user_id, type, title, body, is_read, created_at
		FROM notifications
		WHERE user_id = ? AND is_read = 0
		ORDER BY created_at DESC
		LIMIT 50
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notifs []models.Notification
	for rows.Next() {
		var n models.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Type, &n.Title, &n.Body, &n.IsRead, &n.CreatedAt); err != nil {
			return nil, err
		}
		notifs = append(notifs, n)
	}
	return notifs, nil
}

func (db *DB) MarkNotificationRead(id string) error {
	_, err := db.Exec(`UPDATE notifications SET is_read = 1 WHERE id = ?`, id)
	return err
}
