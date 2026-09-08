package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rtoms/chattui/internal/auth"
	"github.com/rtoms/chattui/internal/tui/styles"
)

type AuthMode int

const (
	ModeRegister AuthMode = iota
	ModeLogin
)

type AuthSubmitMsg struct {
	Mode        AuthMode
	Username    string
	DisplayName string
	Password    string
	UserColor   string
	Status      string
}

type AuthModel struct {
	Mode         AuthMode
	FocusIndex   int
	Inputs       []textinput.Model
	ColorIndex   int
	ErrorMessage string
	IsSubmitting bool
	Width        int
	Height       int
}

func NewAuthModel() AuthModel {
	m := AuthModel{
		Mode:         ModeRegister,
		FocusIndex:   0,
		Inputs:       make([]textinput.Model, 4), // 0: User, 1: Disp, 2: Pass, 3: Status
		ColorIndex:   0,
		IsSubmitting: false,
	}

	// 0: Username
	m.Inputs[0] = textinput.New()
	m.Inputs[0].Placeholder = "rtoms"
	m.Inputs[0].CharLimit = 20
	m.Inputs[0].Focus()

	// 1: Display Name
	m.Inputs[1] = textinput.New()
	m.Inputs[1].Placeholder = "Rtoms"
	m.Inputs[1].CharLimit = 30

	// 2: Password
	m.Inputs[2] = textinput.New()
	m.Inputs[2].Placeholder = "••••••••"
	m.Inputs[2].EchoMode = textinput.EchoPassword
	m.Inputs[2].EchoCharacter = '•'
	m.Inputs[2].CharLimit = 64

	// 3: Custom Status
	m.Inputs[3] = textinput.New()
	m.Inputs[3].Placeholder = "coding rn..."
	m.Inputs[3].CharLimit = 50

	return m
}

func (m AuthModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *AuthModel) SetError(err string) {
	m.ErrorMessage = err
	m.IsSubmitting = false
}

func (m *AuthModel) SwitchMode(mode AuthMode) {
	m.Mode = mode
	m.FocusIndex = 0
	m.ErrorMessage = ""
	m.IsSubmitting = false
	for i := range m.Inputs {
		m.Inputs[i].Blur()
	}
	m.Inputs[0].Focus()
}

