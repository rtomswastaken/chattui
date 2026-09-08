package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rtoms/chattui/internal/models"
	"github.com/rtoms/chattui/internal/protocol"
)

type ConnState int

const (
	StateDisconnected ConnState = iota
	StateReconnecting
	StateConnected
)

func (s ConnState) String() string {
	switch s {
	case StateConnected:
		return "Connected"
	case StateReconnecting:
		return "Reconnecting..."
	default:
		return "Disconnected"
	}
}

func (s ConnState) Indicator() string {
	switch s {
	case StateConnected:
		return "● Connected"
	case StateReconnecting:
		return "◐ Reconnecting..."
	default:
		return "○ Disconnected"
	}
}

type Client struct {
	addr        string
	conn        net.Conn
	connMu      sync.Mutex
	state       ConnState
	stateMu     sync.RWMutex
	onState     func(ConnState)
	events      chan *protocol.WireMessage
	pending     map[string]chan *protocol.WireMessage
	pendingMu   sync.Mutex
	user        *models.User
	token       string
	userMu      sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	closed      int32
}

func NewClient(addr string, onState func(ConnState)) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		addr:    addr,
		state:   StateDisconnected,
		onState: onState,
		events:  make(chan *protocol.WireMessage, 200),
		pending: make(map[string]chan *protocol.WireMessage),
		ctx:     ctx,
		cancel:  cancel,
	}
	return c
}

func (c *Client) Events() <-chan *protocol.WireMessage {
	return c.events
}

func (c *Client) State() ConnState {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.state
}

func (c *Client) setState(s ConnState) {
	c.stateMu.Lock()
	c.state = s
	c.stateMu.Unlock()
	if c.onState != nil {
		c.onState(s)
	}
}

func (c *Client) User() *models.User {
	c.userMu.RLock()
	defer c.userMu.RUnlock()
	return c.user
}

func (c *Client) SetUser(u *models.User) {
	c.userMu.Lock()
	c.user = u
	c.userMu.Unlock()
}

func (c *Client) SetToken(t string) {
	c.userMu.Lock()
	c.token = t
	c.userMu.Unlock()
}

func (c *Client) Connect() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	conn, err := net.DialTimeout("tcp", c.addr, 5*time.Second)
	if err != nil {
		c.setState(StateDisconnected)
		return err
	}

	c.conn = conn
	c.setState(StateConnected)

	go c.readLoop(conn)
	go c.heartbeatLoop()

	return nil
}

func (c *Client) StartAutoReconnect() {
	go func() {
		backoff := 1 * time.Second
		for {
			select {
			case <-c.ctx.Done():
				return
			default:
				if atomic.LoadInt32(&c.closed) == 1 {
					return
				}
				if c.State() == StateDisconnected {
					c.setState(StateReconnecting)
					err := c.Connect()
					if err != nil {
						c.setState(StateDisconnected)
						time.Sleep(backoff)
						if backoff < 10*time.Second {
							backoff *= 2
						}
						continue
					}
					backoff = 1 * time.Second
				}
				time.Sleep(2 * time.Second)
			}
		}
	}()
}

func (c *Client) heartbeatLoop() {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if c.State() == StateConnected {
				_, _ = c.SendRequest(protocol.CmdPing, nil, 3*time.Second)
			}
		}
	}
}

func (c *Client) readLoop(conn net.Conn) {
	for {
		frame, err := protocol.ReadFrame(conn)
		if err != nil {
			if atomic.LoadInt32(&c.closed) == 0 {
				c.setState(StateDisconnected)
			}
			return
		}

		msg, err := protocol.DecodeMessage(frame)
		if err != nil {
			continue
		}

		// If message matches a pending request ID
		if msg.ID != "" {
			c.pendingMu.Lock()
			ch, ok := c.pending[msg.ID]
			c.pendingMu.Unlock()
			if ok {
				select {
				case ch <- msg:
				default:
				}
				continue
			}
		}

		// Otherwise, it's a push event
		select {
		case c.events <- msg:
		default:
			// Buffer full, drop oldest or drop event
		}
	}
}

