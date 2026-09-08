package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rtoms/chattui/internal/database"
	"github.com/rtoms/chattui/internal/protocol"
)

func setupTestDB(t *testing.T) (*database.DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "chattui-auth-test-*")
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

func TestAuthService(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	svc := NewAuthService(db)

	// Register valid user
	regReq := protocol.RegisterReq{
		Username:    "rtoms",
		DisplayName: "Rtoms",
		Password:    "supersecret123",
		UserColor:   "Cyan",
		Status:      "Building chatTUI",
	}

	user, sess, err := svc.Register(regReq)
	if err != nil {
		t.Fatalf("failed to register: %v", err)
	}
	if user.Username != "rtoms" || sess.Token == "" {
		t.Fatalf("unexpected register result: user=%+v, sess=%+v", user, sess)
	}

	// Test duplicate username
	_, _, err = svc.Register(regReq)
	if err != ErrUserExists {
		t.Fatalf("expected ErrUserExists, got %v", err)
	}

	// Test login with correct password
	loginUser, loginSess, err := svc.Login(protocol.LoginReq{
		Username: "rtoms",
		Password: "supersecret123",
	})
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if loginUser.ID != user.ID || loginSess.Token == "" {
		t.Fatalf("unexpected login result: user=%+v, sess=%+v", loginUser, loginSess)
	}

	// Test login with wrong password
	_, _, err = svc.Login(protocol.LoginReq{
		Username: "rtoms",
		Password: "wrongpassword",
	})
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}

	// Test authenticate session
	authUser, err := svc.AuthenticateSession(loginSess.Token)
	if err != nil || authUser == nil || authUser.ID != user.ID {
		t.Fatalf("failed to authenticate session: %v", err)
	}
}