func (m AuthModel) Update(msg tea.Msg) (AuthModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down":
			maxIndex := 6 // 0:user, 1:disp, 2:pass, 3:status, 4:color, 5:submit, 6:toggle
			if m.Mode == ModeLogin {
				maxIndex = 3 // 0:user, 1:pass, 2:submit, 3:toggle
			}
			m.FocusIndex++
			if m.FocusIndex > maxIndex {
				m.FocusIndex = 0
			}
			return m, m.updateFocus()

		case "shift+tab", "up":
			maxIndex := 6
			if m.Mode == ModeLogin {
				maxIndex = 3
			}
			m.FocusIndex--
			if m.FocusIndex < 0 {
				m.FocusIndex = maxIndex
			}
			return m, m.updateFocus()

		case "left":
			if m.Mode == ModeRegister && m.FocusIndex == 4 { // Color cycler
				m.ColorIndex--
				if m.ColorIndex < 0 {
					m.ColorIndex = len(auth.AllowedColors) - 1
				}
				return m, nil
			}

		case "right":
			if m.Mode == ModeRegister && m.FocusIndex == 4 { // Color cycler
				m.ColorIndex++
				if m.ColorIndex >= len(auth.AllowedColors) {
					m.ColorIndex = 0
				}
				return m, nil
			}

		case "enter":
			if m.IsSubmitting {
				return m, nil
			}

			// Handle Toggle link
			if (m.Mode == ModeRegister && m.FocusIndex == 6) || (m.Mode == ModeLogin && m.FocusIndex == 3) {
				if m.Mode == ModeRegister {
					m.SwitchMode(ModeLogin)
				} else {
					m.SwitchMode(ModeRegister)
				}
				return m, nil
			}

			// Check if should submit
			shouldSubmit := false
			if m.Mode == ModeRegister {
				// Submit button is 5, or pressing enter in color (4), or pressing enter in password (2) if filled
				if m.FocusIndex == 5 || m.FocusIndex == 4 {
					shouldSubmit = true
				} else if m.FocusIndex == 2 && m.Inputs[0].Value() != "" && m.Inputs[2].Value() != "" {
					shouldSubmit = true
				}
			} else {
				// In Login mode: 1 is password, 2 is submit button
				if m.FocusIndex == 2 || (m.FocusIndex == 1 && m.Inputs[0].Value() != "" && m.Inputs[2].Value() != "") {
					shouldSubmit = true
				}
			}

			if shouldSubmit {
				m.IsSubmitting = true
				m.ErrorMessage = ""
				color := auth.AllowedColors[m.ColorIndex]
				if m.Mode == ModeRegister {
					disp := strings.TrimSpace(m.Inputs[1].Value())
					if disp == "" {
						disp = strings.TrimSpace(m.Inputs[0].Value())
					}
					return m, func() tea.Msg {
						return AuthSubmitMsg{
							Mode:        ModeRegister,
							Username:    strings.TrimSpace(m.Inputs[0].Value()),
							DisplayName: disp,
							Password:    m.Inputs[2].Value(),
							Status:      strings.TrimSpace(m.Inputs[3].Value()),
							UserColor:   color,
						}
					}
				} else {
					return m, func() tea.Msg {
						return AuthSubmitMsg{
							Mode:     ModeLogin,
							Username: strings.TrimSpace(m.Inputs[0].Value()),
							Password: m.Inputs[2].Value(),
						}
					}
				}
			} else {
				// Advance to next field
				m.FocusIndex++
				maxIndex := 6
				if m.Mode == ModeLogin {
					maxIndex = 3
				}
				if m.FocusIndex > maxIndex {
					m.FocusIndex = 0
				}
				return m, m.updateFocus()
			}
		}
	}

	// Forward key to active textinput
	var cmd tea.Cmd
	if m.Mode == ModeRegister {
		if m.FocusIndex >= 0 && m.FocusIndex <= 3 {
			m.Inputs[m.FocusIndex], cmd = m.Inputs[m.FocusIndex].Update(msg)
		}
	} else {
		if m.FocusIndex == 0 {
			m.Inputs[0], cmd = m.Inputs[0].Update(msg)
		} else if m.FocusIndex == 1 {
			m.Inputs[2], cmd = m.Inputs[2].Update(msg)
		}
	}

	return m, cmd
}

func (m *AuthModel) updateFocus() tea.Cmd {
	cmds := make([]tea.Cmd, len(m.Inputs))
	for i := range m.Inputs {
		m.Inputs[i].Blur()
	}

	if m.Mode == ModeRegister {
		if m.FocusIndex >= 0 && m.FocusIndex <= 3 {
			cmds[m.FocusIndex] = m.Inputs[m.FocusIndex].Focus()
		}
	} else {
		if m.FocusIndex == 0 {
			cmds[0] = m.Inputs[0].Focus()
		} else if m.FocusIndex == 1 {
			cmds[2] = m.Inputs[2].Focus()
		}
	}
	return tea.Batch(cmds...)
}

