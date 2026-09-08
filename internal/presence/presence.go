package presence

import (
	"sync"
	"time"

	"github.com/rtoms/chattui/internal/database"
	"github.com/rtoms/chattui/internal/models"
	"github.com/rtoms/chattui/internal/protocol"
)

type ListenerFunc func(event protocol.PresenceEvent)

type Tracker struct {
	db        *database.DB
	mu        sync.RWMutex
	userConns map[string]int // userID -> connection count
	listeners []ListenerFunc
}

func NewTracker(db *database.DB) *Tracker {
	return &Tracker{
		db:        db,
		userConns: make(map[string]int),
	}
}

func (t *Tracker) Subscribe(fn ListenerFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.listeners = append(t.listeners, fn)
}

func (t *Tracker) OnUserConnected(user *models.User) {
	t.mu.Lock()
	t.userConns[user.ID]++
	now := time.Now().UTC()
	t.mu.Unlock()

	_ = t.db.UpdateUserPresence(user.ID, models.PresenceOnline, now)

	t.broadcast(protocol.PresenceEvent{
		UserID:       user.ID,
		Username:     user.Username,
		Presence:     models.PresenceOnline,
		CustomStatus: user.CustomStatus,
		LastSeenAt:   now,
	})
}

func (t *Tracker) OnUserDisconnected(user *models.User) {
	t.mu.Lock()
	t.userConns[user.ID]--
	remaining := t.userConns[user.ID]
	if remaining <= 0 {
		delete(t.userConns, user.ID)
	}
	now := time.Now().UTC()
	t.mu.Unlock()

	if remaining <= 0 {
		_ = t.db.UpdateUserPresence(user.ID, models.PresenceOffline, now)

		t.broadcast(protocol.PresenceEvent{
			UserID:       user.ID,
			Username:     user.Username,
			Presence:     models.PresenceOffline,
			CustomStatus: user.CustomStatus,
			LastSeenAt:   now,
		})
	}
}

func (t *Tracker) SetPresence(user *models.User, state string, customStatus *string) {
	now := time.Now().UTC()
	_ = t.db.UpdateUserProfile(user.ID, nil, nil, customStatus, &state)

	status := user.CustomStatus
	if customStatus != nil {
		status = *customStatus
	}

	t.broadcast(protocol.PresenceEvent{
		UserID:       user.ID,
		Username:     user.Username,
		Presence:     state,
		CustomStatus: status,
		LastSeenAt:   now,
	})
}

func (t *Tracker) IsOnline(userID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.userConns[userID] > 0
}

func (t *Tracker) broadcast(ev protocol.PresenceEvent) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, fn := range t.listeners {
		go fn(ev)
	}
}
