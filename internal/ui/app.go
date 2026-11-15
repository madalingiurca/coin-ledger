// Package ui is the terminal interface. It is the only package that imports
// Bubble Tea; the budget engine underneath it runs and tests without a
// terminal.
package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/store"
)

type screen int

const (
	screenMonth screen = iota
	screenHistory
	screenPayees
	screenImport
)

// Model is the whole application state.
type Model struct {
	store *store.Store
	theme Theme
	// rules are the categories and payee rules. rulesErr is set when the
	// categories file exists but cannot be read; rules then holds the defaults
	// in memory and nothing is ever saved over the damaged file.
	rules    *model.Rules
	rulesErr error
	// collapsed is shared with every dash so folded categories stay folded.
	collapsed map[string]bool

	width, height int
	ready         bool

	screen  screen
	dash    dash
	hist    hist
	payees  payees
	imp     *importState
	form    *formState
	cat     *catState
	confirm *confirmState
	help    bool

	status  string
	isError bool
}

// New builds the app opened on the current month.
func New(s *store.Store) Model {
	m := Model{store: s, theme: newTheme(true), collapsed: map[string]bool{}}
	m.rules, m.rulesErr = s.LoadRules()
	if m.rulesErr != nil {
		m.rules = model.NewRules()
	}
	m.openMonth(model.Today())
	if m.rulesErr != nil {
		m.setError(m.rulesErr)
	}
	return m
}

func (m Model) Init() tea.Cmd { return tea.RequestBackgroundColor }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.theme = newTheme(msg.IsDark())
		if m.form != nil {
			m.form.restyle(m.theme)
		}
		if m.cat != nil {
			m.cat.restyle(m.theme)
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		return m, nil

	case tea.KeyPressMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func (m Model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	// Overlays get first refusal, innermost first.
	switch {
	case m.form != nil:
		return m.formKey(msg)
	case m.cat != nil:
		return m.catKey(msg)
	case m.confirm != nil:
		return m.confirmKey(key)
	case m.help:
		m.help = false
		return m, nil
	}

	// Any other keystroke retires the last status message.
	m.status, m.isError = "", false

	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.help = true
		return m, nil
	}

	switch m.screen {
	case screenHistory:
		return m.historyKey(key)
	case screenPayees:
		return m.payeesKey(key)
	case screenImport:
		return m.importKey(key)
	}
	return m.monthKey(key)
}

func (m Model) View() tea.View {
	body := m.render()
	v := tea.NewView(body)
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if !m.ready {
		return ""
	}
	t := m.theme
	cw := contentWidth(m.width)

	var frames []string
	switch {
	case m.help:
		frames = []string{t.Box.Width(cw + boxChrome).Render(m.helpView(cw))}
	case m.screen == screenHistory:
		frames = []string{
			t.Box.Width(cw + boxChrome).Render(m.historyView(cw)),
			footer(t, cw+boxChrome, m.historyHints()...),
		}
	case m.screen == screenImport:
		below := footer(t, cw+boxChrome, m.importHints()...)
		frames = []string{
			t.Box.Width(cw + boxChrome).Render(m.importView(cw, m.roomAbove(below))),
			below,
		}
	case m.screen == screenPayees:
		var below string
		if m.cat != nil {
			below = t.Form.Width(cw + boxChrome).Render(m.cat.view(t, cw))
		} else {
			below = footer(t, cw+boxChrome, m.payeesHints()...)
		}
		frames = []string{
			t.Box.Width(cw + boxChrome).Render(m.payeesView(cw, m.roomAbove(below))),
			below,
		}
	default:
		var below string
		switch {
		case m.form != nil:
			below = t.Form.Width(cw + boxChrome).Render(m.form.view(t, cw))
		case m.cat != nil:
			below = t.Form.Width(cw + boxChrome).Render(m.cat.view(t, cw))
		case m.confirm != nil:
			below = m.confirmPrompt(cw)
		default:
			below = footer(t, cw+boxChrome, m.monthHints()...)
		}
		frames = []string{
			t.Box.Width(cw + boxChrome).Render(m.monthView(cw, m.roomAbove(below))),
			below,
		}
	}

	if line := m.statusLine(cw); line != "" {
		frames = append(frames, line)
	}

	view := lipgloss.JoinVertical(lipgloss.Left, frames...)
	if lipgloss.Height(view) >= m.height || lipgloss.Width(view) >= m.width {
		return view // too tall or wide to centre; show it plainly
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, view)
}

// roomAbove is how many lines of content the main frame can hold once the
// frame beneath it, the status line and its own border and padding are paid
// for.
func (m Model) roomAbove(below string) int {
	const frame = 4 // top and bottom border, top and bottom padding
	room := m.height - frame - lipgloss.Height(below) - 1
	if m.status != "" {
		room--
	}
	return max(0, room)
}

func (m Model) statusLine(cw int) string {
	if m.status == "" {
		return ""
	}
	style := m.theme.Notice
	if m.isError {
		style = m.theme.Danger
	}
	return style.Render(truncate(m.status, cw+4))
}

func (m *Model) setStatus(format string, args ...any) {
	m.status, m.isError = fmt.Sprintf(format, args...), false
}

func (m *Model) setError(err error) {
	m.status, m.isError = err.Error(), true
}
