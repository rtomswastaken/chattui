package rooms

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rtoms/chattui/internal/auth"
	"github.com/rtoms/chattui/internal/database"
	"github.com/rtoms/chattui/internal/models"
	"github.com/rtoms/chattui/internal/protocol"
)

var (
	ErrRoomNotFound       = errors.New("room not found")
	ErrRoomExists         = errors.New("a room with this name already exists")
	ErrInvalidPassword   = errors.New("invalid room password")
	ErrBanned             = errors.New("you are banned from this room")
	ErrMuted              = errors.New("you are muted in this room")
	ErrUnauthorized       = errors.New("insufficient permissions for this action")
	ErrCannotModerateSelf = errors.New("cannot perform moderation action on yourself")
	ErrCannotTargetHigher = errors.New("cannot moderate user with equal or higher role")
	ErrCannotLeaveOwned   = errors.New("room owner must transfer ownership before leaving")
)

type RoomManager struct {
	db              *database.DB
	mu              sync.RWMutex
	onRoomExpired   func(room models.Room)
}

func NewRoomManager(db *database.DB, onRoomExpired func(room models.Room)) *RoomManager {
	return &RoomManager{
		db:            db,
		onRoomExpired: onRoomExpired,
	}
}

// StartExpirationWorker runs a background ticker checking for expired temporary rooms
func (m *RoomManager) StartExpirationWorker(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				m.checkExpiredRooms(now.UTC())
			}
		}
	}()
}

func (m *RoomManager) checkExpiredRooms(now time.Time) {
	expired, err := m.db.GetExpiredRooms(now)
	if err != nil {
		return
	}

	for _, r := range expired {
		if m.onRoomExpired != nil {
			m.onRoomExpired(r)
		}
		_ = m.db.DeleteRoom(r.ID)
	}
}

func (m *RoomManager) CreateRoom(creatorID string, req protocol.CreateRoomReq) (*models.Room, error) {
	name := strings.ToLower(strings.TrimSpace(req.Name))
	if err := protocol.ValidateRoomName(name); err != nil {
		return nil, err
	}

	// Check if already exists
	existing, err := m.db.GetRoomByName(name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrRoomExists
	}

	now := time.Now().UTC()
	var passwordHash *string
	if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
		h, err := auth.HashPassword(strings.TrimSpace(*req.Password))
		if err != nil {
			return nil, fmt.Errorf("failed to hash room password: %w", err)
		}
		passwordHash = &h
	}

	var isTemp bool
	var expiresAt *time.Time
	if req.Duration != nil && *req.Duration > 0 {
		isTemp = true
		exp := now.Add(time.Duration(*req.Duration) * time.Hour)
		expiresAt = &exp
	}

	room := &models.Room{
		ID:           uuid.NewString(),
		Name:         name,
		Description:  strings.TrimSpace(req.Description),
		IsPrivate:    req.IsPrivate,
		PasswordHash: passwordHash,
		OwnerID:      creatorID,
		IsTemporary:  isTemp,
		ExpiresAt:    expiresAt,
		CreatedAt:    now,
	}

	if err := m.db.CreateRoom(room); err != nil {
		return nil, err
	}

	room.HasPassword = passwordHash != nil
	room.MemberCount = 1
	room.UserRole = models.RoleOwner

	return room, nil
}

func (m *RoomManager) JoinRoom(userID string, roomName string, password *string) (*models.Room, error) {
	room, err := m.db.GetRoomByName(roomName)
	if err != nil {
		return nil, err
	}
	if room == nil {
		return nil, ErrRoomNotFound
	}

	// Check ban
	banned, err := m.db.IsUserBanned(room.ID, userID)
	if err != nil {
		return nil, err
	}
	if banned {
		return nil, ErrBanned
	}

	// If password-protected, verify password (unless already a member or owner)
	member, err := m.db.GetRoomMember(room.ID, userID)
	if err != nil {
		return nil, err
	}

	if member == nil && room.HasPassword {
		if password == nil || !auth.CheckPassword(*password, *room.PasswordHash) {
			return nil, ErrInvalidPassword
		}
	}

	if member == nil {
		if err := m.db.AddRoomMember(room.ID, userID, models.RoleMember); err != nil {
			return nil, err
		}
		room.UserRole = models.RoleMember
		room.MemberCount++
	} else {
		room.UserRole = member.Role
	}

	return room, nil
}

func (m *RoomManager) LeaveRoom(userID, roomID string) error {
	if roomID == database.GlobalRoomID {
		return errors.New("cannot leave global chat")
	}

	member, err := m.db.GetRoomMember(roomID, userID)
	if err != nil {
		return err
	}
	if member == nil {
		return nil
	}

	if member.Role == models.RoleOwner {
		return ErrCannotLeaveOwned
	}

	return m.db.RemoveRoomMember(roomID, userID)
}

func (m *RoomManager) KickMember(actorID, roomID, targetUserID string) error {
	if actorID == targetUserID {
		return ErrCannotModerateSelf
	}

	actorRole, targetRole, err := m.getActorAndTargetRoles(roomID, actorID, targetUserID)
	if err != nil {
		return err
	}

	// Moderator or higher can kick, but only if actor level > target level
	if models.RoleLevel(actorRole) < models.RoleLevel(models.RoleModerator) {
		return ErrUnauthorized
	}
	if models.RoleLevel(actorRole) <= models.RoleLevel(targetRole) {
		return ErrCannotTargetHigher
	}

	return m.db.RemoveRoomMember(roomID, targetUserID)
}

