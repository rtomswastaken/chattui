package server

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rtoms/chattui/internal/models"
	"github.com/rtoms/chattui/internal/protocol"
)

type testClient struct {
	conn net.Conn
}

func newTestClient(t *testing.T, addr string) *testClient {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to dial server: %v", err)
	}
	return &testClient{conn: conn}
}

func (c *testClient) Close() {
	_ = c.conn.Close()
}

func (c *testClient) send(t *testing.T, msg *protocol.WireMessage) {
	t.Helper()
	data, err := protocol.EncodeMessage(msg)
	if err != nil {
		t.Fatalf("failed to encode msg: %v", err)
	}
	if err := protocol.WriteFrame(c.conn, data); err != nil {
		t.Fatalf("failed to write frame: %v", err)
	}
}

func (c *testClient) read(t *testing.T) *protocol.WireMessage {
	t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	data, err := protocol.ReadFrame(c.conn)
	if err != nil {
		t.Fatalf("failed to read frame: %v", err)
	}
	msg, err := protocol.DecodeMessage(data)
	if err != nil {
		t.Fatalf("failed to decode message: %v", err)
	}
	return msg
}

func (c *testClient) readResponse(t *testing.T, reqID string) *protocol.WireMessage {
	t.Helper()
	for {
		msg := c.read(t)
		if msg.ID == reqID {
			return msg
		}
	}
}

func setupTestServer(t *testing.T) (*Server, string, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "chattui-srv-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	srv, err := NewServer(Config{
		Addr:              "127.0.0.1:0", // Ephemeral port
		DBPath:            dbPath,
		RetentionInterval: 1 * time.Hour,
		ExpiryInterval:    1 * time.Hour,
	})
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create server: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to listen on ephemeral port: %v", err)
	}
	srv.listener = listener
	addr := listener.Addr().String()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			clientConn := &ClientConn{
				id:     conn.RemoteAddr().String(),
				conn:   conn,
				server: srv,
			}
			srv.wg.Add(1)
			go func() {
				defer srv.wg.Done()
				srv.handleClient(clientConn)
			}()
		}
	}()

	cleanup := func() {
		srv.Stop()
		os.RemoveAll(tmpDir)
	}

	return srv, addr, cleanup
}

func TestServerE2E(t *testing.T) {
	_, addr, cleanup := setupTestServer(t)
	defer cleanup()

	// Client 1: Alice
	c1 := newTestClient(t, addr)
	defer c1.Close()

	// Register Alice
	regData1, _ := json.Marshal(protocol.RegisterReq{
		Username:    "alice",
		DisplayName: "Alice",
		Password:    "password123",
		UserColor:   "Cyan",
	})
	c1.send(t, &protocol.WireMessage{
		ID:   "req-1",
		Type: protocol.CmdRegister,
		Data: regData1,
	})

	resp1 := c1.readResponse(t, "req-1")
	if !resp1.Success {
		t.Fatalf("Alice register failed: %s", resp1.Error)
	}
	var authResp1 protocol.AuthResp
	_ = json.Unmarshal(resp1.Data, &authResp1)
	if authResp1.User.Username != "alice" {
		t.Fatalf("unexpected user: %s", authResp1.User.Username)
	}

	// Client 2: Bob
	c2 := newTestClient(t, addr)
	defer c2.Close()

	// Register Bob
	regData2, _ := json.Marshal(protocol.RegisterReq{
		Username:    "bob",
		DisplayName: "Bob",
		Password:    "password123",
		UserColor:   "Yellow",
	})
	c2.send(t, &protocol.WireMessage{
		ID:   "req-2",
		Type: protocol.CmdRegister,
		Data: regData2,
	})

	resp2 := c2.readResponse(t, "req-2")
	if !resp2.Success {
		t.Fatalf("Bob register failed: %s", resp2.Error)
	}
	var authResp2 protocol.AuthResp
	_ = json.Unmarshal(resp2.Data, &authResp2)

	// Alice sends message to global room
	globalRoomID := "room_global"
	sendData, _ := json.Marshal(protocol.SendMsgReq{
		TargetType: models.TargetRoom,
		RoomID:     &globalRoomID,
		Content:    "Hello chatTUI world!",
	})
	c1.send(t, &protocol.WireMessage{
		ID:   "req-3",
		Type: protocol.CmdSendMsg,
		Data: sendData,
	})

	// Read ack on Alice's side
	ack := c1.readResponse(t, "req-3")
	if !ack.Success {
		t.Fatalf("send message failed: %s", ack.Error)
	}

	// Send a DM from Bob to Alice
	dmData, _ := json.Marshal(protocol.SendMsgReq{
		TargetType:  models.TargetDM,
		RecipientID: &authResp1.User.ID,
		Content:     "Hey Alice, this is a secret DM",
	})
	c2.send(t, &protocol.WireMessage{
		ID:   "req-4",
		Type: protocol.CmdSendMsg,
		Data: dmData,
	})

	dmAck := c2.readResponse(t, "req-4")
	if !dmAck.Success {
		t.Fatalf("DM send failed: %s", dmAck.Error)
	}

	// Test Ping / Pong
	c1.send(t, &protocol.WireMessage{
		ID:   "ping-1",
		Type: protocol.CmdPing,
	})
	pong := c1.readResponse(t, "ping-1")
	if pong.Event != protocol.EventPong {
		t.Fatalf("expected pong event, got %+v", pong)
	}
}
