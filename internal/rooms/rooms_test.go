package rooms

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rtoms/chattui/internal/database"
	"github.com/rtoms/chattui/internal/models"
	"github.com/rtoms/chattui/internal/protocol"
)

func setupTestDB(t *testing.T) (*database.DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "chattui-rooms-test-*")
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

func TestRoomManagerPermissions(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	mgr := NewRoomManager(db, nil)

	owner := createUser(t, db, "owner")
	admin := createUser(t, db, "admin")
	mod := createUser(t, db, "moderator")
	member := createUser(t, db, "member")

	// Create room
	room, err := mgr.CreateRoom(owner.ID, protocol.CreateRoomReq{
		Name:        "tech",
		Description: "Tech discussions",
	})
	if err != nil {
		t.Fatalf("failed to create room: %v", err)
	}

	// Join other users
	if _, err := mgr.JoinRoom(admin.ID, "tech", nil); err != nil {
		t.Fatalf("admin join failed: %v", err)
	}
	if _, err := mgr.JoinRoom(mod.ID, "tech", nil); err != nil {
		t.Fatalf("mod join failed: %v", err)
	}
	if _, err := mgr.JoinRoom(member.ID, "tech", nil); err != nil {
		t.Fatalf("member join failed: %v", err)
	}

	// Assign roles
	if err := mgr.SetRole(owner.ID, room.ID, admin.ID, models.RoleAdmin); err != nil {
		t.Fatalf("set admin failed: %v", err)
	}
	if err := mgr.SetRole(admin.ID, room.ID, mod.ID, models.RoleModerator); err != nil {
		t.Fatalf("admin set mod failed: %v", err)
	}

	// Member cannot set role
	if err := mgr.SetRole(member.ID, room.ID, mod.ID, models.RoleAdmin); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}

	// Moderator can kick member
	if err := mgr.KickMember(mod.ID, room.ID, member.ID); err != nil {
		t.Fatalf("mod kick member failed: %v", err)
	}

	// Moderator CANNOT kick Admin
	if err := mgr.KickMember(mod.ID, room.ID, admin.ID); err != ErrCannotTargetHigher {
		t.Fatalf("expected ErrCannotTargetHigher, got %v", err)
	}

	// Admin can ban member
	if err := mgr.BanMember(admin.ID, room.ID, member.ID, "Trolling"); err != nil {
		t.Fatalf("admin ban failed: %v", err)
	}

	// Member cannot rejoin when banned
	if _, err := mgr.JoinRoom(member.ID, "tech", nil); err != ErrBanned {
		t.Fatalf("expected ErrBanned, got %v", err)
	}

	// Moderator can mute
	if err := mgr.MuteMember(mod.ID, room.ID, admin.ID, 10); err != ErrCannotTargetHigher {
		t.Fatalf("expected ErrCannotTargetHigher for mod muting admin, got %v", err)
	}

	// Transfer ownership
	if err := mgr.TransferOwnership(owner.ID, room.ID, admin.ID); err != nil {
		t.Fatalf("transfer ownership failed: %v", err)
	}

	// Check new owner role and old owner role
	adminRM, _ := db.GetRoomMember(room.ID, admin.ID)
	if adminRM.Role != models.RoleOwner {
		t.Fatalf("expected admin to become owner, got %s", adminRM.Role)
	}
	oldOwnerRM, _ := db.GetRoomMember(room.ID, owner.ID)
	if oldOwnerRM.Role != models.RoleAdmin {
		t.Fatalf("expected old owner to become admin, got %s", oldOwnerRM.Role)
	}
}

func TestRoomPassword(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	mgr := NewRoomManager(db, nil)

	owner := createUser(t, db, "owner")
	user := createUser(t, db, "joiner")

	pass := "secret123"
	room, err := mgr.CreateRoom(owner.ID, protocol.CreateRoomReq{
		Name:     "vip",
		Password: &pass,
	})
	if err != nil {
		t.Fatalf("failed to create password room: %v", err)
	}
	if !room.HasPassword {
		t.Fatal("expected HasPassword to be true")
	}

	// Try join with no password
	if _, err := mgr.JoinRoom(user.ID, "vip", nil); err != ErrInvalidPassword {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}

	// Try join with wrong password
	wrongPass := "wrong"
	if _, err := mgr.JoinRoom(user.ID, "vip", &wrongPass); err != ErrInvalidPassword {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}

	// Join with correct password
	if _, err := mgr.JoinRoom(user.ID, "vip", &pass); err != nil {
		t.Fatalf("failed to join with correct password: %v", err)
	}
}

func TestTemporaryRoomExpiration(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	var expiredName string
	mgr := NewRoomManager(db, func(r models.Room) {
		expiredName = r.Name
	})

	owner := createUser(t, db, "owner")

	dur := 1 // 1 hour
	room, err := mgr.CreateRoom(owner.ID, protocol.CreateRoomReq{
		Name:     "temp1",
		Duration: &dur,
	})
	if err != nil {
		t.Fatalf("failed to create temporary room: %v", err)
	}
	if !room.IsTemporary || room.ExpiresAt == nil {
		t.Fatal("expected temporary room with expiration")
	}

	// Check expiration before time
	mgr.checkExpiredRooms(time.Now().UTC())
	if expiredName != "" {
		t.Fatal("room should not be expired yet")
	}

	// Check expiration after 2 hours
	mgr.checkExpiredRooms(time.Now().UTC().Add(2 * time.Hour))
	if expiredName != "temp1" {
		t.Fatalf("expected room temp1 to expire, got '%s'", expiredName)
	}

	// Verify room is deleted
	deletedRoom, _ := db.GetRoomByName("temp1")
	if deletedRoom != nil {
		t.Fatal("expected room to be deleted after expiration")
	}
}