func (c *Client) SendRequest(cmdType string, payload interface{}, timeout time.Duration) (*protocol.WireMessage, error) {
	if c.State() != StateConnected {
		return nil, errors.New("not connected to server")
	}

	reqID := uuid.NewString()
	var rawData json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		rawData = b
	}

	wireMsg := &protocol.WireMessage{
		ID:   reqID,
		Type: cmdType,
		Data: rawData,
	}

	respChan := make(chan *protocol.WireMessage, 1)
	c.pendingMu.Lock()
	c.pending[reqID] = respChan
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, reqID)
		c.pendingMu.Unlock()
	}()

	data, err := protocol.EncodeMessage(wireMsg)
	if err != nil {
		return nil, err
	}

	c.connMu.Lock()
	if c.conn == nil {
		c.connMu.Unlock()
		return nil, errors.New("not connected")
	}
	err = protocol.WriteFrame(c.conn, data)
	c.connMu.Unlock()
	if err != nil {
		c.setState(StateDisconnected)
		return nil, fmt.Errorf("write error: %w", err)
	}

	if timeout <= 0 {
		timeout = 7 * time.Second
	}

	select {
	case <-time.After(timeout):
		return nil, errors.New("request timed out")
	case resp := <-respChan:
		if !resp.Success {
			return nil, errors.New(resp.Error)
		}
		return resp, nil
	}
}

func (c *Client) Register(req protocol.RegisterReq) (*protocol.AuthResp, error) {
	resp, err := c.SendRequest(protocol.CmdRegister, req, 10*time.Second)
	if err != nil {
		return nil, err
	}

	var authResp protocol.AuthResp
	if err := json.Unmarshal(resp.Data, &authResp); err != nil {
		return nil, err
	}

	c.SetUser(&authResp.User)
	c.SetToken(authResp.Token)
	return &authResp, nil
}

func (c *Client) Login(req protocol.LoginReq) (*protocol.AuthResp, error) {
	resp, err := c.SendRequest(protocol.CmdLogin, req, 10*time.Second)
	if err != nil {
		return nil, err
	}

	var authResp protocol.AuthResp
	if err := json.Unmarshal(resp.Data, &authResp); err != nil {
		return nil, err
	}

	c.SetUser(&authResp.User)
	c.SetToken(authResp.Token)
	return &authResp, nil
}

