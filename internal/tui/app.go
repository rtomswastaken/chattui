package tui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rtoms/chattui/internal/client"
	"github.com/rtoms/chattui/internal/database"
	"github.com/rtoms/chattui/internal/models"
	"github.com/rtoms/chattui/internal/protocol"
	"github.com/rtoms/chattui/internal/tui/views"
)

type AppScreen int

const (
	ScreenAuth AppScreen = iota
	ScreenChat
)

// Internal Bubble Tea messages
type WireEventMsg *protocol.WireMessage
type ConnStateMsg client.ConnState
type InactivityTickMsg time.Time

type AuthSuccessMsg struct {
	Resp *protocol.AuthResp
}

type AuthErrMsg struct {
	Err string
}

type JoinRoomSuccessMsg struct {
	Room *models.Room
}

type JoinRoomErrMsg struct {
	Err string
}

type CreateRoomSuccessMsg struct {
	Room *models.Room
}

type CreateRoomErrMsg struct {
	Err string
}

type UpdateProfileSuccessMsg struct {
	Profile *models.UserProfile
}

type UpdateProfileErrMsg struct {
	Err string
}

type TransferOwnershipResultMsg struct {
	Err error
}

type ChannelHistoryMsg struct {
	TargetType string
	TargetID   string
	Messages   []models.Message
}

type OpenNewDMMsg struct {
	Users []models.UserProfile
	Err   error
}

type OpenRoomBrowserMsg struct {
	Rooms []models.Room
	Err   error
}

type ExecuteDMMsg struct {
	TargetUser models.UserProfile
	Content    string
}

type ActionToastMsg struct {
	Text string
}

type LeaveRoomResultMsg struct {
	RoomID string
	Err    error
}

type OpenTransferConfirmMsg struct {
	RoomID     string
	RoomName   string
	TargetUser models.UserProfile
}

type AppModel struct {
	Screen       AppScreen
	Client       *client.Client
	AuthView     views.AuthModel
	ChatView     views.ChatViewModel
	Modals       views.ModalState
	LastActivity time.Time
	IsIdle       bool
	Width        int
	Height       int
}

func NewApp(c *client.Client) AppModel {
	authM := views.NewAuthModel()
	chatM := views.NewChatViewModel()
	modals := views.NewModalState()

	return AppModel{
		Screen:       ScreenAuth,
		Client:       c,
		AuthView:     authM,
		ChatView:     chatM,
		Modals:       modals,
		LastActivity: time.Now(),
		IsIdle:       false,
		Width:        80,
		Height:       24,
	}
}

func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		m.AuthView.Init(),
		m.listenEvents(),
		m.scheduleInactivityTick(),
	)
}

func (m AppModel) listenEvents() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.Client.Events()
		if !ok {
			return nil
		}
		return WireEventMsg(ev)
	}
}

