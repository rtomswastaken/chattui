package views

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rtoms/chattui/internal/auth"
	"github.com/rtoms/chattui/internal/models"
	"github.com/rtoms/chattui/internal/tui/styles"
)

type ModalType int

const (
	ModalNone ModalType = iota
	ModalProfile
	ModalRoomBrowser
	ModalCreateRoom
	ModalPasswordPrompt
	ModalNewDM
	ModalCommandPalette
	ModalHelp
	ModalTransferConfirm
)

// Messages emitted by modals
type CloseModalMsg struct{}

type JoinRoomRequestMsg struct {
	RoomName string
	Password *string
}

type CreateRoomRequestMsg struct {
	Name        string
	Description string
	IsPrivate   bool
	Password    *string
	Duration    *int
}

type StartDMRequestMsg struct {
	TargetUser models.UserProfile
}

type UpdateProfileRequestMsg struct {
	DisplayName  string
	CustomStatus string
	UserColor    string
	Presence     string
}

type TransferOwnershipConfirmMsg struct {
	RoomID     string
	NewOwnerID string
}

type ModalState struct {
	ActiveType ModalType

	// Profile modal
	ProfileUser    models.UserProfile
	ProfileEdit    bool
	ProfileInputs  []textinput.Model
	ProfileColorIdx int
	ProfilePresIdx int

	// Room browser
	RoomsList       []models.Room
	RoomSelectIdx   int
	PendingJoinRoom string

	// Create room
	CreateInputs   []textinput.Model
	CreatePrivate  bool
	CreateFocusIdx int

	// Password prompt
	PasswordInput textinput.Model

	// DM Picker
	UsersList    []models.UserProfile
	UserFilter   textinput.Model
	UserSelectIdx int

	// Command Palette
	PaletteInput     textinput.Model
	PaletteSelectIdx int

	// Transfer confirmation
	TransferRoomID   string
	TransferRoomName string
	TransferUser     models.UserProfile
	TransferConfirm  bool

	Width  int
	Height int
}

func NewModalState() ModalState {
	s := ModalState{
		ActiveType:    ModalNone,
		ProfileInputs: make([]textinput.Model, 2),
		CreateInputs:  make([]textinput.Model, 4),
	}

	// Profile inputs: 0: display name, 1: custom status
	s.ProfileInputs[0] = textinput.New()
	s.ProfileInputs[0].Placeholder = "Display name"
	s.ProfileInputs[1] = textinput.New()
	s.ProfileInputs[1].Placeholder = "Custom status..."

	// Create room: 0: name, 1: description, 2: password, 3: duration hours
	s.CreateInputs[0] = textinput.New()
	s.CreateInputs[0].Placeholder = "channel-name"
	s.CreateInputs[1] = textinput.New()
	s.CreateInputs[1].Placeholder = "Channel description"
	s.CreateInputs[2] = textinput.New()
	s.CreateInputs[2].Placeholder = "Optional password"
	s.CreateInputs[2].EchoMode = textinput.EchoPassword
	s.CreateInputs[3] = textinput.New()
	s.CreateInputs[3].Placeholder = "Lifetime in hours (e.g. 6) or blank"

	s.PasswordInput = textinput.New()
	s.PasswordInput.Placeholder = "Password"
	s.PasswordInput.EchoMode = textinput.EchoPassword

	s.UserFilter = textinput.New()
	s.UserFilter.Placeholder = "Search username..."

	s.PaletteInput = textinput.New()
	s.PaletteInput.Placeholder = "Type a command or search..."

	return s
}

func (s *ModalState) OpenProfile(u models.UserProfile) {
	s.ActiveType = ModalProfile
	s.ProfileUser = u
	s.ProfileEdit = false
	s.ProfileInputs[0].SetValue(u.DisplayName)
	s.ProfileInputs[1].SetValue(u.CustomStatus)

	// match color index
	s.ProfileColorIdx = 0
	for i, c := range auth.AllowedColors {
		if strings.EqualFold(c, u.UserColor) {
			s.ProfileColorIdx = i
			break
		}
	}
}

