package models

import (
	"time"
)

// Presence constants
const (
	PresenceOnline  = "online"
	PresenceIdle    = "idle"
	PresenceAway    = "away"
	PresenceOffline = "offline"
)

// Role constants for room permissions hierarchy
const (
	RoleOwner     = "owner"
	RoleAdmin     = "admin"
	RoleModerator = "moderator"
	RoleMember    = "member"
)

// RoleHierarchy returns numeric ranking: higher number = higher privilege
func RoleLevel(role string) int {
	switch role {
	case RoleOwner:
		return 40
	case RoleAdmin:
		return 30
	case RoleModerator:
		return 20
	case RoleMember:
		return 10
	default:
		return 0
	}
}

// Target types for messages and read states
const (
	TargetRoom = "room"
	TargetDM   = "dm"
)

// Notification types
const (
	NotifyMention    = "mention"
	NotifyDM         = "dm"
	NotifySystem     = "system"
	NotifyModeration = "moderation"
	NotifyInvite     = "invite"
)

// User model
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"display_name"`
	PasswordHash string    `json:"-"`
	UserColor    string    `json:"user_color"`
	CustomStatus string    `json:"custom_status"`
	Presence     string    `json:"presence"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// UserProfile for public view/display
type UserProfile struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"display_name"`
	UserColor    string    `json:"user_color"`
	CustomStatus string    `json:"custom_status"`
	Presence     string    `json:"presence"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// ToProfile converts a User to public UserProfile
func (u *User) ToProfile() UserProfile {
	return UserProfile{
		ID:           u.ID,
		Username:     u.Username,
		DisplayName:  u.DisplayName,
		UserColor:    u.UserColor,
		CustomStatus: u.CustomStatus,
		Presence:     u.Presence,
		LastSeenAt:   u.LastSeenAt,
		CreatedAt:    u.CreatedAt,
	}
}

// Session model
type Session struct {
	Token     string    `json:"token"`
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Room model
type Room struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	IsPrivate     bool       `json:"is_private"`
	HasPassword   bool       `json:"has_password"`
	PasswordHash  *string    `json:"-"`
	OwnerID       string     `json:"owner_id"`
	OwnerUsername string     `json:"owner_username,omitempty"`
	IsTemporary   bool       `json:"is_temporary"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	MemberCount   int        `json:"member_count"`
	UserRole      string     `json:"user_role,omitempty"` // role of querying user
}

// RoomMember model
type RoomMember struct {
	RoomID      string    `json:"room_id"`
	UserID      string    `json:"user_id"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joined_at"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	UserColor   string    `json:"user_color"`
	Presence    string    `json:"presence"`
}

// Message model
type Message struct {
	ID                string    `json:"id"`
	SenderID          string    `json:"sender_id"`
	TargetType        string    `json:"target_type"` // "room" or "dm"
	RoomID            *string   `json:"room_id,omitempty"`
	RecipientID       *string   `json:"recipient_id,omitempty"`
	Content           string    `json:"content"`
	CreatedAt         time.Time `json:"created_at"`
	IsSystem          bool      `json:"is_system"`
	SenderUsername    string    `json:"sender_username"`
	SenderDisplayName string    `json:"sender_display_name"`
	SenderColor       string    `json:"sender_color"`
}

// ReadState model
type ReadState struct {
	UserID            string    `json:"user_id"`
	TargetType        string    `json:"target_type"`
	TargetID          string    `json:"target_id"` // room_id or partner user_id
	LastReadMessageID string    `json:"last_read_message_id"`
	UpdatedAt         time.Time `json:"updated_at"`
	UnreadCount       int       `json:"unread_count"`
}

// Notification model
type Notification struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	IsRead    bool      `json:"is_read"`
	CreatedAt time.Time `json:"created_at"`
}

// RoomBan model
type RoomBan struct {
	RoomID    string    `json:"room_id"`
	UserID    string    `json:"user_id"`
	BannedBy  string    `json:"banned_by"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

// RoomMute model
type RoomMute struct {
	RoomID    string    `json:"room_id"`
	UserID    string    `json:"user_id"`
	MutedBy   string    `json:"muted_by"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}