func (m AppModel) scheduleInactivityTick() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return InactivityTickMsg(t)
	})
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.AuthView.Width = msg.Width
		m.AuthView.Height = msg.Height
		m.ChatView.SetSize(msg.Width, msg.Height)
		m.Modals.Width = msg.Width
		m.Modals.Height = msg.Height
		return m, nil

	case InactivityTickMsg:
		if m.Screen == ScreenChat && !m.IsIdle && time.Since(m.LastActivity) > 5*time.Minute {
			m.IsIdle = true
			if m.Client.User() != nil {
				m.Client.User().Presence = models.PresenceIdle
				_ = m.Client.SetPresence(models.PresenceIdle, nil)
			}
		}
		cmds = append(cmds, m.scheduleInactivityTick())
		return m, tea.Batch(cmds...)

	case ConnStateMsg:
		m.ChatView.ConnState = client.ConnState(msg)
		return m, nil

	case WireEventMsg:
		cmds = append(cmds, m.listenEvents())
		if msg == nil {
			return m, tea.Batch(cmds...)
		}
		m.handleServerEvent((*protocol.WireMessage)(msg))
		return m, tea.Batch(cmds...)

	case views.AuthSubmitMsg:
		return m, m.handleAuthSubmit(msg)

	case AuthSuccessMsg:
		m.onAuthSuccess(msg.Resp)
		if len(m.ChatView.SidebarItems) > 0 {
			firstItem := m.ChatView.SidebarItems[0]
			return m, m.fetchHistory(firstItem)
		}
		return m, nil

	case AuthErrMsg:
		m.AuthView.SetError(msg.Err)
		return m, nil

	case OpenNewDMMsg:
		if msg.Err != nil {
			m.ChatView.SetToast("Failed to list users: "+msg.Err.Error(), 3*time.Second)
			return m, nil
		}
		m.Modals.OpenNewDM(msg.Users)
		return m, nil

	case OpenRoomBrowserMsg:
		if msg.Err != nil {
			m.ChatView.SetToast("Failed to list rooms: "+msg.Err.Error(), 3*time.Second)
			return m, nil
		}
		m.Modals.OpenRoomBrowser(msg.Rooms)
		return m, nil

	case ExecuteDMMsg:
		item := m.startDM(msg.TargetUser)
		m.sendChatMessage(msg.Content)
		return m, m.fetchHistory(item)

	case ActionToastMsg:
		m.ChatView.SetToast(msg.Text, 4*time.Second)
		return m, nil

	case LeaveRoomResultMsg:
		if msg.Err != nil {
			m.ChatView.SetToast(msg.Err.Error(), 4*time.Second)
			return m, nil
		}
		var remaining []views.SidebarItem
		for _, it := range m.ChatView.SidebarItems {
			if it.ID != msg.RoomID {
				remaining = append(remaining, it)
			}
		}
		m.ChatView.SidebarItems = remaining
		if m.ChatView.ActiveItem.ID == msg.RoomID {
			m.switchChannel(remaining[0])
			return m, m.fetchHistory(remaining[0])
		}
		m.ChatView.SetToast("Left room", 3*time.Second)
		return m, nil

	case OpenTransferConfirmMsg:
		m.Modals.OpenTransferConfirm(msg.RoomID, msg.RoomName, msg.TargetUser)
		return m, nil

	case views.ChannelSelectedMsg:
		m.switchChannel(msg.Item)
		return m, m.fetchHistory(msg.Item)

	case views.JoinRoomRequestMsg:
		return m, m.handleJoinRoom(msg)

	case JoinRoomSuccessMsg:
		cat := views.CategoryRooms
		if msg.Room.IsPrivate {
			cat = views.CategoryChats
		}
		item := views.SidebarItem{
			ID:       msg.Room.ID,
			Name:     msg.Room.Name,
			Category: cat,
			IsDM:     false,
		}
		exists := false
		for _, it := range m.ChatView.SidebarItems {
			if it.ID == msg.Room.ID {
				exists = true
				break
			}
		}
		if !exists {
			m.ChatView.SidebarItems = append(m.ChatView.SidebarItems, item)
		}
		m.switchChannel(item)
		return m, m.fetchHistory(item)

	case JoinRoomErrMsg:
		m.ChatView.SetToast(msg.Err, 4*time.Second)
		return m, nil

	case views.CreateRoomRequestMsg:
		return m, m.handleCreateRoom(msg)

	case CreateRoomSuccessMsg:
		cat := views.CategoryRooms
		if msg.Room.IsPrivate {
			cat = views.CategoryChats
		}
		item := views.SidebarItem{
			ID:       msg.Room.ID,
			Name:     msg.Room.Name,
			Category: cat,
			IsDM:     false,
		}
		m.ChatView.SidebarItems = append(m.ChatView.SidebarItems, item)
		m.switchChannel(item)
		return m, m.fetchHistory(item)

	case CreateRoomErrMsg:
		m.ChatView.SetToast(msg.Err, 4*time.Second)
		return m, nil

	case views.StartDMRequestMsg:
		item := m.startDM(msg.TargetUser)
		return m, m.fetchHistory(item)

	case views.UpdateProfileRequestMsg:
		return m, m.handleUpdateProfile(msg)

	case UpdateProfileSuccessMsg:
		if m.Client.User() != nil {
			m.Client.User().DisplayName = msg.Profile.DisplayName
			m.Client.User().UserColor = msg.Profile.UserColor
			m.Client.User().CustomStatus = msg.Profile.CustomStatus
			m.Client.User().Presence = msg.Profile.Presence
		}
		m.ChatView.SetToast("Profile updated successfully", 3*time.Second)
		return m, nil

	case UpdateProfileErrMsg:
		m.ChatView.SetToast(msg.Err, 4*time.Second)
		return m, nil

	case views.TransferOwnershipConfirmMsg:
		return m, m.handleTransferOwnership(msg)

	case TransferOwnershipResultMsg:
		if msg.Err != nil {
			m.ChatView.SetToast(msg.Err.Error(), 4*time.Second)
		} else {
			m.ChatView.SetToast("Room ownership transferred", 4*time.Second)
		}
		return m, nil

	case ChannelHistoryMsg:
		if msg.TargetID == m.ChatView.ActiveItem.ID {
			m.ChatView.SetMessages(msg.Messages)
			if len(msg.Messages) > 0 {
				lastID := msg.Messages[len(msg.Messages)-1].ID
				_ = m.Client.MarkRead(msg.TargetType, msg.TargetID, lastID)
			}
		}
		return m, nil

	case string:
		// String returned from command palette
		if strings.HasPrefix(msg, "/") {
			return m.executeCommand(msg)
		}
		return m, nil

	case tea.KeyMsg:
		m.LastActivity = time.Now()
		if m.IsIdle && m.Screen == ScreenChat {
			m.IsIdle = false
			if m.Client.User() != nil {
				m.Client.User().Presence = models.PresenceOnline
				_ = m.Client.SetPresence(models.PresenceOnline, nil)
			}
		}

		// Global Hotkeys
		switch msg.String() {
		case "ctrl+q":
			if m.Client.User() != nil {
				_ = m.Client.SetPresence(models.PresenceOffline, nil)
			}
			m.Client.Close()
			return m, tea.Quit

		case "ctrl+k":
			if m.Screen == ScreenChat && m.Modals.ActiveType == views.ModalNone {
				m.Modals.OpenCommandPalette()
				return m, nil
			}

		case "ctrl+n":
			if m.Screen == ScreenChat && m.Modals.ActiveType == views.ModalNone {
				client := m.Client
				return m, func() tea.Msg {
					users, err := client.ListUsers()
					return OpenNewDMMsg{Users: users, Err: err}
				}
			}

		case "ctrl+r":
			if m.Screen == ScreenChat && m.Modals.ActiveType == views.ModalNone {
				client := m.Client
				return m, func() tea.Msg {
					rooms, err := client.ListRooms(false)
					return OpenRoomBrowserMsg{Rooms: rooms, Err: err}
				}
			}

		case "ctrl+p":
			if m.Screen == ScreenChat && m.Modals.ActiveType == views.ModalNone && m.Client.User() != nil {
				m.Modals.OpenProfile(m.Client.User().ToProfile())
				return m, nil
			}

		case "ctrl+h":
			if m.Screen == ScreenChat && m.Modals.ActiveType == views.ModalNone {
				m.Modals.OpenHelp()
				return m, nil
			}
		}
	}

	// Route updates based on modals and screen
	if m.Modals.ActiveType != views.ModalNone {
		m.Modals, cmd = m.Modals.Update(msg)
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)
	}

	if m.Screen == ScreenAuth {
		m.AuthView, cmd = m.AuthView.Update(msg)
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)
	}

	// ScreenChat text submission
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "enter" && !m.ChatView.SidebarFocused {
		inputVal := strings.TrimSpace(m.ChatView.Input.Value())
		if inputVal != "" {
			m.ChatView.Input.Reset()
			if strings.HasPrefix(inputVal, "/") {
				return m.executeCommand(inputVal)
			} else {
				m.sendChatMessage(inputVal)
			}
			return m, nil
		}
	}

	m.ChatView, cmd = m.ChatView.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *AppModel) handleServerEvent(wireMsg *protocol.WireMessage) {
	switch wireMsg.Event {
	case protocol.EventMessage:
		var msg models.Message
		if err := json.Unmarshal(wireMsg.Data, &msg); err == nil {
			isForActive := false
			if m.ChatView.ActiveItem.IsDM {
				if msg.TargetType == models.TargetDM &&
					((msg.RecipientID != nil && *msg.RecipientID == m.ChatView.ActiveItem.ID) ||
						(msg.SenderID == m.ChatView.ActiveItem.ID)) {
					isForActive = true
				}
			} else {
				if msg.TargetType == models.TargetRoom && msg.RoomID != nil && *msg.RoomID == m.ChatView.ActiveItem.ID {
					isForActive = true
				}
			}

			if isForActive {
				m.ChatView.AddMessage(msg)
				_ = m.Client.MarkRead(msg.TargetType, m.ChatView.ActiveItem.ID, msg.ID)
			} else {
				found := false
				for i := range m.ChatView.SidebarItems {
					item := &m.ChatView.SidebarItems[i]
					if item.IsDM && msg.TargetType == models.TargetDM && (item.ID == msg.SenderID || (msg.RecipientID != nil && item.ID == *msg.RecipientID)) {
						item.UnreadCount++
						found = true
						break
					} else if !item.IsDM && msg.TargetType == models.TargetRoom && msg.RoomID != nil && *msg.RoomID == item.ID {
						item.UnreadCount++
						found = true
						break
					}
				}

				// If incoming DM from someone not yet in sidebar, automatically add it!
				if !found && msg.TargetType == models.TargetDM {
					otherID := msg.SenderID
					otherName := msg.SenderUsername
					if m.Client.User() != nil && msg.SenderID == m.Client.User().ID && msg.RecipientID != nil {
						otherID = *msg.RecipientID
						otherName = "User"
					}
					m.ChatView.SidebarItems = append(m.ChatView.SidebarItems, views.SidebarItem{
						ID:          otherID,
						Name:        otherName,
						Category:    views.CategoryDMs,
						IsDM:        true,
						UnreadCount: 1,
					})
					m.ChatView.SetToast("New DM from @"+msg.SenderUsername, 4*time.Second)
				}
			}
		}

	case protocol.EventNotification:
		var notif models.Notification
		if err := json.Unmarshal(wireMsg.Data, &notif); err == nil {
			m.ChatView.SetToast(notif.Title, 5*time.Second)
		}

	case protocol.EventPresence:
		var pe protocol.PresenceEvent
		if err := json.Unmarshal(wireMsg.Data, &pe); err == nil {
			for i := range m.ChatView.SidebarItems {
				item := &m.ChatView.SidebarItems[i]
				if item.IsDM && item.ID == pe.UserID {
					item.Presence = pe.Presence
				}
			}
		}

	case protocol.EventRoomExpired:
		var r models.Room
		if err := json.Unmarshal(wireMsg.Data, &r); err == nil {
			m.ChatView.SetToast(fmt.Sprintf("Room #%s has expired", r.Name), 6*time.Second)
			var filtered []views.SidebarItem
			for _, it := range m.ChatView.SidebarItems {
				if it.ID != r.ID {
					filtered = append(filtered, it)
				}
			}
			m.ChatView.SidebarItems = filtered
			if m.ChatView.ActiveItem.ID == r.ID {
				m.switchChannel(m.ChatView.SidebarItems[0])
			}
		}
	}
}

