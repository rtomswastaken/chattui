package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rtoms/chattui/internal/auth"
	"github.com/rtoms/chattui/internal/chat"
	"github.com/rtoms/chattui/internal/database"
	"github.com/rtoms/chattui/internal/models"
	"github.com/rtoms/chattui/internal/presence"
	"github.com/rtoms/chattui/internal/protocol"
	"github.com/rtoms/chattui/internal/rooms"
)

type Config struct {
	Addr              string
	DBPath            string
	RetentionInterval time.Duration
	ExpiryInterval    time.Duration
}

type ClientConn struct {
	id     string
	conn   net.Conn
	user   *models.User
	server *Server
	mu     sync.Mutex
	closed bool
}

func (c *ClientConn) Send(msg *protocol.WireMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return errors.New("connection closed")
	}

	data, err := protocol.EncodeMessage(msg)
	if err != nil {
		return err
	}

	return protocol.WriteFrame(c.conn, data)
}

func (c *ClientConn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		_ = c.conn.Close()
	}
}

type Server struct {
	config    Config
	db        *database.DB
	authSvc   *auth.AuthService
	roomMgr   *rooms.RoomManager
	chatEng   *chat.Engine
	presence  *presence.Tracker
	listener  net.Listener
	clients   map[string]map[string]*ClientConn // userID -> connID -> ClientConn
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

func NewServer(cfg Config) (*Server, error) {
	db, err := database.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	s := &Server{
		config:   cfg,
		db:       db,
		clients:  make(map[string]map[string]*ClientConn),
		ctx:      ctx,
		cancel:   cancel,
	}

	s.authSvc = auth.NewAuthService(db)
	s.presence = presence.NewTracker(db)

	s.roomMgr = rooms.NewRoomManager(db, func(r models.Room) {
		// Broadcast room expired event
		s.BroadcastToRoom(r.ID, &protocol.WireMessage{
			Event: protocol.EventRoomExpired,
			Data:  mustJSON(r),
		})
	})

	s.chatEng = chat.NewEngine(db,
		func(targetType, targetID string, msg *models.Message) {
			wireMsg := &protocol.WireMessage{
				Event: protocol.EventMessage,
				Data:  mustJSON(msg),
			}
			if targetType == models.TargetRoom {
				s.BroadcastToRoom(targetID, wireMsg)
			} else {
				s.BroadcastToUser(targetID, wireMsg)
			}
		},
		func(targetUserID string, notif *models.Notification) {
			s.BroadcastToUser(targetUserID, &protocol.WireMessage{
				Event: protocol.EventNotification,
				Data:  mustJSON(notif),
			})
		},
	)

	// Subscribe to presence events to fan out to all connected clients
	s.presence.Subscribe(func(ev protocol.PresenceEvent) {
		s.BroadcastToAll(&protocol.WireMessage{
			Event: protocol.EventPresence,
			Data:  mustJSON(ev),
		})
	})

	// Start background workers
	retentionInt := cfg.RetentionInterval
	if retentionInt <= 0 {
		retentionInt = 15 * time.Minute
	}
	s.chatEng.StartRetentionWorker(ctx, retentionInt)

	expiryInt := cfg.ExpiryInterval
	if expiryInt <= 0 {
		expiryInt = 1 * time.Minute
	}
	s.roomMgr.StartExpirationWorker(ctx, expiryInt)

	return s, nil
}

func (s *Server) Start() error {
	l, err := net.Listen("tcp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.config.Addr, err)
	}
	s.listener = l
	log.Printf("[chatTUI] Server listening on %s", s.config.Addr)

	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return nil
			default:
				log.Printf("[chatTUI] Accept error: %v", err)
				continue
			}
		}

		clientConn := &ClientConn{
			id:     uuid.NewString(),
			conn:   conn,
			server: s,
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleClient(clientConn)
		}()
	}
}

