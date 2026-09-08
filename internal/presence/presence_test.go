package presence

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rtoms/chattui/internal/database"
	"github.com/rtoms/chattui/internal/models"
	"github.com/rtoms/chattui/internal/protocol"
)

func TestPresenceTracker(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "chattui-presence-*")
	defer os.RemoveAll(tmpDir)

	db, err := database.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	tracker := NewTracker(db)

	var mu sync.Mutex
	var events []protocol.PresenceEvent

	tracker.Subscribe(func(ev protocol.PresenceEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})

	user := &models.User{
		ID:           "u1",
		Username:     "tester",
		DisplayName:  "Tester",
		UserColor:    "Cyan",
		CustomStatus: "Testing",
		Presence:     models.PresenceOnline,
	}

	// Connect
	tracker.OnUserConnected(user)
	if !tracker.IsOnline("u1") {
		t.Fatal("expected user to be online")
	}

	// Change presence to Idle
	tracker.SetPresence(user, models.PresenceIdle, nil)

	// Disconnect
	tracker.OnUserDisconnected(user)
	if tracker.IsOnline("u1") {
		t.Fatal("expected user to be offline")
	}

	// Allow goroutines to finish
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(events))
	}
}