func (m *RoomManager) BanMember(actorID, roomID, targetUserID, reason string) error {
	if actorID == targetUserID {
		return ErrCannotModerateSelf
	}

	actorRole, targetRole, err := m.getActorAndTargetRoles(roomID, actorID, targetUserID)
	if err != nil {
		return err
	}

	// Admin or higher can ban
	if models.RoleLevel(actorRole) < models.RoleLevel(models.RoleAdmin) {
		return ErrUnauthorized
	}
	if models.RoleLevel(actorRole) <= models.RoleLevel(targetRole) {
		return ErrCannotTargetHigher
	}

	ban := &models.RoomBan{
		RoomID:    roomID,
		UserID:    targetUserID,
		BannedBy:  actorID,
		Reason:    reason,
		CreatedAt: time.Now().UTC(),
	}

	return m.db.BanUser(ban)
}

func (m *RoomManager) MuteMember(actorID, roomID, targetUserID string, durationMinutes int) error {
	if actorID == targetUserID {
		return ErrCannotModerateSelf
	}
	if durationMinutes <= 0 {
		durationMinutes = 15
	}

	actorRole, targetRole, err := m.getActorAndTargetRoles(roomID, actorID, targetUserID)
	if err != nil {
		return err
	}

	// Moderator or higher can mute
	if models.RoleLevel(actorRole) < models.RoleLevel(models.RoleModerator) {
		return ErrUnauthorized
	}
	if models.RoleLevel(actorRole) <= models.RoleLevel(targetRole) {
		return ErrCannotTargetHigher
	}

	now := time.Now().UTC()
	mute := &models.RoomMute{
		RoomID:    roomID,
		UserID:    targetUserID,
		MutedBy:   actorID,
		ExpiresAt: now.Add(time.Duration(durationMinutes) * time.Minute),
		CreatedAt: now,
	}

	return m.db.MuteUser(mute)
}

func (m *RoomManager) SetRole(actorID, roomID, targetUserID, newRole string) error {
	if actorID == targetUserID {
		return ErrCannotModerateSelf
	}

	actorRole, targetRole, err := m.getActorAndTargetRoles(roomID, actorID, targetUserID)
	if err != nil {
		return err
	}

	// Owner can manage admins and moderators
	// Admin can manage moderators
	switch newRole {
	case models.RoleAdmin:
		if actorRole != models.RoleOwner {
			return ErrUnauthorized
		}
	case models.RoleModerator:
		if models.RoleLevel(actorRole) < models.RoleLevel(models.RoleAdmin) {
			return ErrUnauthorized
		}
		if models.RoleLevel(actorRole) <= models.RoleLevel(targetRole) {
			return ErrCannotTargetHigher
		}
	case models.RoleMember:
		if models.RoleLevel(actorRole) <= models.RoleLevel(targetRole) {
			return ErrCannotTargetHigher
		}
	default:
		return errors.New("invalid role")
	}

	return m.db.SetMemberRole(roomID, targetUserID, newRole)
}

func (m *RoomManager) TransferOwnership(actorID, roomID, newOwnerID string) error {
	if actorID == newOwnerID {
		return errors.New("cannot transfer ownership to yourself")
	}

	actorRole, targetRole, err := m.getActorAndTargetRoles(roomID, actorID, newOwnerID)
	if err != nil {
		return err
	}

	if actorRole != models.RoleOwner {
		return ErrUnauthorized
	}
	if targetRole == "" {
		return errors.New("target user is not a member of this room")
	}

	// Demote old owner to Admin
	return m.db.UpdateRoomOwner(roomID, newOwnerID, models.RoleAdmin)
}

func (m *RoomManager) CanSendMessage(userID, roomID string) error {
	// For global room, all authenticated users are allowed unless banned/muted
	if roomID != database.GlobalRoomID {
		member, err := m.db.GetRoomMember(roomID, userID)
		if err != nil {
			return err
		}
		if member == nil {
			return errors.New("must join room to send messages")
		}
	}

	banned, err := m.db.IsUserBanned(roomID, userID)
	if err != nil {
		return err
	}
	if banned {
		return ErrBanned
	}

	muted, err := m.db.IsUserMuted(roomID, userID)
	if err != nil {
		return err
	}
	if muted {
		return ErrMuted
	}

	return nil
}

func (m *RoomManager) getActorAndTargetRoles(roomID, actorID, targetUserID string) (string, string, error) {
	actorMember, err := m.db.GetRoomMember(roomID, actorID)
	if err != nil {
		return "", "", err
	}
	if actorMember == nil {
		return "", "", ErrUnauthorized
	}

	targetMember, err := m.db.GetRoomMember(roomID, targetUserID)
	if err != nil {
		return "", "", err
	}
	targetRole := ""
	if targetMember != nil {
		targetRole = targetMember.Role
	}

	return actorMember.Role, targetRole, nil
}