func (s *Server) Stop() {
	s.cancel()
	if s.listener != nil {
		_ = s.listener.Close()
	}

	// Close all client connections
	s.mu.Lock()
	for _, userConns := range s.clients {
		for _, c := range userConns {
			c.Close()
		}
	}
	s.clients = make(map[string]map[string]*ClientConn)
	s.mu.Unlock()

	s.wg.Wait()
	_ = s.db.Close()
	log.Println("[chatTUI] Server stopped cleanly")
}

func (s *Server) registerClientConn(c *ClientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.clients[c.user.ID]; !ok {
		s.clients[c.user.ID] = make(map[string]*ClientConn)
	}
	s.clients[c.user.ID][c.id] = c
	s.presence.OnUserConnected(c.user)
}

func (s *Server) unregisterClientConn(c *ClientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if c.user != nil {
		if userConns, ok := s.clients[c.user.ID]; ok {
			delete(userConns, c.id)
			if len(userConns) == 0 {
				delete(s.clients, c.user.ID)
			}
		}
		s.presence.OnUserDisconnected(c.user)
	}
}

func (s *Server) BroadcastToUser(userID string, msg *protocol.WireMessage) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if conns, ok := s.clients[userID]; ok {
		for _, c := range conns {
			_ = c.Send(msg)
		}
	}
}

func (s *Server) BroadcastToRoom(roomID string, msg *protocol.WireMessage) {
	members, err := s.db.GetRoomMembers(roomID)
	if err != nil {
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, m := range members {
		if conns, ok := s.clients[m.UserID]; ok {
			for _, c := range conns {
				_ = c.Send(msg)
			}
		}
	}
}

func (s *Server) BroadcastToAll(msg *protocol.WireMessage) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, conns := range s.clients {
		for _, c := range conns {
			_ = c.Send(msg)
		}
	}
}

func (s *Server) handleClient(c *ClientConn) {
	defer func() {
		s.unregisterClientConn(c)
		c.Close()
	}()

	for {
		frame, err := protocol.ReadFrame(c.conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("[chatTUI] Read error: %v", err)
			return
		}

		wireMsg, err := protocol.DecodeMessage(frame)
		if err != nil {
			_ = c.Send(&protocol.WireMessage{
				Success: false,
				Error:   "invalid protocol frame JSON",
			})
			continue
		}

		s.routeMessage(c, wireMsg)
	}
}

func (s *Server) routeMessage(c *ClientConn, msg *protocol.WireMessage) {
	resp := &protocol.WireMessage{
		ID:      msg.ID,
		Type:    msg.Type,
		Success: true,
	}

	// Handle Ping before auth
	if msg.Type == protocol.CmdPing {
		resp.Event = protocol.EventPong
		_ = c.Send(resp)
		return
	}

	// Unauthenticated commands
	if c.user == nil {
		switch msg.Type {
		case protocol.CmdRegister:
			s.handleRegister(c, msg, resp)
		case protocol.CmdLogin:
			s.handleLogin(c, msg, resp)
		default:
			resp.Success = false
			resp.Error = "authentication required"
			_ = c.Send(resp)
		}
		return
	}

	// Authenticated commands
	switch msg.Type {
	case protocol.CmdSendMsg:
		s.handleSendMsg(c, msg, resp)
	case protocol.CmdJoinRoom:
		s.handleJoinRoom(c, msg, resp)
	case protocol.CmdLeaveRoom:
		s.handleLeaveRoom(c, msg, resp)
	case protocol.CmdCreateRoom:
		s.handleCreateRoom(c, msg, resp)
	case protocol.CmdListRooms:
		s.handleListRooms(c, msg, resp)
	case protocol.CmdListUsers:
		s.handleListUsers(c, msg, resp)
	case protocol.CmdGetHistory:
		s.handleGetHistory(c, msg, resp)
	case protocol.CmdSetPresence:
		s.handleSetPresence(c, msg, resp)
	case protocol.CmdUpdateProfile:
		s.handleUpdateProfile(c, msg, resp)
	case protocol.CmdGetProfile:
		s.handleGetProfile(c, msg, resp)
	case protocol.CmdMarkRead:
		s.handleMarkRead(c, msg, resp)
	case protocol.CmdKickMember:
		s.handleKickMember(c, msg, resp)
	case protocol.CmdBanMember:
		s.handleBanMember(c, msg, resp)
	case protocol.CmdMuteMember:
		s.handleMuteMember(c, msg, resp)
	case protocol.CmdTransferOwnership:
		s.handleTransferOwnership(c, msg, resp)
	default:
		resp.Success = false
		resp.Error = fmt.Sprintf("unknown command: %s", msg.Type)
		_ = c.Send(resp)
	}
}

