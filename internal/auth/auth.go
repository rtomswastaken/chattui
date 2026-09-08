package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rtoms/chattui/internal/database"
	"github.com/rtoms/chattui/internal/models"
	"github.com/rtoms/chattui/internal/protocol"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserExists       = errors.New("username is already taken")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrSessionExpired   = errors.New("session expired or invalid")
	ErrWeakPassword     = errors.New("password must be at least 6 characters")
)

// AllowedColors represents terminal-safe colors
var AllowedColors = []string{
	"Cyan",
	"Blue",
	"Green",
	"Yellow",
	"Magenta",
	"Red",
	"White",
}

func IsValidColor(c string) bool {
	for _, a := range AllowedColors {
		if strings.EqualFold(a, c) {
			return true
		}
	}
	return false
}

func NormalizeColor(c string) string {
	for _, a := range AllowedColors {
		if strings.EqualFold(a, c) {
			return a
		}
	}
	return "Cyan"
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func GenerateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type AuthService struct {
	db *database.DB
}

func NewAuthService(db *database.DB) *AuthService {
	return &AuthService{db: db}
}

func (s *AuthService) Register(req protocol.RegisterReq) (*models.User, *models.Session, error) {
	username := strings.TrimSpace(req.Username)
	if err := protocol.ValidateUsername(username); err != nil {
		return nil, nil, err
	}

	if len(req.Password) < 6 {
		return nil, nil, ErrWeakPassword
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = username
	}

	// Check existing user
	existing, err := s.db.GetUserByUsername(username)
	if err != nil {
		return nil, nil, err
	}
	if existing != nil {
		return nil, nil, ErrUserExists
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	user := &models.User{
		ID:           uuid.NewString(),
		Username:     username,
		DisplayName:  displayName,
		PasswordHash: hash,
		UserColor:    NormalizeColor(req.UserColor),
		CustomStatus: strings.TrimSpace(req.Status),
		Presence:     models.PresenceOnline,
		LastSeenAt:   now,
		CreatedAt:    now,
	}

	if err := s.db.CreateUser(user); err != nil {
		return nil, nil, err
	}

	// Auto-join global room
	_ = s.db.AddRoomMember(database.GlobalRoomID, user.ID, models.RoleMember)

	token, err := GenerateSessionToken()
	if err != nil {
		return nil, nil, err
	}

	sess := &models.Session{
		Token:     token,
		UserID:    user.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(30 * 24 * time.Hour), // 30-day session
	}

	if err := s.db.CreateSession(sess); err != nil {
		return nil, nil, err
	}

	return user, sess, nil
}

func (s *AuthService) Login(req protocol.LoginReq) (*models.User, *models.Session, error) {
	username := strings.TrimSpace(req.Username)
	user, err := s.db.GetUserByUsername(username)
	if err != nil {
		return nil, nil, err
	}
	if user == nil {
		return nil, nil, ErrInvalidCredentials
	}

	if !CheckPassword(req.Password, user.PasswordHash) {
		return nil, nil, ErrInvalidCredentials
	}

	now := time.Now().UTC()
	token, err := GenerateSessionToken()
	if err != nil {
		return nil, nil, err
	}

	sess := &models.Session{
		Token:     token,
		UserID:    user.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(30 * 24 * time.Hour),
	}

	if err := s.db.CreateSession(sess); err != nil {
		return nil, nil, err
	}

	// Auto-join global room in case it was missed
	_ = s.db.AddRoomMember(database.GlobalRoomID, user.ID, models.RoleMember)

	return user, sess, nil
}

func (s *AuthService) AuthenticateSession(token string) (*models.User, error) {
	sess, err := s.db.GetSession(token)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, ErrSessionExpired
	}

	user, err := s.db.GetUserByID(sess.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrSessionExpired
	}

	return user, nil
}