func (m AppModel) handleAuthSubmit(msg views.AuthSubmitMsg) tea.Cmd {
	client := m.Client
	return func() tea.Msg {
		if msg.Mode == views.ModeRegister {
			resp, err := client.Register(protocol.RegisterReq{
				Username:    msg.Username,
				DisplayName: msg.DisplayName,
				Password:    msg.Password,
				UserColor:   msg.UserColor,
				Status:      msg.Status,
			})
			if err != nil {
				return AuthErrMsg{Err: err.Error()}
			}
			return AuthSuccessMsg{Resp: resp}
		} else {
			resp, err := client.Login(protocol.LoginReq{
				Username: msg.Username,
				Password: msg.Password,
			})
			if err != nil {
				return AuthErrMsg{Err: err.Error()}
			}
			return AuthSuccessMsg{Resp: resp}
		}
	}
}

func (m *AppModel) onAuthSuccess(resp *protocol.AuthResp) {
	m.Screen = ScreenChat
	m.ChatView.CurrentUser = &resp.User

	// Build initial sidebar items
	items := []views.SidebarItem{
		{
			ID:       database.GlobalRoomID,
			Name:     "global",
			Category: views.CategoryChats,
			IsDM:     false,
		},
	}

	for _, ch := range resp.Channels {
		if ch.ID == database.GlobalRoomID {
			continue
		}
		cat := views.CategoryRooms
		if ch.IsPrivate {
			cat = views.CategoryChats
		}
		items = append(items, views.SidebarItem{
			ID:       ch.ID,
			Name:     ch.Name,
			Category: cat,
			IsDM:     false,
		})
	}

	m.ChatView.SidebarItems = items
	m.switchChannel(items[0])
}

