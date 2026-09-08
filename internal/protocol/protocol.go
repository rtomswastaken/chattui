package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/rtoms/chattui/internal/models"
)

const (
	MaxFrameSize = 1024 * 1024 // 1 MB limit
)

// Request types
const (
	CmdRegister          = "register"
	CmdLogin             = "login"
	CmdSendMsg           = "send_msg"
	CmdJoinRoom          = "join_room"
	CmdLeaveRoom         = "leave_room"
	CmdCreateRoom        = "create_room"
	CmdListRooms         = "list_rooms"
	CmdGetHistory        = "get_history"
	CmdSetPresence       = "set_presence"
	CmdUpdateProfile     = "update_profile"
	CmdGetProfile        = "get_profile"
	CmdListUsers         = "list_users"
	CmdKickMember        = "kick_member"
	CmdBanMember         = "ban_member"
	CmdMuteMember        = "mute_member"
	CmdTransferOwnership = "transfer_ownership"
	CmdMarkRead          = "mark_read"
	CmdPing              = "ping"
)

// Event types
const (
	EventMessage      = "msg_event"
	EventPresence     = "presence_event"
	EventRoomUpdate   = "room_event"
	EventNotification = "notification_event"
	EventMember       = "member_event"
	EventRoomExpired  = "room_expired"
	EventPong         = "pong"
)

// WireMessage represents any frame sent over the wire
type WireMessage struct {
	ID      string          `json:"id,omitempty"`      // Request ID
	Type    string          `json:"type,omitempty"`    // Command type (request)
	Event   string          `json:"event,omitempty"`   // Event type (server push)
	Success bool            `json:"success,omitempty"` // For response
	Error   string          `json:"error,omitempty"`   // Error message if Success is false
	Data    json.RawMessage `json:"data,omitempty"`    // Payload
}

// Framing functions
func ReadFrame(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}
	if length > MaxFrameSize {
		return nil, fmt.Errorf("frame size %d exceeds max %d", length, MaxFrameSize)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > MaxFrameSize {
		return fmt.Errorf("payload size %d exceeds max %d", len(payload), MaxFrameSize)
	}
	length := uint32(len(payload))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// Encode WireMessage helper
func EncodeMessage(msg *WireMessage) ([]byte, error) {
	return json.Marshal(msg)
}

// Decode WireMessage helper
func DecodeMessage(data []byte) (*WireMessage, error) {
	var msg WireMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// Specific Request payloads

type RegisterReq struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	UserColor   string `json:"user_color"`
	Status      string `json:"status,omitempty"`
}

type LoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type SendMsgReq struct {
	TargetType  string  `json:"target_type"` // "room" or "dm"
	RoomID      *string `json:"room_id,omitempty"`
	RecipientID *string `json:"recipient_id,omitempty"`
	Content     string  `json:"content"`
}

type JoinRoomReq struct {
	RoomName string  `json:"room_name"`
	Password *string `json:"password,omitempty"`
}

type LeaveRoomReq struct {
	RoomID string `json:"room_id"`
}

type CreateRoomReq struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	IsPrivate   bool    `json:"is_private"`
	Password    *string `json:"password,omitempty"`
	Duration    *int    `json:"duration_hours,omitempty"` // Hours for temporary rooms
}

type ListRoomsReq struct {
	IncludePrivateJoined bool `json:"include_private_joined"`
}

type GetHistoryReq struct {
	TargetType string  `json:"target_type"` // "room" or "dm"
	RoomID     *string `json:"room_id,omitempty"`
	OtherID    *string `json:"other_id,omitempty"`
	Limit      int     `json:"limit,omitempty"`
}

type SetPresenceReq struct {
	Presence     string  `json:"presence"`
	CustomStatus *string `json:"custom_status,omitempty"`
}

type UpdateProfileReq struct {
	DisplayName  *string `json:"display_name,omitempty"`
	UserColor    *string `json:"user_color,omitempty"`
	CustomStatus *string `json:"custom_status,omitempty"`
	Presence     *string `json:"presence,omitempty"`
}

type GetProfileReq struct {
	Username string `json:"username,omitempty"`
	UserID   string `json:"user_id,omitempty"`
}

type MarkReadReq struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	MessageID  string `json:"message_id"`
}

type KickMemberReq struct {
	RoomID string `json:"room_id"`
	UserID string `json:"user_id"`
}

type BanMemberReq struct {
	RoomID string `json:"room_id"`
	UserID string `json:"user_id"`
	Reason string `json:"reason,omitempty"`
}

type MuteMemberReq struct {
	RoomID          string `json:"room_id"`
	UserID          string `json:"user_id"`
	DurationMinutes int    `json:"duration_minutes"`
}

type TransferOwnershipReq struct {
	RoomID    string `json:"room_id"`
	NewOwnerID string `json:"new_owner_id"`
}

// Responses

type AuthResp struct {
	User     models.User       `json:"user"`
	Token    string            `json:"token"`
	Channels []models.Room     `json:"channels"`
	Unreads  []models.ReadState `json:"unreads"`
}

type HistoryResp struct {
	TargetType string           `json:"target_type"`
	TargetID   string           `json:"target_id"`
	Messages   []models.Message `json:"messages"`
}

type RoomListResp struct {
	Rooms []models.Room `json:"rooms"`
}

type UserListResp struct {
	Users []models.UserProfile `json:"users"`
}

// Push Events

type PresenceEvent struct {
	UserID       string    `json:"user_id"`
	Username     string    `json:"username"`
	Presence     string    `json:"presence"`
	CustomStatus string    `json:"custom_status"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

type MemberEvent struct {
	RoomID      string `json:"room_id"`
	RoomName    string `json:"room_name"`
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	Action      string `json:"action"` // "joined", "left", "kicked", "banned", "muted", "role_change"
	Role        string `json:"role,omitempty"`
	Message     string `json:"message,omitempty"`
}

func ValidateUsername(u string) error {
	if len(u) < 3 || len(u) > 20 {
		return errors.New("username must be between 3 and 20 characters")
	}
	for _, ch := range u {
		if !(ch >= 'a' && ch <= 'z') && !(ch >= 'A' && ch <= 'Z') && !(ch >= '0' && ch <= '9') && ch != '_' && ch != '-' {
			return errors.New("username can only contain alphanumeric characters, underscores, and hyphens")
		}
	}
	return nil
}

func ValidateRoomName(name string) error {
	if len(name) < 2 || len(name) > 30 {
		return errors.New("room name must be between 2 and 30 characters")
	}
	for _, ch := range name {
		if !(ch >= 'a' && ch <= 'z') && !(ch >= 'A' && ch <= 'Z') && !(ch >= '0' && ch <= '9') && ch != '_' && ch != '-' {
			return errors.New("room name can only contain alphanumeric characters, underscores, and hyphens")
		}
	}
	return nil
}