func (s *ModalState) OpenRoomBrowser(rooms []models.Room) {
	s.ActiveType = ModalRoomBrowser
	s.RoomsList = rooms
	s.RoomSelectIdx = 0
}

func (s *ModalState) OpenCreateRoom() {
	s.ActiveType = ModalCreateRoom
	for i := range s.CreateInputs {
		s.CreateInputs[i].Reset()
		s.CreateInputs[i].Blur()
	}
	s.CreateInputs[0].Focus()
	s.CreatePrivate = false
	s.CreateFocusIdx = 0
}

func (s *ModalState) OpenPasswordPrompt(roomName string) {
	s.ActiveType = ModalPasswordPrompt
	s.PendingJoinRoom = roomName
	s.PasswordInput.Reset()
	s.PasswordInput.Focus()
}

func (s *ModalState) OpenNewDM(users []models.UserProfile) {
	s.ActiveType = ModalNewDM
	s.UsersList = users
	s.UserFilter.Reset()
	s.UserFilter.Focus()
	s.UserSelectIdx = 0
}

func (s *ModalState) OpenCommandPalette() {
	s.ActiveType = ModalCommandPalette
	s.PaletteInput.Reset()
	s.PaletteInput.Focus()
	s.PaletteSelectIdx = 0
}

func (s *ModalState) OpenHelp() {
	s.ActiveType = ModalHelp
}

func (s *ModalState) OpenTransferConfirm(roomID, roomName string, target models.UserProfile) {
	s.ActiveType = ModalTransferConfirm
	s.TransferRoomID = roomID
	s.TransferRoomName = roomName
	s.TransferUser = target
	s.TransferConfirm = false
}

func (s *ModalState) Close() {
	s.ActiveType = ModalNone
}

func (s ModalState) Update(msg tea.Msg) (ModalState, tea.Cmd) {
	if s.ActiveType == ModalNone {
		return s, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "esc" {
			s.Close()
			return s, func() tea.Msg { return CloseModalMsg{} }
		}

		switch s.ActiveType {
		case ModalProfile:
			return s.updateProfile(msg)
		case ModalRoomBrowser:
			return s.updateRoomBrowser(msg)
		case ModalCreateRoom:
			return s.updateCreateRoom(msg)
		case ModalPasswordPrompt:
			return s.updatePasswordPrompt(msg)
		case ModalNewDM:
			return s.updateNewDM(msg)
		case ModalCommandPalette:
			return s.updatePalette(msg)
		case ModalHelp:
			if msg.String() == "enter" || msg.String() == "q" {
				s.Close()
				return s, func() tea.Msg { return CloseModalMsg{} }
			}
		case ModalTransferConfirm:
			return s.updateTransfer(msg)
		}
	}

	return s, nil
}