func (s *Server) handleRegister(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.RegisterReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid register payload"
		_ = c.Send(resp)
		return
	}

	user, sess, err := s.authSvc.Register(req)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		_ = c.Send(resp)
		return
	}

	c.user = user
	s.registerClientConn(c)

	// Fetch initial rooms and unreads
	channels, _ := s.db.ListRooms(user.ID, true)
	unreads, _ := s.db.GetUnreadStates(user.ID, time.Now().UTC().Add(-24*time.Hour))

	authResp := protocol.AuthResp{
		User:     *user,
		Token:    sess.Token,
		Channels: channels,
		Unreads:  unreads,
	}

	resp.Data = mustJSON(authResp)
	_ = c.Send(resp)

	// Broadcast welcome message to global chat
	s.chatEng.BroadcastNewUserWelcome(user.Username)
}

func (s *Server) handleLogin(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.LoginReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid login payload"
		_ = c.Send(resp)
		return
	}

	user, sess, err := s.authSvc.Login(req)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		_ = c.Send(resp)
		return
	}

	c.user = user
	s.registerClientConn(c)

	channels, _ := s.db.ListRooms(user.ID, true)
	unreads, _ := s.db.GetUnreadStates(user.ID, time.Now().UTC().Add(-24*time.Hour))

	authResp := protocol.AuthResp{
		User:     *user,
		Token:    sess.Token,
		Channels: channels,
		Unreads:  unreads,
	}

	resp.Data = mustJSON(authResp)
	_ = c.Send(resp)
}

func (s *Server) handleSendMsg(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.SendMsgReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid send message payload"
		_ = c.Send(resp)
		return
	}

	if req.TargetType == models.TargetRoom {
		if req.RoomID == nil {
			resp.Success = false
			resp.Error = "room_id required"
			_ = c.Send(resp)
			return
		}

		room, err := s.db.GetRoomByID(*req.RoomID)
		if err != nil || room == nil {
			resp.Success = false
			resp.Error = "room not found"
			_ = c.Send(resp)
			return
		}

		if err := s.roomMgr.CanSendMessage(c.user.ID, room.ID); err != nil {
			resp.Success = false
			resp.Error = err.Error()
			_ = c.Send(resp)
			return
		}

		saved, err := s.chatEng.SendRoomMessage(c.user, room, req.Content)
		if err != nil {
			resp.Success = false
			resp.Error = err.Error()
			_ = c.Send(resp)
			return
		}

		resp.Data = mustJSON(saved)
		_ = c.Send(resp)
		return
	}

	if req.TargetType == models.TargetDM {
		if req.RecipientID == nil {
			resp.Success = false
			resp.Error = "recipient_id required"
			_ = c.Send(resp)
			return
		}

		recipient, err := s.db.GetUserByID(*req.RecipientID)
		if err != nil || recipient == nil {
			resp.Success = false
			resp.Error = "recipient not found"
			_ = c.Send(resp)
			return
		}

		saved, err := s.chatEng.SendDM(c.user, recipient, req.Content)
		if err != nil {
			resp.Success = false
			resp.Error = err.Error()
			_ = c.Send(resp)
			return
		}

		resp.Data = mustJSON(saved)
		_ = c.Send(resp)
		return
	}

	resp.Success = false
	resp.Error = "unsupported target type"
	_ = c.Send(resp)
}