func (m *AppModel) switchChannel(item views.SidebarItem) {
	m.ChatView.ActiveItem = item
	for i := range m.ChatView.SidebarItems {
		if m.ChatView.SidebarItems[i].ID == item.ID && m.ChatView.SidebarItems[i].IsDM == item.IsDM {
			m.ChatView.SidebarItems[i].UnreadCount = 0
			m.ChatView.SidebarIndex = i
			break
		}
	}
}

func (m AppModel) fetchHistory(item views.SidebarItem) tea.Cmd {
	client := m.Client
	return func() tea.Msg {
		var targetType string
		var roomID *string
		var otherID *string

		if item.IsDM {
			targetType = models.TargetDM
			otherID = &item.ID
		} else {
			targetType = models.TargetRoom
			roomID = &item.ID
		}

		messages, err := client.GetHistory(targetType, roomID, otherID, 100)
		if err != nil {
			return nil
		}
		return ChannelHistoryMsg{
			TargetType: targetType,
			TargetID:   item.ID,
			Messages:   messages,
		}
	}
}

func (m *AppModel) startDM(target models.UserProfile) views.SidebarItem {
	for _, item := range m.ChatView.SidebarItems {
		if item.IsDM && item.ID == target.ID {
			m.switchChannel(item)
			return item
		}
	}

	newItem := views.SidebarItem{
		ID:         target.ID,
		Name:       target.Username,
		Category:   views.CategoryDMs,
		IsDM:       true,
		TargetUser: &target,
		Presence:   target.Presence,
	}

	m.ChatView.SidebarItems = append(m.ChatView.SidebarItems, newItem)
	m.switchChannel(newItem)
	return newItem
}

