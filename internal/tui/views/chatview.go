package views

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rtoms/chattui/internal/client"
	"github.com/rtoms/chattui/internal/database"
	"github.com/rtoms/chattui/internal/models"
	"github.com/rtoms/chattui/internal/tui/styles"
)

type SidebarCategory int

const (
	CategoryChats SidebarCategory = iota
	CategoryDMs
	CategoryRooms
)

type SidebarItem struct {
	ID          string
	Name        string
	Category    SidebarCategory
	IsDM        bool
	TargetUser  *models.UserProfile
	UnreadCount int
	Presence    string
}

type ChannelSelectedMsg struct {
	Item SidebarItem
}

type ChatViewModel struct {
	Viewport        viewport.Model
	Input           textinput.Model
	ActiveItem      SidebarItem
	SidebarItems    []SidebarItem
	SidebarIndex    int
	SidebarFocused  bool
	AutoScroll      bool
	Messages        []models.Message
	CurrentUser     *models.User
	ConnState       client.ConnState
	ToastMessage    string
	ToastExpiry     time.Time
	Width           int
	Height          int
	mentionRegex    *regexp.Regexp
}

func NewChatViewModel() ChatViewModel {
	vp := viewport.New(60, 20)
	vp.SetContent("")

	ti := textinput.New()
	ti.Placeholder = "Type a message or /help..."
	ti.Prompt = "> "
	ti.CharLimit = 4000
	ti.Focus()

	defaultItem := SidebarItem{
		ID:       database.GlobalRoomID,
		Name:     "global",
		Category: CategoryChats,
		IsDM:     false,
	}

	return ChatViewModel{
		Viewport:       vp,
		Input:          ti,
		ActiveItem:     defaultItem,
		SidebarItems:   []SidebarItem{defaultItem},
		SidebarIndex:   0,
		SidebarFocused: false,
		AutoScroll:     true,
		mentionRegex:   regexp.MustCompile(`@([a-zA-Z0-9_-]+)`),
	}
}

func (m *ChatViewModel) SetSize(width, height int) {
	m.Width = width
	m.Height = height

	sidebarWidth := 26
	chatWidth := width - sidebarWidth - 4
	if chatWidth < 20 {
		chatWidth = 20
	}

	chatHeight := height - 7
	if chatHeight < 5 {
		chatHeight = 5
	}

	m.Viewport.Width = chatWidth
	m.Viewport.Height = chatHeight
	m.Input.Width = chatWidth + 2

	m.refreshViewportContent()
}

func (m *ChatViewModel) SetToast(msg string, dur time.Duration) {
	m.ToastMessage = msg
	m.ToastExpiry = time.Now().Add(dur)
}

func (m *ChatViewModel) SetMessages(messages []models.Message) {
	m.Messages = messages
	m.refreshViewportContent()
	if m.AutoScroll {
		m.Viewport.GotoBottom()
	}
}

func (m *ChatViewModel) AddMessage(msg models.Message) {
	m.Messages = append(m.Messages, msg)
	m.refreshViewportContent()
	if m.AutoScroll {
		m.Viewport.GotoBottom()
	}
}

