package styles

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Base palette
var (
	ColorBgDark     = lipgloss.Color("#111318")
	ColorBgPanel    = lipgloss.Color("#181b22")
	ColorBgActive   = lipgloss.Color("#222734")
	ColorBorder     = lipgloss.Color("#2e3444")
	ColorBorderFocus= lipgloss.Color("#6366f1")
	ColorText       = lipgloss.Color("#e2e8f0")
	ColorTextMuted  = lipgloss.Color("#64748b")
	ColorTextDim    = lipgloss.Color("#475569")
	ColorAccent     = lipgloss.Color("#06b6d4")
	ColorGreen      = lipgloss.Color("#10b981")
	ColorYellow     = lipgloss.Color("#f59e0b")
	ColorRed        = lipgloss.Color("#f43f5e")
	ColorPurple     = lipgloss.Color("#a855f7")
)

// User display color mapping
func GetUserColor(name string) lipgloss.TerminalColor {
	switch strings.ToLower(name) {
	case "cyan":
		return lipgloss.Color("#06b6d4")
	case "blue":
		return lipgloss.Color("#3b82f6")
	case "green":
		return lipgloss.Color("#10b981")
	case "yellow":
		return lipgloss.Color("#f59e0b")
	case "magenta", "purple":
		return lipgloss.Color("#d946ef")
	case "red":
		return lipgloss.Color("#f43f5e")
	case "white":
		return lipgloss.Color("#f8fafc")
	default:
		return lipgloss.Color("#06b6d4")
	}
}

// Presence colors
func GetPresenceColor(presence string) lipgloss.TerminalColor {
	switch strings.ToLower(presence) {
	case "online":
		return ColorGreen
	case "idle":
		return ColorYellow
	case "away":
		return ColorRed
	default:
		return ColorTextDim
	}
}

func PresenceSymbol(presence string) string {
	switch strings.ToLower(presence) {
	case "online":
		return "●"
	case "idle":
		return "◐"
	case "away":
		return "○"
	default:
		return "○"
	}
}

// Styles
var (
	// App Header
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorAccent).
			Padding(0, 1)

	ConnConnected = lipgloss.NewStyle().
			Foreground(ColorGreen).
			Bold(true)

	ConnReconnecting = lipgloss.NewStyle().
				Foreground(ColorYellow).
				Bold(true)

	ConnDisconnected = lipgloss.NewStyle().
				Foreground(ColorRed).
				Bold(true)

	// Layout Panes
	SidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(0, 1)

	ChatPaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(0, 1)

	InputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(0, 1)

	InputBoxActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorBorderFocus).
				Padding(0, 1)

	// Sidebar items
	SectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorTextMuted).
				MarginTop(1)

	ChannelItemStyle = lipgloss.NewStyle().
				Foreground(ColorText).
				PaddingLeft(1)

	ChannelItemActiveStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorAccent).
				Background(ColorBgActive).
				PaddingLeft(1)

	UnreadDotStyle = lipgloss.NewStyle().
			Foreground(ColorGreen).
			Bold(true)

	UnreadTextStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff"))

	// Messages
	TimestampStyle = lipgloss.NewStyle().
			Foreground(ColorTextDim)

	AuthorStyle = lipgloss.NewStyle().
			Bold(true)

	SystemMsgStyle = lipgloss.NewStyle().
			Foreground(ColorTextMuted).
			Italic(true)

	MentionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#4f46e5")).
			Padding(0, 1)

	// Footer status bar
	FooterStyle = lipgloss.NewStyle().
			Foreground(ColorTextMuted).
			Padding(0, 1)

	KeyBadgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorAccent)

	// Modals & Dialogs
	ModalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorderFocus).
			Padding(1, 2).
			Background(ColorBgPanel)

	ModalTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorAccent).
			MarginBottom(1)

	ButtonActiveStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ffffff")).
				Background(ColorBorderFocus).
				Padding(0, 2)

	ButtonInactiveStyle = lipgloss.NewStyle().
				Foreground(ColorTextMuted).
				Background(ColorBgActive).
				Padding(0, 2)

	ToastStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorAccent).
			Background(ColorBgActive).
			Padding(0, 1).
			Bold(true)
)