func (s ModalState) updateProfile(msg tea.KeyMsg) (ModalState, tea.Cmd) {
	presences := []string{models.PresenceOnline, models.PresenceIdle, models.PresenceAway}

	if !s.ProfileEdit {
		if msg.String() == "e" || msg.String() == "enter" {
			s.ProfileEdit = true
			s.ProfileInputs[0].Focus()
			return s, nil
		}
		return s, nil
	}

	switch msg.String() {
	case "tab":
		if s.ProfileInputs[0].Focused() {
			s.ProfileInputs[0].Blur()
			s.ProfileInputs[1].Focus()
		} else if s.ProfileInputs[1].Focused() {
			s.ProfileInputs[1].Blur()
		} else {
			s.ProfileInputs[0].Focus()
		}
		return s, nil

	case "left":
		if !s.ProfileInputs[0].Focused() && !s.ProfileInputs[1].Focused() {
			s.ProfileColorIdx--
			if s.ProfileColorIdx < 0 {
				s.ProfileColorIdx = len(auth.AllowedColors) - 1
			}
			return s, nil
		}

	case "right":
		if !s.ProfileInputs[0].Focused() && !s.ProfileInputs[1].Focused() {
			s.ProfileColorIdx++
			if s.ProfileColorIdx >= len(auth.AllowedColors) {
				s.ProfileColorIdx = 0
			}
			return s, nil
		}

	case "p":
		if !s.ProfileInputs[0].Focused() && !s.ProfileInputs[1].Focused() {
			s.ProfilePresIdx = (s.ProfilePresIdx + 1) % len(presences)
			return s, nil
		}

	case "enter":
		// Save profile
		newName := strings.TrimSpace(s.ProfileInputs[0].Value())
		newStatus := strings.TrimSpace(s.ProfileInputs[1].Value())
		newColor := auth.AllowedColors[s.ProfileColorIdx]
		newPres := presences[s.ProfilePresIdx]

		s.Close()
		return s, func() tea.Msg {
			return UpdateProfileRequestMsg{
				DisplayName:  newName,
				CustomStatus: newStatus,
				UserColor:    newColor,
				Presence:     newPres,
			}
		}
	}

	var cmd tea.Cmd
	if s.ProfileInputs[0].Focused() {
		s.ProfileInputs[0], cmd = s.ProfileInputs[0].Update(msg)
	} else if s.ProfileInputs[1].Focused() {
		s.ProfileInputs[1], cmd = s.ProfileInputs[1].Update(msg)
	}
	return s, cmd
}

func (s ModalState) updateRoomBrowser(msg tea.KeyMsg) (ModalState, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if s.RoomSelectIdx > 0 {
			s.RoomSelectIdx--
		}
	case "down", "j":
		if s.RoomSelectIdx < len(s.RoomsList)-1 {
			s.RoomSelectIdx++
		}
	case "c":
		s.OpenCreateRoom()
		return s, nil
	case "enter":
		if len(s.RoomsList) > 0 {
			room := s.RoomsList[s.RoomSelectIdx]
			if room.HasPassword {
				s.OpenPasswordPrompt(room.Name)
				return s, nil
			}
			s.Close()
			return s, func() tea.Msg {
				return JoinRoomRequestMsg{RoomName: room.Name}
			}
		}
	}
	return s, nil
}

func (s ModalState) updateCreateRoom(msg tea.KeyMsg) (ModalState, tea.Cmd) {
	switch msg.String() {
	case "tab", "down":
		s.CreateFocusIdx = (s.CreateFocusIdx + 1) % 7
		return s, s.updateCreateInputsFocus()

	case "shift+tab", "up":
		s.CreateFocusIdx = (s.CreateFocusIdx + 6) % 7
		return s, s.updateCreateInputsFocus()

	case " ":
		if s.CreateFocusIdx == 4 {
			s.CreatePrivate = !s.CreatePrivate
			return s, nil
		}

	case "enter":
		if s.CreateFocusIdx < 4 {
			// Advance to next field
			s.CreateFocusIdx++
			return s, s.updateCreateInputsFocus()
		} else if s.CreateFocusIdx == 4 {
			s.CreatePrivate = !s.CreatePrivate
			return s, nil
		} else if s.CreateFocusIdx == 5 || (s.CreateFocusIdx < 4 && s.CreateInputs[0].Value() != "") {
			name := strings.TrimSpace(s.CreateInputs[0].Value())
			if name == "" {
				s.CreateFocusIdx = 0
				return s, s.updateCreateInputsFocus()
			}
			desc := strings.TrimSpace(s.CreateInputs[1].Value())
			pass := strings.TrimSpace(s.CreateInputs[2].Value())
			var passPtr *string
			if pass != "" {
				passPtr = &pass
			}

			durStr := strings.TrimSpace(s.CreateInputs[3].Value())
			var durPtr *int
			if durStr != "" {
				if d, err := strconv.Atoi(durStr); err == nil && d > 0 {
					durPtr = &d
				}
			}

			s.Close()
			return s, func() tea.Msg {
				return CreateRoomRequestMsg{
					Name:        name,
					Description: desc,
					IsPrivate:   s.CreatePrivate,
					Password:    passPtr,
					Duration:    durPtr,
				}
			}
		} else if s.CreateFocusIdx == 6 {
			s.Close()
			return s, func() tea.Msg { return CloseModalMsg{} }
		}
	}

	if s.CreateFocusIdx >= 0 && s.CreateFocusIdx < 4 {
		var cmd tea.Cmd
		s.CreateInputs[s.CreateFocusIdx], cmd = s.CreateInputs[s.CreateFocusIdx].Update(msg)
		return s, cmd
	}

	return s, nil
}