func (s *Server) handleJoinRoom(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.JoinRoomReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid join room payload"
		_ = c.Send(resp)
		return
	}

	room, err := s.roomMgr.JoinRoom(c.user.ID, req.RoomName, req.Password)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		_ = c.Send(resp)
		return
	}

	resp.Data = mustJSON(room)
	_ = c.Send(resp)

	// Broadcast member join event
	joinMsg := fmt.Sprintf("● %s joined #%s", c.user.Username, room.Name)
	_, _ = s.chatEng.BroadcastSystemMessage(room.ID, joinMsg)
}

func (s *Server) handleLeaveRoom(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.LeaveRoomReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid leave room payload"
		_ = c.Send(resp)
		return
	}

	room, err := s.db.GetRoomByID(req.RoomID)
	if err != nil || room == nil {
		resp.Success = false
		resp.Error = "room not found"
		_ = c.Send(resp)
		return
	}

	if err := s.roomMgr.LeaveRoom(c.user.ID, req.RoomID); err != nil {
		resp.Success = false
		resp.Error = err.Error()
		_ = c.Send(resp)
		return
	}

	_ = c.Send(resp)

	// Broadcast leave event
	leaveMsg := fmt.Sprintf("○ %s left #%s", c.user.Username, room.Name)
	_, _ = s.chatEng.BroadcastSystemMessage(room.ID, leaveMsg)
}

func (s *Server) handleCreateRoom(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.CreateRoomReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid create room payload"
		_ = c.Send(resp)
		return
	}

	room, err := s.roomMgr.CreateRoom(c.user.ID, req)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		_ = c.Send(resp)
		return
	}

	resp.Data = mustJSON(room)
	_ = c.Send(resp)

	// If public, broadcast room creation to everyone
	if !room.IsPrivate {
		s.BroadcastToAll(&protocol.WireMessage{
			Event: protocol.EventRoomUpdate,
			Data:  mustJSON(room),
		})
	}
}

func (s *Server) handleListRooms(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.ListRoomsReq
	_ = json.Unmarshal(msg.Data, &req)

	roomsList, err := s.db.ListRooms(c.user.ID, req.IncludePrivateJoined)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		_ = c.Send(resp)
		return
	}

	resp.Data = mustJSON(protocol.RoomListResp{Rooms: roomsList})
	_ = c.Send(resp)
}

func (s *Server) handleListUsers(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	usersList, err := s.db.ListUsers()
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		_ = c.Send(resp)
		return
	}

	resp.Data = mustJSON(protocol.UserListResp{Users: usersList})
	_ = c.Send(resp)
}

func (s *Server) handleGetHistory(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.GetHistoryReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid get history payload"
		_ = c.Send(resp)
		return
	}

	messages, err := s.chatEng.GetHistory(req, c.user.ID)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		_ = c.Send(resp)
		return
	}

	targetID := ""
	if req.RoomID != nil {
		targetID = *req.RoomID
	} else if req.OtherID != nil {
		targetID = *req.OtherID
	}

	resp.Data = mustJSON(protocol.HistoryResp{
		TargetType: req.TargetType,
		TargetID:   targetID,
		Messages:   messages,
	})
	_ = c.Send(resp)
}

func (s *Server) handleSetPresence(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.SetPresenceReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid presence payload"
		_ = c.Send(resp)
		return
	}

	s.presence.SetPresence(c.user, req.Presence, req.CustomStatus)
	_ = c.Send(resp)
}

func (s *Server) handleUpdateProfile(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.UpdateProfileReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid profile payload"
		_ = c.Send(resp)
		return
	}

	var validColor *string
	if req.UserColor != nil {
		norm := auth.NormalizeColor(*req.UserColor)
		validColor = &norm
	}

	err := s.db.UpdateUserProfile(c.user.ID, req.DisplayName, validColor, req.CustomStatus, req.Presence)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		_ = c.Send(resp)
		return
	}

	updated, _ := s.db.GetUserByID(c.user.ID)
	if updated != nil {
		c.user = updated
	}

	resp.Data = mustJSON(c.user.ToProfile())
	_ = c.Send(resp)

	// Broadcast presence update if status changed
	s.presence.SetPresence(c.user, c.user.Presence, &c.user.CustomStatus)
}