func (c *Client) SendMessage(targetType string, roomID, recipientID *string, content string) (*models.Message, error) {
	req := protocol.SendMsgReq{
		TargetType:  targetType,
		RoomID:      roomID,
		RecipientID: recipientID,
		Content:     content,
	}

	resp, err := c.SendRequest(protocol.CmdSendMsg, req, 5*time.Second)
	if err != nil {
		return nil, err
	}

	var msg models.Message
	if err := json.Unmarshal(resp.Data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (c *Client) JoinRoom(roomName string, password *string) (*models.Room, error) {
	req := protocol.JoinRoomReq{
		RoomName: roomName,
		Password: password,
	}
	resp, err := c.SendRequest(protocol.CmdJoinRoom, req, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var room models.Room
	if err := json.Unmarshal(resp.Data, &room); err != nil {
		return nil, err
	}
	return &room, nil
}

func (c *Client) LeaveRoom(roomID string) error {
	req := protocol.LeaveRoomReq{RoomID: roomID}
	_, err := c.SendRequest(protocol.CmdLeaveRoom, req, 5*time.Second)
	return err
}

func (c *Client) CreateRoom(req protocol.CreateRoomReq) (*models.Room, error) {
	resp, err := c.SendRequest(protocol.CmdCreateRoom, req, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var room models.Room
	if err := json.Unmarshal(resp.Data, &room); err != nil {
		return nil, err
	}
	return &room, nil
}

func (c *Client) ListRooms(includePrivateJoined bool) ([]models.Room, error) {
	req := protocol.ListRoomsReq{IncludePrivateJoined: includePrivateJoined}
	resp, err := c.SendRequest(protocol.CmdListRooms, req, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var r protocol.RoomListResp
	if err := json.Unmarshal(resp.Data, &r); err != nil {
		return nil, err
	}
	return r.Rooms, nil
}

func (c *Client) ListUsers() ([]models.UserProfile, error) {
	resp, err := c.SendRequest(protocol.CmdListUsers, nil, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var r protocol.UserListResp
	if err := json.Unmarshal(resp.Data, &r); err != nil {
		return nil, err
	}
	return r.Users, nil
}

func (c *Client) GetHistory(targetType string, roomID, otherID *string, limit int) ([]models.Message, error) {
	req := protocol.GetHistoryReq{
		TargetType: targetType,
		RoomID:     roomID,
		OtherID:    otherID,
		Limit:      limit,
	}
	resp, err := c.SendRequest(protocol.CmdGetHistory, req, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var r protocol.HistoryResp
	if err := json.Unmarshal(resp.Data, &r); err != nil {
		return nil, err
	}
	return r.Messages, nil
}

func (c *Client) SetPresence(state string, status *string) error {
	req := protocol.SetPresenceReq{
		Presence:     state,
		CustomStatus: status,
	}
	_, err := c.SendRequest(protocol.CmdSetPresence, req, 3*time.Second)
	return err
}

func (c *Client) UpdateProfile(displayName, userColor, customStatus, presence *string) (*models.UserProfile, error) {
	req := protocol.UpdateProfileReq{
		DisplayName:  displayName,
		UserColor:    userColor,
		CustomStatus: customStatus,
		Presence:     presence,
	}
	resp, err := c.SendRequest(protocol.CmdUpdateProfile, req, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var p models.UserProfile
	if err := json.Unmarshal(resp.Data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) GetProfile(username, userID string) (*models.UserProfile, error) {
	req := protocol.GetProfileReq{
		Username: username,
		UserID:   userID,
	}
	resp, err := c.SendRequest(protocol.CmdGetProfile, req, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var p models.UserProfile
	if err := json.Unmarshal(resp.Data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) MarkRead(targetType, targetID, msgID string) error {
	req := protocol.MarkReadReq{
		TargetType: targetType,
		TargetID:   targetID,
		MessageID:  msgID,
	}
	_, err := c.SendRequest(protocol.CmdMarkRead, req, 2*time.Second)
	return err
}

func (c *Client) KickMember(roomID, userID string) error {
	req := protocol.KickMemberReq{RoomID: roomID, UserID: userID}
	_, err := c.SendRequest(protocol.CmdKickMember, req, 5*time.Second)
	return err
}

func (c *Client) BanMember(roomID, userID, reason string) error {
	req := protocol.BanMemberReq{RoomID: roomID, UserID: userID, Reason: reason}
	_, err := c.SendRequest(protocol.CmdBanMember, req, 5*time.Second)
	return err
}

func (c *Client) MuteMember(roomID, userID string, durationMinutes int) error {
	req := protocol.MuteMemberReq{RoomID: roomID, UserID: userID, DurationMinutes: durationMinutes}
	_, err := c.SendRequest(protocol.CmdMuteMember, req, 5*time.Second)
	return err
}

func (c *Client) TransferOwnership(roomID, newOwnerID string) error {
	req := protocol.TransferOwnershipReq{RoomID: roomID, NewOwnerID: newOwnerID}
	_, err := c.SendRequest(protocol.CmdTransferOwnership, req, 5*time.Second)
	return err
}

func (c *Client) Close() {
	atomic.StoreInt32(&c.closed, 1)
	c.cancel()
	c.connMu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.connMu.Unlock()
	c.setState(StateDisconnected)
}