func (s *ModalState) updateCreateInputsFocus() tea.Cmd {
	cmds := make([]tea.Cmd, len(s.CreateInputs))
	for i := range s.CreateInputs {
		if i == s.CreateFocusIdx {
			cmds[i] = s.CreateInputs[i].Focus()
		} else {
			s.CreateInputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}

func (s ModalState) updatePasswordPrompt(msg tea.KeyMsg) (ModalState, tea.Cmd) {
	if msg.String() == "enter" {
		pass := s.PasswordInput.Value()
		roomName := s.PendingJoinRoom
		s.Close()
		return s, func() tea.Msg {
			return JoinRoomRequestMsg{
				RoomName: roomName,
				Password: &pass,
			}
		}
	}
	var cmd tea.Cmd
	s.PasswordInput, cmd = s.PasswordInput.Update(msg)
	return s, cmd
}

func (s ModalState) updateNewDM(msg tea.KeyMsg) (ModalState, tea.Cmd) {
	filtered := s.getFilteredUsers()

	switch msg.String() {
	case "up":
		if s.UserSelectIdx > 0 {
			s.UserSelectIdx--
		}
	case "down":
		if s.UserSelectIdx < len(filtered)-1 {
			s.UserSelectIdx++
		}
	case "enter":
		if len(filtered) > 0 && s.UserSelectIdx < len(filtered) {
			target := filtered[s.UserSelectIdx]
			s.Close()
			return s, func() tea.Msg {
				return StartDMRequestMsg{TargetUser: target}
			}
		}
	default:
		var cmd tea.Cmd
		s.UserFilter, cmd = s.UserFilter.Update(msg)
		s.UserSelectIdx = 0
		return s, cmd
	}
	return s, nil
}

func (s ModalState) getFilteredUsers() []models.UserProfile {
	filter := strings.ToLower(strings.TrimSpace(s.UserFilter.Value()))
	if filter == "" {
		return s.UsersList
	}
	var res []models.UserProfile
	for _, u := range s.UsersList {
		if strings.Contains(strings.ToLower(u.Username), filter) || strings.Contains(strings.ToLower(u.DisplayName), filter) {
			res = append(res, u)
		}
	}
	return res
}

func (s ModalState) updatePalette(msg tea.KeyMsg) (ModalState, tea.Cmd) {
	if msg.String() == "enter" {
		val := strings.TrimSpace(s.PaletteInput.Value())
		s.Close()
		// If command, dispatch
		return s, func() tea.Msg {
			return val
		}
	}
	var cmd tea.Cmd
	s.PaletteInput, cmd = s.PaletteInput.Update(msg)
	return s, cmd
}

func (s ModalState) updateTransfer(msg tea.KeyMsg) (ModalState, tea.Cmd) {
	switch msg.String() {
	case "left", "right", "tab":
		s.TransferConfirm = !s.TransferConfirm
	case "enter":
		if s.TransferConfirm {
			roomID := s.TransferRoomID
			newOwner := s.TransferUser.ID
			s.Close()
			return s, func() tea.Msg {
				return TransferOwnershipConfirmMsg{
					RoomID:     roomID,
					NewOwnerID: newOwner,
				}
			}
		} else {
			s.Close()
			return s, func() tea.Msg { return CloseModalMsg{} }
		}
	}
	return s, nil
}

func (s ModalState) View() string {
	if s.ActiveType == ModalNone {
		return ""
	}

	var content string
	switch s.ActiveType {
	case ModalProfile:
		content = s.viewProfile()
	case ModalRoomBrowser:
		content = s.viewRoomBrowser()
	case ModalCreateRoom:
		content = s.viewCreateRoom()
	case ModalPasswordPrompt:
		content = s.viewPasswordPrompt()
	case ModalNewDM:
		content = s.viewNewDM()
	case ModalCommandPalette:
		content = s.viewPalette()
	case ModalHelp:
		content = s.viewHelp()
	case ModalTransferConfirm:
		content = s.viewTransferConfirm()
	}

	box := styles.ModalStyle.Render(content)
	return lipgloss.Place(s.Width, s.Height, lipgloss.Center, lipgloss.Center, box)
}

func (s ModalState) viewProfile() string {
	var b strings.Builder
	b.WriteString(styles.ModalTitleStyle.Render("User Profile"))
	b.WriteString("\n\n")

	u := s.ProfileUser
	nameColor := styles.GetUserColor(u.UserColor)
	b.WriteString(lipgloss.NewStyle().Foreground(nameColor).Bold(true).Render(u.DisplayName))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorTextMuted).Render("@" + u.Username))
	b.WriteString("\n\n")

	// Presence
	presSym := styles.PresenceSymbol(u.Presence)
	presCol := styles.GetPresenceColor(u.Presence)
	b.WriteString(fmt.Sprintf("%s %s\n", lipgloss.NewStyle().Foreground(presCol).Render(presSym), strings.Title(u.Presence)))
	if u.CustomStatus != "" {
		b.WriteString(lipgloss.NewStyle().Italic(true).Foreground(styles.ColorText).Render(u.CustomStatus))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	b.WriteString(fmt.Sprintf("Color: %s\n", lipgloss.NewStyle().Foreground(nameColor).Bold(true).Render(u.UserColor)))
	b.WriteString(fmt.Sprintf("Account created: %s\n", u.CreatedAt.Format("Jan 02, 2006")))
	b.WriteString(fmt.Sprintf("Last seen: %s\n\n", u.LastSeenAt.Format("15:04:05")))

	if s.ProfileEdit {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorAccent).Bold(true).Render("Editing Profile:\n"))
		b.WriteString("Display name:  " + s.ProfileInputs[0].View() + "\n")
		b.WriteString("Custom status: " + s.ProfileInputs[1].View() + "\n")
		currColor := auth.AllowedColors[s.ProfileColorIdx]
		b.WriteString(fmt.Sprintf("User color:    < %s > (Use ← / →)\n", lipgloss.NewStyle().Foreground(styles.GetUserColor(currColor)).Render(currColor)))
		b.WriteString("[ Press Enter to Save  |  Esc to Cancel ]")
	} else {
		btn := styles.ButtonActiveStyle.Render("[ Edit Profile (e) ]")
		b.WriteString(btn + "   " + lipgloss.NewStyle().Foreground(styles.ColorTextDim).Render("[ Esc to Close ]"))
	}

	return b.String()
}