func (m AppModel) handleJoinRoom(msg views.JoinRoomRequestMsg) tea.Cmd {
	client := m.Client
	return func() tea.Msg {
		room, err := client.JoinRoom(msg.RoomName, msg.Password)
		if err != nil {
			return JoinRoomErrMsg{Err: err.Error()}
		}
		return JoinRoomSuccessMsg{Room: room}
	}
}

func (m AppModel) handleCreateRoom(msg views.CreateRoomRequestMsg) tea.Cmd {
	client := m.Client
	return func() tea.Msg {
		room, err := client.CreateRoom(protocol.CreateRoomReq{
			Name:        msg.Name,
			Description: msg.Description,
			IsPrivate:   msg.IsPrivate,
			Password:    msg.Password,
			Duration:    msg.Duration,
		})
		if err != nil {
			return CreateRoomErrMsg{Err: err.Error()}
		}
		return CreateRoomSuccessMsg{Room: room}
	}
}

func (m AppModel) handleUpdateProfile(msg views.UpdateProfileRequestMsg) tea.Cmd {
	client := m.Client
	return func() tea.Msg {
		prof, err := client.UpdateProfile(&msg.DisplayName, &msg.UserColor, &msg.CustomStatus, &msg.Presence)
		if err != nil {
			return UpdateProfileErrMsg{Err: err.Error()}
		}
		return UpdateProfileSuccessMsg{Profile: prof}
	}
}

