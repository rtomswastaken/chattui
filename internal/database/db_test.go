package database

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rtoms/chattui/internal/models"
)

func setupTestDB(t *testing.T) (*DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "chattui-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := Open(dbPath)
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

func TestDatabaseInit(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Check global room
	global, err := db.GetRoomByID(GlobalRoomID)
	if err != nil {
		t.Fatalf("failed to get global room: %v", err)
	}
	if global == nil || global.Name != "global" {
		t.Fatalf("expected global room, got %+v", global)
	}

	// Check system user
	sysUser, err := db.GetUserByID(SystemUserID)
	if err != nil {
		t.Fatalf("failed to get system user: %v", err)
	}
	if sysUser == nil || sysUser.Username != "system" {
		t.Fatalf("expected system user, got %+v", sysUser)
	}
}

func TestUserAndSession(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	now := time.Now().UTC()
	user := &models.User{
		ID:           uuid.NewString(),
		Username:     "testuser",
		DisplayName:  "Test User",
		PasswordHash: "hashedsecret",
		UserColor:    "Cyan",
		CustomStatus: "Testing things",
		Presence:     "online",
		LastSeenAt:   now,
		CreatedAt:    now,
	}

	err := db.CreateUser(user)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Duplicate username should fail
	dupUser := &models.User{
		ID:           uuid.NewString(),
		Username:     "testuser",
		DisplayName:  "Dup",
		PasswordHash: "hash",
		UserColor:    "Blue",
		LastSeenAt:   now,
		CreatedAt:    now,
	}
	if err := db.CreateUser(dupUser); err == nil {
		t.Fatal("expected duplicate username error, got nil")
	}

	// Fetch by username
	fetched, err := db.GetUserByUsername("TESTUSER") // Case-insensitive
	if err != nil || fetched == nil {
		t.Fatalf("failed to fetch user case-insensitively: %v", err)
	}
	if fetched.DisplayName != "Test User" {
		t.Fatalf("unexpected display name: %s", fetched.DisplayName)
	}

	// Create session
	sess := &models.Session{
		Token:     "token-12345",
		UserID:    user.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(24 * time.Hour),
	}
	if err := db.CreateSession(sess); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	fetchedSess, err := db.GetSession("token-12345")
	if err != nil || fetchedSess == nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if fetchedSess.UserID != user.ID {
		t.Fatalf("session user id mismatch: got %s, want %s", fetchedSess.UserID, user.ID)
	}
}

func TestRoomsAndPermissions(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	now := time.Now().UTC()
	owner := &models.User{
		ID:           uuid.NewString(),
		Username:     "owner1",
		DisplayName:  "Owner One",
		PasswordHash: "hash",
		UserColor:    "Green",
		LastSeenAt:   now,
		CreatedAt:    now,
	}
	_ = db.CreateUser(owner)

	member := &models.User{
		ID:           uuid.NewString(),
		Username:     "member1",
		DisplayName:  "Member One",
		PasswordHash: "hash",
		UserColor:    "Yellow",
		LastSeenAt:   now,
		CreatedAt:    now,
	}
	_ = db.CreateUser(member)

	// Create room
	room := &models.Room{
		ID:          uuid.NewString(),
		Name:        "dev",
		Description: "Development channel",
		IsPrivate:   false,
		OwnerID:     owner.ID,
		CreatedAt:   now,
	}
	if err := db.CreateRoom(room); err != nil {
		t.Fatalf("failed to create room: %v", err)
	}

	// Check owner membership
	rm, err := db.GetRoomMember(room.ID, owner.ID)
	if err != nil || rm == nil {
		t.Fatalf("failed to get owner membership: %v", err)
	}
	if rm.Role != models.RoleOwner {
		t.Fatalf("expected role owner, got %s", rm.Role)
	}

	// Add member
	if err := db.AddRoomMember(room.ID, member.ID, models.RoleMember); err != nil {
		t.Fatalf("failed to add member: %v", err)
	}

	// Promote member to Admin
	if err := db.SetMemberRole(room.ID, member.ID, models.RoleAdmin); err != nil {
		t.Fatalf("failed to promote member: %v", err)
	}
	updatedRM, _ := db.GetRoomMember(room.ID, member.ID)
	if updatedRM.Role != models.RoleAdmin {
		t.Fatalf("expected role admin, got %s", updatedRM.Role)
	}

	// Transfer ownership
	if err := db.UpdateRoomOwner(room.ID, member.ID, models.RoleAdmin); err != nil {
		t.Fatalf("failed to transfer ownership: %v", err)
	}
	newOwnerRM, _ := db.GetRoomMember(room.ID, member.ID)
	if newOwnerRM.Role != models.RoleOwner {
		t.Fatalf("expected new owner role owner, got %s", newOwnerRM.Role)
	}
	oldOwnerRM, _ := db.GetRoomMember(room.ID, owner.ID)
	if oldOwnerRM.Role != models.RoleAdmin {
		t.Fatalf("expected old owner demoted to admin, got %s", oldOwnerRM.Role)
	}
}

func TestMessageRetentionCleanup(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	now := time.Now().UTC()
	user := &models.User{
		ID:           uuid.NewString(),
		Username:     "msguser",
		DisplayName:  "Msg User",
		PasswordHash: "hash",
		UserColor:    "Cyan",
		LastSeenAt:   now,
		CreatedAt:    now,
	}
	_ = db.CreateUser(user)

	roomID := GlobalRoomID

	// Message 25 hours ago
	oldMsg := &models.Message{
		ID:         uuid.NewString(),
		SenderID:   user.ID,
		TargetType: models.TargetRoom,
		RoomID:     &roomID,
		Content:    "Old message from yesterday",
		CreatedAt:  now.Add(-25 * time.Hour),
	}
	if err := db.SaveMessage(oldMsg); err != nil {
		t.Fatalf("failed to save old msg: %v", err)
	}

	// Message 2 hours ago
	recentMsg := &models.Message{
		ID:         uuid.NewString(),
		SenderID:   user.ID,
		TargetType: models.TargetRoom,
		RoomID:     &roomID,
		Content:    "Recent message from today",
		CreatedAt:  now.Add(-2 * time.Hour),
	}
	if err := db.SaveMessage(recentMsg); err != nil {
		t.Fatalf("failed to save recent msg: %v", err)
	}

	// Query last 24h
	cutoff := now.Add(-24 * time.Hour)
	messages, err := db.GetRoomMessages(roomID, cutoff, 100)
	if err != nil {
		t.Fatalf("failed to query room messages: %v", err)
	}
	if len(messages) != 1 || messages[0].ID != recentMsg.ID {
		t.Fatalf("expected 1 recent message, got %d", len(messages))
	}

	// Prune messages older than 24 hours
	deleted, err := db.PruneExpiredMessages(cutoff)
	if err != nil {
		t.Fatalf("failed to prune messages: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted message, got %d", deleted)
	}
}
