package views

import (
	"strings"
	"testing"
	"time"

	"github.com/rtoms/chattui/internal/models"
)

func TestMessageGrouping(t *testing.T) {
	vm := NewChatViewModel()
	vm.SetSize(80, 24)

	now := time.Now()

	// Alice sends 2 consecutive messages
	m1 := models.Message{
		ID:                "m1",
		SenderID:          "user_alice",
		SenderUsername:    "alice",
		SenderDisplayName: "Alice",
		SenderColor:       "Cyan",
		Content:           "First message from Alice",
		CreatedAt:         now,
		IsSystem:          false,
	}
	m2 := models.Message{
		ID:                "m2",
		SenderID:          "user_alice",
		SenderUsername:    "alice",
		SenderDisplayName: "Alice",
		SenderColor:       "Cyan",
		Content:           "Second message from Alice",
		CreatedAt:         now.Add(10 * time.Second),
		IsSystem:          false,
	}

	// Bob sends 1 message
	m3 := models.Message{
		ID:                "m3",
		SenderID:          "user_bob",
		SenderUsername:    "bob",
		SenderDisplayName: "Bob",
		SenderColor:       "Yellow",
		Content:           "Hello from Bob",
		CreatedAt:         now.Add(20 * time.Second),
		IsSystem:          false,
	}

	vm.SetMessages([]models.Message{m1, m2, m3})
	content := vm.Viewport.View()

	// Alice's name should appear exactly ONCE
	aliceCount := strings.Count(content, "alice")
	if aliceCount != 1 {
		t.Fatalf("expected 'alice' to appear 1 time due to grouping, but appeared %d times in:\n%s", aliceCount, content)
	}

	// Bob's name should appear exactly ONCE
	bobCount := strings.Count(content, "bob")
	if bobCount != 1 {
		t.Fatalf("expected 'bob' to appear 1 time, but appeared %d times in:\n%s", bobCount, content)
	}

	// Both of Alice's message contents must be present
	if !strings.Contains(content, "First message from Alice") || !strings.Contains(content, "Second message from Alice") {
		t.Fatalf("missing Alice messages in:\n%s", content)
	}
	if !strings.Contains(content, "Hello from Bob") {
		t.Fatalf("missing Bob message in:\n%s", content)
	}
}
