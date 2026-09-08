package chat

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rtoms/chattui/internal/database"
	"github.com/rtoms/chattui/internal/models"
	"github.com/rtoms/chattui/internal/protocol"
)

func setupTestDB(t *testing.T) (*database.DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "chattui-chat-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open test db: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return db, cleanup
}

func createUser(t *testing.T, db *database.DB, username string) *models.User {
	t.Helper()
	now := time.Now().UTC()
	u := &models.User{
		ID:           uuid.NewString(),
		Username:     username,
		DisplayName:  username,
		PasswordHash: "hash",
		UserColor:    "Cyan",
		LastSeenAt:   now,
		CreatedAt:    now,
	}
	if err := db.CreateUser(u); err != nil {
		t.Fatalf("failed to create user %s: %v", username, err)
	}
	return u
}

func TestMentionExtraction(t *testing.T) {
	text := "Hello @alex and @rtoms, check this out! Also cc @alex."
	mentions := ExtractMentions(text)
	if len(mentions) != 2 {
		t.Fatalf("expected 2 unique mentions, got %d: %v", len(mentions), mentions)
	}
	if mentions[0] != "alex" || mentions[1] != "rtoms" {
		t.Fatalf("unexpected mentions: %v", mentions)
	}
}

func TestChatEngineMessagingAndMentions(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	var mu sync.Mutex
	var deliveredMsgs []*models.Message
	var deliveredNotifs []*models.Notification

	engine := NewEngine(db,
		func(targetType, targetID string, msg *models.Message) {
			mu.Lock()
			deliveredMsgs = append(deliveredMsgs, msg)
			mu.Unlock()
		},
		func(targetUserID string, notif *models.Notification) {
			mu.Lock()
			deliveredNotifs = append(deliveredNotifs, notif)
			mu.Unlock()
		},
	)

	alice := createUser(t, db, "alice")
	bob := createUser(t, db, "bob")

	room, _ := db.GetRoomByID(database.GlobalRoomID)

	// Alice sends message mentioning Bob
	content := "Hey @bob how are you?"
	msg, err := engine.SendRoomMessage(alice, room, content)
	if err != nil {
		t.Fatalf("failed to send room message: %v", err)
	}
	if msg.Content != content {
		t.Fatalf("message content mismatch: %s", msg.Content)
	}

	// Allow goroutine to process mention
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(deliveredMsgs) == 0 {
		t.Fatal("expected delivered message")
	}
	if len(deliveredNotifs) == 0 {
		t.Fatal("expected delivered mention notification for bob")
	}
	if deliveredNotifs[0].UserID != bob.ID {
		t.Fatalf("notification sent to wrong user: %s != %s", deliveredNotifs[0].UserID, bob.ID)
	}
	mu.Unlock()

	// Send direct message from Bob to Alice
	dmContent := "Hey Alice, I am great!"
	dmMsg, err := engine.SendDM(bob, alice, dmContent)
	if err != nil {
		t.Fatalf("failed to send DM: %v", err)
	}
	if dmMsg.Content != dmContent {
		t.Fatalf("dm content mismatch: %s", dmMsg.Content)
	}

	// Query history
	hist, err := engine.GetHistory(protocol.GetHistoryReq{
		TargetType: models.TargetRoom,
		RoomID:     &room.ID,
	}, alice.ID)
	if err != nil {
		t.Fatalf("get history failed: %v", err)
	}
	if len(hist) < 1 {
		t.Fatalf("expected at least 1 history message, got %d", len(hist))
	}
}