func (m AuthModel) View() string {
	var b strings.Builder

	title := styles.HeaderStyle.Render("chatTUI")
	subTitle := ""
	if m.Mode == ModeRegister {
		subTitle = lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render("Create your account")
	} else {
		subTitle = lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render("Sign in to your account")
	}

	b.WriteString(lipgloss.NewStyle().Width(44).Align(lipgloss.Center).Render(title))
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Width(44).Align(lipgloss.Center).Render(subTitle))
	b.WriteString("\n\n")

	labelStyle := lipgloss.NewStyle().Width(14).Foreground(styles.ColorTextMuted)

	if m.Mode == ModeRegister {
		// 0: Username
		b.WriteString(labelStyle.Render("Username:"))
		b.WriteString(m.Inputs[0].View())
		b.WriteString("\n\n")

		// 1: Display Name
		b.WriteString(labelStyle.Render("Display name:"))
		b.WriteString(m.Inputs[1].View())
		b.WriteString("\n\n")

		// 2: Password
		b.WriteString(labelStyle.Render("Password:"))
		b.WriteString(m.Inputs[2].View())
		b.WriteString("\n\n")

		// 3: Custom status
		b.WriteString(labelStyle.Render("Custom status:"))
		b.WriteString(m.Inputs[3].View())
		b.WriteString("\n\n")

		// 4: Color selector
		currColor := auth.AllowedColors[m.ColorIndex]
		colorStyle := lipgloss.NewStyle().Foreground(styles.GetUserColor(currColor)).Bold(true)
		colorDisplay := fmt.Sprintf("< %s >", colorStyle.Render(currColor))
		if m.FocusIndex == 4 {
			colorDisplay = styles.ButtonActiveStyle.Render(colorDisplay)
		}
		b.WriteString(labelStyle.Render("User color:"))
		b.WriteString(colorDisplay + "  " + lipgloss.NewStyle().Foreground(styles.ColorTextDim).Render("(← / →)"))
		b.WriteString("\n\n")

		// 5: Submit button
		btn := "[ Create Account ]"
		if m.IsSubmitting {
			btn = styles.ButtonActiveStyle.Render("[ Creating account... ]")
		} else if m.FocusIndex == 5 {
			btn = styles.ButtonActiveStyle.Render(btn)
		} else {
			btn = styles.ButtonInactiveStyle.Render(btn)
		}
		b.WriteString(lipgloss.NewStyle().Width(44).Align(lipgloss.Center).Render(btn))
		b.WriteString("\n\n")

		// 6: Switch mode link
		switchLink := "Already have an account? Sign In"
		if m.FocusIndex == 6 {
			switchLink = styles.ButtonActiveStyle.Render("[ " + switchLink + " ]")
		} else {
			switchLink = lipgloss.NewStyle().Foreground(styles.ColorTextMuted).Render(switchLink)
		}
		b.WriteString(lipgloss.NewStyle().Width(44).Align(lipgloss.Center).Render(switchLink))

	} else {
		// Login mode
		// 0: Username
		b.WriteString(labelStyle.Render("Username:"))
		b.WriteString(m.Inputs[0].View())
		b.WriteString("\n\n")

		// 1: Password
		b.WriteString(labelStyle.Render("Password:"))
		b.WriteString(m.Inputs[2].View())
		b.WriteString("\n\n")

		// 2: Submit button
		btn := "[ Sign In ]"
		if m.IsSubmitting {
			btn = styles.ButtonActiveStyle.Render("[ Signing in... ]")
		} else if m.FocusIndex == 2 {
			btn = styles.ButtonActiveStyle.Render(btn)
		} else {
			btn = styles.ButtonInactiveStyle.Render(btn)
		}
		b.WriteString(lipgloss.NewStyle().Width(44).Align(lipgloss.Center).Render(btn))
		b.WriteString("\n\n")

		// 3: Switch mode link
		switchLink := "Need an account? Create one"
		if m.FocusIndex == 3 {
			switchLink = styles.ButtonActiveStyle.Render("[ " + switchLink + " ]")
		} else {
			switchLink = lipgloss.NewStyle().Foreground(styles.ColorTextMuted).Render(switchLink)
		}
		b.WriteString(lipgloss.NewStyle().Width(44).Align(lipgloss.Center).Render(switchLink))
	}

	if m.ErrorMessage != "" {
		b.WriteString("\n\n")
		errBox := lipgloss.NewStyle().Foreground(styles.ColorRed).Bold(true).Render("✕ " + m.ErrorMessage)
		b.WriteString(lipgloss.NewStyle().Width(44).Align(lipgloss.Center).Render(errBox))
	}

	box := styles.ModalStyle.Width(48).Render(b.String())
	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, box)
}
