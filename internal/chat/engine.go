package chat

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rtoms/chattui/internal/database"
	"github.com/rtoms/chattui/internal/models"
	"github.com/rtoms/chattui/internal/protocol"
)

var mentionRegex = regexp.MustCompile(`(?:^|\s)@([a-zA-Z0-9_-]+)`)

type MessageHandler func(targetType string, targetID string, msg *models.Message)
type NotificationHandler func(targetUserID string, notif *models.Notification)

type Engine struct {
	db              *database.DB
	mu              sync.RWMutex
	onMessage       MessageHandler
	onNotification  NotificationHandler
}

func NewEngine(db *database.DB, onMsg MessageHandler, onNotif NotificationHandler) *Engine {
	return &Engine{
		db:             db,
		onMessage:      onMsg,
		onNotification: onNotif,
	}
}

// StartRetentionWorker periodically deletes messages older than 24 hours
func (e *Engine) StartRetentionWorker(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				cutoff := now.UTC().Add(-24 * time.Hour)
				_, _ = e.db.PruneExpiredMessages(cutoff)
			}
		}
	}()
}

// ExtractMentions returns unique usernames mentioned with @
func ExtractMentions(content string) []string {
	matches := mentionRegex.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	var usernames []string
	for _, m := range matches {
		if len(m) > 1 {
			uname := strings.ToLower(m[1])
			if !seen[uname] {
				seen[uname] = true
				usernames = append(usernames, uname)
			}
		}
	}
	return usernames
}

// SendRoomMessage validates, saves, and broadcasts a message to a room
func (e *Engine) SendRoomMessage(sender *models.User, room *models.Room, content string) (*models.Message, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, fmt.Errorf("message cannot be empty")
	}
	if len(trimmed) > 4000 {
		return nil, fmt.Errorf("message exceeds maximum length of 4000 characters")
	}

	now := time.Now().UTC()
	msg := &models.Message{
		ID:                uuid.NewString(),
		SenderID:          sender.ID,
		TargetType:        models.TargetRoom,
		RoomID:            &room.ID,
		Content:           trimmed,
		CreatedAt:         now,
		IsSystem:          false,
		SenderUsername:    sender.Username,
		SenderDisplayName: sender.DisplayName,
		SenderColor:       sender.UserColor,
	}

	if err := e.db.SaveMessage(msg); err != nil {
		return nil, err
	}

	// Deliver to room listeners
	if e.onMessage != nil {
		e.onMessage(models.TargetRoom, room.ID, msg)
	}

	// Process mentions
	go e.processMentions(sender, room.Name, trimmed)

	return msg, nil
}

// SendDM validates, saves, and delivers a 1-on-1 direct message
func (e *Engine) SendDM(sender *models.User, recipient *models.User, content string) (*models.Message, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, fmt.Errorf("message cannot be empty")
	}
	if len(trimmed) > 4000 {
		return nil, fmt.Errorf("message exceeds maximum length of 4000 characters")
	}

	now := time.Now().UTC()
	msg := &models.Message{
		ID:                uuid.NewString(),
		SenderID:          sender.ID,
		TargetType:        models.TargetDM,
		RecipientID:       &recipient.ID,
		Content:           trimmed,
		CreatedAt:         now,
		IsSystem:          false,
		SenderUsername:    sender.Username,
		SenderDisplayName: sender.DisplayName,
		SenderColor:       sender.UserColor,
	}

	if err := e.db.SaveMessage(msg); err != nil {
		return nil, err
	}

	// Deliver to DM listeners (both sender and recipient)
	if e.onMessage != nil {
		e.onMessage(models.TargetDM, recipient.ID, msg)
		e.onMessage(models.TargetDM, sender.ID, msg)
	}

	// Generate notification for recipient
	notif := &models.Notification{
		ID:        uuid.NewString(),
		UserID:    recipient.ID,
		Type:      models.NotifyDM,
		Title:     fmt.Sprintf("New message from @%s", sender.Username),
		Body:      trimmed,
		IsRead:    false,
		CreatedAt: now,
	}
	_ = e.db.CreateNotification(notif)
	if e.onNotification != nil {
		e.onNotification(recipient.ID, notif)
	}

	return msg, nil
}

// BroadcastSystemMessage sends a system notification into a room
func (e *Engine) BroadcastSystemMessage(roomID string, content string) (*models.Message, error) {
	now := time.Now().UTC()
	msg := &models.Message{
		ID:                uuid.NewString(),
		SenderID:          database.SystemUserID,
		TargetType:        models.TargetRoom,
		RoomID:            &roomID,
		Content:           content,
		CreatedAt:         now,
		IsSystem:          true,
		SenderUsername:    database.SystemUsername,
		SenderDisplayName: database.SystemDisplayName,
		SenderColor:       "Magenta",
	}

	if err := e.db.SaveMessage(msg); err != nil {
		return nil, err
	}

	if e.onMessage != nil {
		e.onMessage(models.TargetRoom, roomID, msg)
	}

	return msg, nil
}

// BroadcastNewUserWelcome sends a global message when a new account is created
func (e *Engine) BroadcastNewUserWelcome(username string) {
	msgContent := fmt.Sprintf("● %s just joined chatTUI", username)
	_, _ = e.BroadcastSystemMessage(database.GlobalRoomID, msgContent)
}

func (e *Engine) processMentions(sender *models.User, roomName, content string) {
	mentions := ExtractMentions(content)
	now := time.Now().UTC()

	for _, username := range mentions {
		if strings.EqualFold(username, sender.Username) {
			continue // Skip self-mentions
		}

		targetUser, err := e.db.GetUserByUsername(username)
		if err != nil || targetUser == nil {
			continue
		}

		notif := &models.Notification{
			ID:        uuid.NewString(),
			UserID:    targetUser.ID,
			Type:      models.NotifyMention,
			Title:     fmt.Sprintf("@%s mentioned you in #%s", sender.Username, roomName),
			Body:      content,
			IsRead:    false,
			CreatedAt: now,
		}

		_ = e.db.CreateNotification(notif)
		if e.onNotification != nil {
			e.onNotification(targetUser.ID, notif)
		}
	}
}

// GetHistory retrieves up to 24-hour message history
func (e *Engine) GetHistory(req protocol.GetHistoryReq, currentUserID string) ([]models.Message, error) {
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	limit := req.Limit
	if limit <= 0 || limit > 300 {
		limit = 100
	}

	if req.TargetType == models.TargetRoom && req.RoomID != nil {
		return e.db.GetRoomMessages(*req.RoomID, cutoff, limit)
	}

	if req.TargetType == models.TargetDM && req.OtherID != nil {
		return e.db.GetDMMessages(currentUserID, *req.OtherID, cutoff, limit)
	}

	return nil, fmt.Errorf("invalid history target")
}