func (s ModalState) viewRoomBrowser() string {
	var b strings.Builder
	b.WriteString(styles.ModalTitleStyle.Render("ROOM BROWSER"))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorTextMuted).Render("Browse and join public channels, or press 'c' to create one."))
	b.WriteString("\n\n")

	if len(s.RoomsList) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorTextDim).Render("No public rooms available."))
	} else {
		for i, r := range s.RoomsList {
			prefix := "  "
			if i == s.RoomSelectIdx {
				prefix = "> "
			}

			lock := ""
			if r.HasPassword {
				lock = " 🔒"
			}
			temp := ""
			if r.IsTemporary {
				temp = " ⏱ (temp)"
			}

			line := fmt.Sprintf("%-20s %2d members%s%s", "# "+r.Name, r.MemberCount, lock, temp)
			if i == s.RoomSelectIdx {
				b.WriteString(styles.ButtonActiveStyle.Render(prefix + line))
			} else {
				b.WriteString(styles.ChannelItemStyle.Render(prefix + line))
			}
			b.WriteString("\n")
			if r.Description != "" && i == s.RoomSelectIdx {
				b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorTextMuted).PaddingLeft(4).Render(r.Description) + "\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorTextDim).Render("[Enter: Join | c: Create Room | Esc: Close]"))
	return b.String()
}

func (s ModalState) viewCreateRoom() string {
	var b strings.Builder
	b.WriteString(styles.ModalTitleStyle.Render("CREATE A ROOM"))
	b.WriteString("\n\n")

	b.WriteString("Room Name:    " + s.CreateInputs[0].View() + "\n\n")
	b.WriteString("Description:  " + s.CreateInputs[1].View() + "\n\n")
	b.WriteString("Password:     " + s.CreateInputs[2].View() + "\n\n")
	b.WriteString("Duration (h): " + s.CreateInputs[3].View() + "\n\n")

	privText := "[ ] Public"
	if s.CreatePrivate {
		privText = "[✓] Private"
	}
	if s.CreateFocusIdx == 4 {
		privText = styles.ButtonActiveStyle.Render(privText)
	} else {
		privText = styles.ButtonInactiveStyle.Render(privText)
	}
	b.WriteString("Visibility:   " + privText + "  " + lipgloss.NewStyle().Foreground(styles.ColorTextDim).Render("(Space to toggle)") + "\n\n")

	createBtn := "[ Create Room ]"
	if s.CreateFocusIdx == 5 {
		createBtn = styles.ButtonActiveStyle.Render(createBtn)
	} else {
		createBtn = styles.ButtonInactiveStyle.Render(createBtn)
	}

	cancelBtn := "[ Cancel ]"
	if s.CreateFocusIdx == 6 {
		cancelBtn = styles.ButtonActiveStyle.Render(cancelBtn)
	} else {
		cancelBtn = styles.ButtonInactiveStyle.Render(cancelBtn)
	}

	b.WriteString(createBtn + "  " + cancelBtn + "  " + lipgloss.NewStyle().Foreground(styles.ColorTextDim).Render("[Tab / Enter to navigate]"))
	return b.String()
}

func (s ModalState) viewPasswordPrompt() string {
	var b strings.Builder
	b.WriteString(styles.ModalTitleStyle.Render("ENTER ROOM PASSWORD"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("Room #%s is password protected.\n\n", s.PendingJoinRoom))
	b.WriteString("Password: " + s.PasswordInput.View() + "\n\n")
	b.WriteString(styles.ButtonActiveStyle.Render("[ Join (Enter) ]") + "  " + lipgloss.NewStyle().Foreground(styles.ColorTextDim).Render("[Esc: Cancel]"))
	return b.String()
}

func (s ModalState) viewNewDM() string {
	var b strings.Builder
	b.WriteString(styles.ModalTitleStyle.Render("START DIRECT MESSAGE"))
	b.WriteString("\n\n")
	b.WriteString("Search: " + s.UserFilter.View() + "\n\n")

	filtered := s.getFilteredUsers()
	if len(filtered) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorTextDim).Render("No users found.\n"))
	} else {
		for i, u := range filtered {
			if i >= 10 {
				break
			}
			prefix := "  "
			if i == s.UserSelectIdx {
				prefix = "> "
			}

			presSym := styles.PresenceSymbol(u.Presence)
			presCol := styles.GetPresenceColor(u.Presence)
			symStyled := lipgloss.NewStyle().Foreground(presCol).Render(presSym)

			userCol := styles.GetUserColor(u.UserColor)
			line := fmt.Sprintf("%s @%-15s (%s)", symStyled, u.Username, u.DisplayName)

			if i == s.UserSelectIdx {
				b.WriteString(styles.ButtonActiveStyle.Render(prefix + line))
			} else {
				b.WriteString(lipgloss.NewStyle().Foreground(userCol).Render(prefix + line))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n" + lipgloss.NewStyle().Foreground(styles.ColorTextDim).Render("[Enter: Start DM | ↑/↓: Navigate | Esc: Cancel]"))
	return b.String()
}

func (s ModalState) viewPalette() string {
	var b strings.Builder
	b.WriteString(styles.ModalTitleStyle.Render("COMMAND PALETTE"))
	b.WriteString("\n\n")
	b.WriteString("> " + s.PaletteInput.View() + "\n\n")

	b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorTextMuted).Render(
		"Commands: /help, /who, /room list, /room create, /msg <user> <text>, /color <name>, /profile, /quit\n\n",
	))
	b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorTextDim).Render("[Enter: Execute | Esc: Close]"))
	return b.String()
}