func (s *Server) handleGetProfile(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.GetProfileReq
	_ = json.Unmarshal(msg.Data, &req)

	var target *models.User
	var err error

	if req.UserID != "" {
		target, err = s.db.GetUserByID(req.UserID)
	} else if req.Username != "" {
		target, err = s.db.GetUserByUsername(req.Username)
	} else {
		target = c.user
	}

	if err != nil || target == nil {
		resp.Success = false
		resp.Error = "user not found"
		_ = c.Send(resp)
		return
	}

	resp.Data = mustJSON(target.ToProfile())
	_ = c.Send(resp)
}

func (s *Server) handleMarkRead(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.MarkReadReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid mark read payload"
		_ = c.Send(resp)
		return
	}

	_ = s.db.UpdateReadState(c.user.ID, req.TargetType, req.TargetID, req.MessageID)
	_ = c.Send(resp)
}

func (s *Server) handleKickMember(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.KickMemberReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid kick payload"
		_ = c.Send(resp)
		return
	}

	if err := s.roomMgr.KickMember(c.user.ID, req.RoomID, req.UserID); err != nil {
		resp.Success = false
		resp.Error = err.Error()
		_ = c.Send(resp)
		return
	}

	_ = c.Send(resp)
	targetUser, _ := s.db.GetUserByID(req.UserID)
	tName := req.UserID
	if targetUser != nil {
		tName = targetUser.Username
	}
	_, _ = s.chatEng.BroadcastSystemMessage(req.RoomID, fmt.Sprintf("○ %s was kicked from the room", tName))
}

func (s *Server) handleBanMember(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.BanMemberReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid ban payload"
		_ = c.Send(resp)
		return
	}

	if err := s.roomMgr.BanMember(c.user.ID, req.RoomID, req.UserID, req.Reason); err != nil {
		resp.Success = false
		resp.Error = err.Error()
		_ = c.Send(resp)
		return
	}

	_ = c.Send(resp)
	targetUser, _ := s.db.GetUserByID(req.UserID)
	tName := req.UserID
	if targetUser != nil {
		tName = targetUser.Username
	}
	_, _ = s.chatEng.BroadcastSystemMessage(req.RoomID, fmt.Sprintf("○ %s was banned from the room", tName))
}

func (s *Server) handleMuteMember(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.MuteMemberReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid mute payload"
		_ = c.Send(resp)
		return
	}

	if err := s.roomMgr.MuteMember(c.user.ID, req.RoomID, req.UserID, req.DurationMinutes); err != nil {
		resp.Success = false
		resp.Error = err.Error()
		_ = c.Send(resp)
		return
	}

	_ = c.Send(resp)
	targetUser, _ := s.db.GetUserByID(req.UserID)
	tName := req.UserID
	if targetUser != nil {
		tName = targetUser.Username
	}
	_, _ = s.chatEng.BroadcastSystemMessage(req.RoomID, fmt.Sprintf("◐ %s was muted for %d minutes", tName, req.DurationMinutes))
}

func (s *Server) handleTransferOwnership(c *ClientConn, msg *protocol.WireMessage, resp *protocol.WireMessage) {
	var req protocol.TransferOwnershipReq
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		resp.Success = false
		resp.Error = "invalid transfer payload"
		_ = c.Send(resp)
		return
	}

	if err := s.roomMgr.TransferOwnership(c.user.ID, req.RoomID, req.NewOwnerID); err != nil {
		resp.Success = false
		resp.Error = err.Error()
		_ = c.Send(resp)
		return
	}

	_ = c.Send(resp)
	targetUser, _ := s.db.GetUserByID(req.NewOwnerID)
	tName := req.NewOwnerID
	if targetUser != nil {
		tName = targetUser.Username
	}
	_, _ = s.chatEng.BroadcastSystemMessage(req.RoomID, fmt.Sprintf("● %s transferred room ownership to %s", c.user.Username, tName))
}

func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