func (m AppModel) handleTransferOwnership(msg views.TransferOwnershipConfirmMsg) tea.Cmd {
	client := m.Client
	return func() tea.Msg {
		err := client.TransferOwnership(msg.RoomID, msg.NewOwnerID)
		return TransferOwnershipResultMsg{Err: err}
	}
}

func (m *AppModel) sendChatMessage(content string) {
	item := m.ChatView.ActiveItem
	var targetType string
	var roomID *string
	var recipientID *string

	if item.IsDM {
		targetType = models.TargetDM
		recipientID = &item.ID
	} else {
		targetType = models.TargetRoom
		roomID = &item.ID
	}

	go func() {
		_, err := m.Client.SendMessage(targetType, roomID, recipientID, content)
		if err != nil {
			m.ChatView.SetToast("Error: "+err.Error(), 4*time.Second)
		}
	}()
}

func (m AppModel) executeCommand(cmdLine string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(cmdLine)
	if len(parts) == 0 {
		return m, nil
	}
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/help":
		m.Modals.OpenHelp()
		return m, nil

	case "/profile":
		if m.Client.User() != nil {
			m.Modals.OpenProfile(m.Client.User().ToProfile())
		}
		return m, nil

	case "/who":
		client := m.Client
		return m, func() tea.Msg {
			users, err := client.ListUsers()
			return OpenNewDMMsg{Users: users, Err: err}
		}

	case "/msg":
		if len(parts) < 3 {
			m.ChatView.SetToast("Usage: /msg <username> <message>", 3*time.Second)
			return m, nil
		}
		targetUsername := parts[1]
		msgText := strings.Join(parts[2:], " ")
		client := m.Client
		return m, func() tea.Msg {
			users, err := client.ListUsers()
			if err != nil {
				return ActionToastMsg{Text: "Failed to list users: " + err.Error()}
			}
			for _, u := range users {
				if strings.EqualFold(u.Username, targetUsername) {
					return ExecuteDMMsg{TargetUser: u, Content: msgText}
				}
			}
			return ActionToastMsg{Text: "User not found: " + targetUsername}
		}

	case "/room":
		if len(parts) < 2 || parts[1] == "list" {
			client := m.Client
			return m, func() tea.Msg {
				rooms, err := client.ListRooms(false)
				return OpenRoomBrowserMsg{Rooms: rooms, Err: err}
			}
		}
		subCmd := strings.ToLower(parts[1])
		switch subCmd {
		case "create":
			if len(parts) < 3 {
				m.Modals.OpenCreateRoom()
				return m, nil
			}
			roomName := parts[2]
			return m, m.handleCreateRoom(views.CreateRoomRequestMsg{Name: roomName})

		case "join":
			if len(parts) < 3 {
				m.ChatView.SetToast("Usage: /room join <name> [password]", 3*time.Second)
				return m, nil
			}
			roomName := parts[2]
			var pass *string
			if len(parts) >= 4 {
				pass = &parts[3]
			}
			return m, m.handleJoinRoom(views.JoinRoomRequestMsg{RoomName: roomName, Password: pass})

		case "leave":
			if !m.ChatView.ActiveItem.IsDM && m.ChatView.ActiveItem.ID != database.GlobalRoomID {
				roomID := m.ChatView.ActiveItem.ID
				client := m.Client
				return m, func() tea.Msg {
					err := client.LeaveRoom(roomID)
					return LeaveRoomResultMsg{RoomID: roomID, Err: err}
				}
			} else {
				m.ChatView.SetToast("Cannot leave global chat", 3*time.Second)
				return m, nil
			}
		}

	case "/color":
		if len(parts) < 2 {
			m.ChatView.SetToast("Usage: /color <Cyan|Blue|Green|Yellow|Magenta|Red|White>", 3*time.Second)
			return m, nil
		}
		newCol := parts[1]
		return m, m.handleUpdateProfile(views.UpdateProfileRequestMsg{UserColor: newCol})

	case "/status":
		if len(parts) < 2 {
			m.ChatView.SetToast("Usage: /status <your custom status>", 3*time.Second)
			return m, nil
		}
		newStatus := strings.Join(parts[1:], " ")
		return m, m.handleUpdateProfile(views.UpdateProfileRequestMsg{CustomStatus: newStatus})

	case "/kick":
		if len(parts) < 2 || m.ChatView.ActiveItem.IsDM {
			m.ChatView.SetToast("Usage: /kick <username> (inside a room)", 3*time.Second)
			return m, nil
		}
		targetName := parts[1]
		client := m.Client
		roomID := m.ChatView.ActiveItem.ID
		return m, func() tea.Msg {
			u, err := client.GetProfile(targetName, "")
			if err != nil {
				return ActionToastMsg{Text: "User not found: " + targetName}
			}
			err = client.KickMember(roomID, u.ID)
			if err != nil {
				return ActionToastMsg{Text: "Kick error: " + err.Error()}
			}
			return ActionToastMsg{Text: "Kicked @" + targetName}
		}

	case "/ban":
		if len(parts) < 2 || m.ChatView.ActiveItem.IsDM {
			m.ChatView.SetToast("Usage: /ban <username> [reason]", 3*time.Second)
			return m, nil
		}
		targetName := parts[1]
		reason := "Banned by admin"
		if len(parts) >= 3 {
			reason = strings.Join(parts[2:], " ")
		}
		client := m.Client
		roomID := m.ChatView.ActiveItem.ID
		return m, func() tea.Msg {
			u, err := client.GetProfile(targetName, "")
			if err != nil {
				return ActionToastMsg{Text: "User not found: " + targetName}
			}
			err = client.BanMember(roomID, u.ID, reason)
			if err != nil {
				return ActionToastMsg{Text: "Ban error: " + err.Error()}
			}
			return ActionToastMsg{Text: "Banned @" + targetName}
		}

	case "/mute":
		if len(parts) < 2 || m.ChatView.ActiveItem.IsDM {
			m.ChatView.SetToast("Usage: /mute <username> [minutes]", 3*time.Second)
			return m, nil
		}
		targetName := parts[1]
		dur := 15
		if len(parts) >= 3 {
			if d, err := strconv.Atoi(parts[2]); err == nil && d > 0 {
				dur = d
			}
		}
		client := m.Client
		roomID := m.ChatView.ActiveItem.ID
		return m, func() tea.Msg {
			u, err := client.GetProfile(targetName, "")
			if err != nil {
				return ActionToastMsg{Text: "User not found: " + targetName}
			}
			err = client.MuteMember(roomID, u.ID, dur)
			if err != nil {
				return ActionToastMsg{Text: "Mute error: " + err.Error()}
			}
			return ActionToastMsg{Text: fmt.Sprintf("Muted @%s for %d min", targetName, dur)}
		}

	case "/transfer":
		if len(parts) < 2 || m.ChatView.ActiveItem.IsDM {
			m.ChatView.SetToast("Usage: /transfer <username>", 3*time.Second)
			return m, nil
		}
		targetName := parts[1]
		client := m.Client
		roomID := m.ChatView.ActiveItem.ID
		roomName := m.ChatView.ActiveItem.Name
		return m, func() tea.Msg {
			u, err := client.GetProfile(targetName, "")
			if err != nil {
				return ActionToastMsg{Text: "User not found: " + targetName}
			}
			return OpenTransferConfirmMsg{
				RoomID:     roomID,
				RoomName:   roomName,
				TargetUser: *u,
			}
		}

	case "/quit":
		if m.Client.User() != nil {
			_ = m.Client.SetPresence(models.PresenceOffline, nil)
		}
		m.Client.Close()
		return m, tea.Quit

	default:
		m.ChatView.SetToast("Unknown command: "+cmd+" (try /help)", 3*time.Second)
		return m, nil
	}
	return m, nil
}

func (m AppModel) View() string {
	if m.Modals.ActiveType != views.ModalNone {
		return m.Modals.View()
	}

	if m.Screen == ScreenAuth {
		return m.AuthView.View()
	}

	return m.ChatView.View()
}