func (m *ChatViewModel) refreshViewportContent() {
	var b strings.Builder

	currUsername := ""
	if m.CurrentUser != nil {
		currUsername = strings.ToLower(m.CurrentUser.Username)
	}

	var lastSenderID string
	var lastTime time.Time
	lastWasSystem := true

	for _, msg := range m.Messages {
		timeStr := styles.TimestampStyle.Render(msg.CreatedAt.Local().Format("15:04"))

		if msg.IsSystem {
			// System message: e.g. "10:34  ● alex joined #coding"
			line := fmt.Sprintf("%s  %s", timeStr, styles.SystemMsgStyle.Render(msg.Content))
			b.WriteString(line + "\n")
			lastSenderID = ""
			lastWasSystem = true
			continue
		}

		// Highlight mentions
		content := msg.Content
		content = m.mentionRegex.ReplaceAllStringFunc(content, func(match string) string {
			uname := strings.TrimPrefix(match, "@")
			if strings.EqualFold(uname, currUsername) {
				return styles.MentionStyle.Render(match)
			}
			return lipgloss.NewStyle().Foreground(styles.ColorAccent).Bold(true).Render(match)
		})

		// Message grouping check:
		// Group consecutive messages from the same sender within 5 minutes without an intervening system message
		isGrouped := !lastWasSystem &&
			msg.SenderID != "" &&
			msg.SenderID == lastSenderID &&
			msg.CreatedAt.Sub(lastTime) < 5*time.Minute

		if isGrouped {
			// Grouped: omit author name and header, just indent message content
			indented := "       " + content
			b.WriteString(indented + "\n")
		} else {
			// New group header: print timestamp and author display name
			if lastSenderID != "" || !lastWasSystem {
				b.WriteString("\n") // Spacing between different authors
			}
			authorColor := styles.GetUserColor(msg.SenderColor)
			author := lipgloss.NewStyle().Foreground(authorColor).Bold(true).Render(msg.SenderUsername)
			b.WriteString(fmt.Sprintf("%s  %s\n", timeStr, author))

			indented := "       " + content
			b.WriteString(indented + "\n")

			lastSenderID = msg.SenderID
			lastTime = msg.CreatedAt
			lastWasSystem = false
		}
	}

	m.Viewport.SetContent(b.String())
}

func (m ChatViewModel) Update(msg tea.Msg) (ChatViewModel, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "pgup":
			m.AutoScroll = false
			m.Viewport.HalfViewUp()
			return m, nil

		case "pgdown":
			m.Viewport.HalfViewDown()
			if m.Viewport.AtBottom() {
				m.AutoScroll = true
			}
			return m, nil

		case "end":
			m.AutoScroll = true
			m.Viewport.GotoBottom()
			return m, nil

		case "home":
			m.AutoScroll = false
			m.Viewport.GotoTop()
			return m, nil

		// Quick channel switching shortcuts
		case "alt+up", "ctrl+up":
			if m.SidebarIndex > 0 {
				m.SidebarIndex--
				target := m.SidebarItems[m.SidebarIndex]
				m.ActiveItem = target
				m.SidebarItems[m.SidebarIndex].UnreadCount = 0
				return m, func() tea.Msg { return ChannelSelectedMsg{Item: target} }
			}
			return m, nil

		case "alt+down", "ctrl+down":
			if m.SidebarIndex < len(m.SidebarItems)-1 {
				m.SidebarIndex++
				target := m.SidebarItems[m.SidebarIndex]
				m.ActiveItem = target
				m.SidebarItems[m.SidebarIndex].UnreadCount = 0
				return m, func() tea.Msg { return ChannelSelectedMsg{Item: target} }
			}
			return m, nil

		// Toggle sidebar focus with Esc or Tab (when input is empty)
		case "esc":
			if m.SidebarFocused {
				m.SidebarFocused = false
				m.Input.Focus()
			} else {
				m.SidebarFocused = true
				m.Input.Blur()
			}
			return m, nil

		case "tab":
			if m.Input.Value() == "" {
				m.SidebarFocused = !m.SidebarFocused
				if m.SidebarFocused {
					m.Input.Blur()
				} else {
					m.Input.Focus()
				}
				return m, nil
			}
		}

		// When sidebar is focused, Up/Down and Enter navigate channels
		if m.SidebarFocused {
			switch msg.String() {
			case "up", "k":
				if m.SidebarIndex > 0 {
					m.SidebarIndex--
				}
				return m, nil
			case "down", "j":
				if m.SidebarIndex < len(m.SidebarItems)-1 {
					m.SidebarIndex++
				}
				return m, nil
			case "enter":
				if m.SidebarIndex >= 0 && m.SidebarIndex < len(m.SidebarItems) {
					target := m.SidebarItems[m.SidebarIndex]
					m.ActiveItem = target
					m.SidebarItems[m.SidebarIndex].UnreadCount = 0
					m.SidebarFocused = false
					m.Input.Focus()
					return m, func() tea.Msg { return ChannelSelectedMsg{Item: target} }
				}
				return m, nil
			}
		}
	}

	// Update text input when not sidebar-focused
	if !m.SidebarFocused {
		m.Input, cmd = m.Input.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Check if viewport scrolled to bottom to resume auto-scroll
	if m.Viewport.AtBottom() {
		m.AutoScroll = true
	}

	return m, tea.Batch(cmds...)
}