func (s ModalState) viewHelp() string {
	var b strings.Builder
	b.WriteString(styles.ModalTitleStyle.Render("chatTUI COMMANDS & SHORTCUTS"))
	b.WriteString("\n\n")

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render("KEYBOARD SHORTCUTS:\n"))
	b.WriteString("  Ctrl+K      Open Command Palette\n")
	b.WriteString("  Ctrl+N      Start Direct Message (DM)\n")
	b.WriteString("  Ctrl+R      Open Room Browser / Create Room\n")
	b.WriteString("  Ctrl+P      View and Edit Your Profile\n")
	b.WriteString("  Ctrl+H      Show this Help Screen\n")
	b.WriteString("  Ctrl+Q      Clean Quit\n")
	b.WriteString("  ↑ / ↓       Navigate sidebar / Scroll chat\n")
	b.WriteString("  PageUp/Down Scroll messages\n")
	b.WriteString("  End         Jump to newest message\n")
	b.WriteString("  Esc         Close any modal or dialog\n\n")

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render("SLASH COMMANDS:\n"))
	b.WriteString("  /help                     Show help info\n")
	b.WriteString("  /who                      List all users and presence\n")
	b.WriteString("  /msg <user> <msg>         Send a direct message\n")
	b.WriteString("  /room list                Browse public rooms\n")
	b.WriteString("  /room create <name>       Create a new room\n")
	b.WriteString("  /room join <name> [pass]  Join a room\n")
	b.WriteString("  /room leave               Leave current room\n")
	b.WriteString("  /color <color>            Change user color\n")
	b.WriteString("  /status <text>            Set custom status\n")
	b.WriteString("  /profile                  Open profile screen\n")
	b.WriteString("  /kick <user>              (Mod+) Kick a user\n")
	b.WriteString("  /ban <user> [reason]      (Admin+) Ban a user\n")
	b.WriteString("  /mute <user> [minutes]    (Mod+) Mute a user\n")
	b.WriteString("  /transfer <user>          (Owner) Transfer room ownership\n")
	b.WriteString("  /quit                     Quit chatTUI\n\n")

	b.WriteString(styles.ButtonActiveStyle.Render("[ Close (Enter / Esc) ]"))
	return b.String()
}

func (s ModalState) viewTransferConfirm() string {
	var b strings.Builder
	b.WriteString(styles.ModalTitleStyle.Render("TRANSFER ROOM OWNERSHIP"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("Are you sure you want to transfer ownership of\n#%s to @%s?\n\n", s.TransferRoomName, s.TransferUser.Username))
	b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorYellow).Render("Warning: You will be demoted to Admin.\n\n"))

	cancelBtn := "[ Cancel ]"
	confirmBtn := "[ Confirm ]"

	if s.TransferConfirm {
		confirmBtn = styles.ButtonActiveStyle.Render(confirmBtn)
		cancelBtn = styles.ButtonInactiveStyle.Render(cancelBtn)
	} else {
		cancelBtn = styles.ButtonActiveStyle.Render(cancelBtn)
		confirmBtn = styles.ButtonInactiveStyle.Render(confirmBtn)
	}

	b.WriteString(cancelBtn + "    " + confirmBtn + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorTextDim).Render("[Use Tab / Arrows to select, Enter to confirm, Esc to abort]"))
	return b.String()
}