func (m ChatViewModel) View() string {
	sidebarWidth := 26
	chatWidth := m.Width - sidebarWidth - 4
	if chatWidth < 20 {
		chatWidth = 20
	}

	// Top Bar
	topBar := m.renderTopBar()

	// Sidebar
	sidebar := m.renderSidebar(sidebarWidth)

	// Chat Pane
	chatPane := m.renderChatPane(chatWidth)

	// Middle Panes Side-by-Side
	panes := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, chatPane)

	// Bottom input box
	inputBorder := styles.InputBoxActiveStyle
	if m.SidebarFocused {
		inputBorder = styles.InputBoxStyle
	}
	inputView := inputBorder.Width(m.Width - 4).Render(m.Input.View())

	// Footer bar
	footer := m.renderFooter()

	fullView := lipgloss.JoinVertical(lipgloss.Left, topBar, panes, inputView, footer)
	return fullView
}

func (m ChatViewModel) renderTopBar() string {
	title := styles.HeaderStyle.Render("chatTUI")

	// Connection indicator
	var connStatus string
	switch m.ConnState {
	case client.StateConnected:
		connStatus = styles.ConnConnected.Render(m.ConnState.Indicator())
	case client.StateReconnecting:
		connStatus = styles.ConnReconnecting.Render(m.ConnState.Indicator())
	default:
		connStatus = styles.ConnDisconnected.Render(m.ConnState.Indicator())
	}

	// User status badge
	userBadge := ""
	if m.CurrentUser != nil {
		presSym := styles.PresenceSymbol(m.CurrentUser.Presence)
		presCol := styles.GetPresenceColor(m.CurrentUser.Presence)
		sym := lipgloss.NewStyle().Foreground(presCol).Render(presSym)
		userCol := styles.GetUserColor(m.CurrentUser.UserColor)
		name := lipgloss.NewStyle().Foreground(userCol).Bold(true).Render(m.CurrentUser.DisplayName)
		userBadge = fmt.Sprintf("%s %s", sym, name)
	}

	// Toast banner if active
	toast := ""
	if m.ToastMessage != "" && time.Now().Before(m.ToastExpiry) {
		toast = styles.ToastStyle.Render("🔔 " + m.ToastMessage)
	}

	left := title
	right := lipgloss.JoinHorizontal(lipgloss.Center, toast, "  ", userBadge, "  ", connStatus)

	availSpace := m.Width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if availSpace < 0 {
		availSpace = 0
	}
	spacer := strings.Repeat(" ", availSpace)

	return lipgloss.JoinHorizontal(lipgloss.Center, left, spacer, right) + "\n"
}

func (m ChatViewModel) renderSidebar(width int) string {
	var b strings.Builder
	paneHeight := m.Height - 7
	if paneHeight < 5 {
		paneHeight = 5
	}

	// Section 1: CHATS
	b.WriteString(styles.SectionHeaderStyle.Render("CHATS") + "\n")
	for i, item := range m.SidebarItems {
		if item.Category != CategoryChats {
			continue
		}
		b.WriteString(m.renderSidebarItem(i, item, width-4) + "\n")
	}

	// Section 2: DIRECT MESSAGES
	b.WriteString(styles.SectionHeaderStyle.Render("DIRECT MESSAGES") + "\n")
	hasDMs := false
	for i, item := range m.SidebarItems {
		if item.Category != CategoryDMs {
			continue
		}
		hasDMs = true
		b.WriteString(m.renderSidebarItem(i, item, width-4) + "\n")
	}
	if !hasDMs {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorTextDim).PaddingLeft(2).Render("(Ctrl+N for DM)\n"))
	}

	// Section 3: ROOMS
	b.WriteString(styles.SectionHeaderStyle.Render("ROOMS") + "\n")
	hasRooms := false
	for i, item := range m.SidebarItems {
		if item.Category != CategoryRooms {
			continue
		}
		hasRooms = true
		b.WriteString(m.renderSidebarItem(i, item, width-4) + "\n")
	}
	if !hasRooms {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorTextDim).PaddingLeft(2).Render("(Ctrl+R to browse)\n"))
	}

	borderStyle := styles.SidebarStyle
	if m.SidebarFocused {
		borderStyle = borderStyle.BorderForeground(styles.ColorBorderFocus)
	}

	return borderStyle.Width(width).Height(paneHeight).Render(b.String())
}

func (m ChatViewModel) renderSidebarItem(idx int, item SidebarItem, maxLen int) string {
	isActive := (item.ID == m.ActiveItem.ID && item.IsDM == m.ActiveItem.IsDM)
	isCursor := (idx == m.SidebarIndex && m.SidebarFocused)

	unreadPrefix := "  "
	if item.UnreadCount > 0 {
		unreadPrefix = styles.UnreadDotStyle.Render("● ")
	}

	prefix := "# "
	if item.IsDM {
		prefix = "@"
	}

	displayName := prefix + item.Name
	if len(displayName) > maxLen-4 {
		displayName = displayName[:maxLen-5] + "…"
	}

	var renderedName string
	if item.UnreadCount > 0 {
		renderedName = styles.UnreadTextStyle.Render(displayName)
	} else if isCursor {
		renderedName = styles.ButtonActiveStyle.Render("> " + displayName)
	} else if isActive {
		renderedName = styles.ChannelItemActiveStyle.Render(displayName)
	} else {
		renderedName = styles.ChannelItemStyle.Render(displayName)
	}

	return unreadPrefix + renderedName
}

func (m ChatViewModel) renderChatPane(width int) string {
	paneHeight := m.Height - 7
	if paneHeight < 5 {
		paneHeight = 5
	}

	// Header
	headerName := "# " + m.ActiveItem.Name
	if m.ActiveItem.IsDM {
		headerName = "@ " + m.ActiveItem.Name
	}
	header := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render(headerName)
	divider := lipgloss.NewStyle().Foreground(styles.ColorBorder).Render(strings.Repeat("─", width))

	content := lipgloss.JoinVertical(lipgloss.Left, header, divider, m.Viewport.View())
	return styles.ChatPaneStyle.Width(width).Height(paneHeight).Render(content)
}

func (m ChatViewModel) renderFooter() string {
	kStyle := styles.KeyBadgeStyle
	txtStyle := styles.FooterStyle

	focusHint := "Tab: Focus Sidebar"
	if m.SidebarFocused {
		focusHint = "Enter: Select | Esc: Input"
	}

	items := []string{
		kStyle.Render("Ctrl+K") + txtStyle.Render(" Search"),
		kStyle.Render("Ctrl+N") + txtStyle.Render(" DM"),
		kStyle.Render("Ctrl+R") + txtStyle.Render(" Room"),
		kStyle.Render("Ctrl+P") + txtStyle.Render(" Profile"),
		kStyle.Render("Alt+↑/↓") + txtStyle.Render(" Switch"),
		kStyle.Render("[ " + focusHint + " ]"),
		kStyle.Render("Ctrl+Q") + txtStyle.Render(" Quit"),
	}

	return strings.Join(items, "   ")
}
