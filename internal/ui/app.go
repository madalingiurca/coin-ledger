// Package ui is the terminal interface. It is the only package that imports
// Bubble Tea; the budget engine underneath it runs and tests without a
// terminal.
package ui

import (
	"fmt"
	"strings"

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
	rename  *renameState
	confirm *confirmState
	menu    *menuState

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
		if m.rename != nil {
			m.rename.restyle(m.theme)
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
	case m.rename != nil:
		return m.renameKey(msg)
	case m.cat != nil:
		return m.catKey(msg)
	case m.confirm != nil:
		return m.confirmKey(key)
	case m.menu != nil:
		return m.menuKey(msg)
	}

	// Any other keystroke retires the last status message.
	m.status, m.isError = "", false

	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.menu = &menuState{}
		return m, nil
	case "1", "2", "3", "4":
		return m.jump(key)
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

// jump goes to the screen whose pane carries that number. Going to the screen
// already open does nothing, so a half-done import is not thrown away.
func (m Model) jump(key string) (tea.Model, tea.Cmd) {
	want := map[string]screen{"1": screenMonth, "2": screenHistory, "3": screenPayees, "4": screenImport}[key]
	if want == m.screen {
		return m, nil
	}
	switch want {
	case screenMonth:
		m.screen = screenMonth
	case screenHistory:
		m.openHistory()
	case screenPayees:
		m.openPayees()
	case screenImport:
		m.openImport()
	}
	return m, nil
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
	mainW := cw + paneChrome
	// One line at the bottom is the key bar; the panes fill the rest.
	h := max(3, m.height-1)

	var block string
	hints := m.barHints()
	switch {
	case m.screen == screenHistory:
		block = pane{title: "[2] History", counter: counter(m.hist.cursor, len(m.hist.rows)),
			body: m.historyView(cw), w: mainW, h: h, focused: true}.render(t)
	case m.screen == screenImport:
		block = pane{title: m.importTitle(), counter: m.importCounter(),
			body: m.importView(cw, h-2), w: mainW, h: h, focused: true}.render(t)
	case m.screen == screenPayees:
		block = pane{title: m.payeesTitle(), counter: m.payeesCounter(),
			body: m.payeesView(cw, h-2), w: mainW, h: h, focused: !m.dialogOpen()}.render(t)
	default:
		block = m.monthScreen(cw, h)
	}
	if m.dialogOpen() {
		block, hints = m.withPopup(block, cw, h), nil
	}

	blockW := lipgloss.Width(block)
	bar := m.bottomLine(blockW, hints)
	view := block + "\n" + bar
	if pad := (m.width - blockW) / 2; pad > 0 {
		indent := strings.Repeat(" ", pad)
		lines := strings.Split(view, "\n")
		for i := range lines {
			lines[i] = indent + lines[i]
		}
		view = strings.Join(lines, "\n")
	}
	return view
}

// sideMin and sideMax bound the totals pane beside the month list, and
// sideFrom is the narrowest terminal that gets one; below it the totals sit
// under the list.
const (
	sideMin  = 30
	sideMax  = 44
	sideFrom = 100
)

// monthScreen lays out the month: the list as the focused pane, and the
// totals beside it on a wide terminal and under it on a narrow one.
func (m Model) monthScreen(cw, h int) string {
	t := m.theme
	if m.dash.month == nil {
		body, _ := m.monthList(cw, 0)
		return pane{title: m.monthTitle(), body: body, w: cw + paneChrome, h: h, focused: true}.render(t)
	}
	if m.width >= sideFrom {
		sideW := min(sideMax, max(sideMin, m.width-cw-paneChrome))
		listW := min(cw+paneChrome, m.width-sideW)
		body, count := m.monthList(listW-paneChrome, h-2)
		list := pane{title: m.monthTitle(), counter: count, body: body,
			w: listW, h: h, focused: !m.dialogOpen()}.render(t)
		totals := pane{title: "Totals", body: m.monthTotals(sideW-paneChrome, true), w: sideW}.render(t)
		return lipgloss.JoinHorizontal(lipgloss.Top, list, totals)
	}
	totals := pane{title: "Left", body: m.monthTotals(cw, false), w: cw + paneChrome}.render(t)
	listH := max(3, h-lipgloss.Height(totals))
	body, count := m.monthList(cw, listH-2)
	list := pane{title: m.monthTitle(), counter: count, body: body,
		w: cw + paneChrome, h: listH, focused: !m.dialogOpen()}.render(t)
	return list + "\n" + totals
}

// dialogOpen reports whether a dialog has the keyboard.
func (m Model) dialogOpen() bool {
	return m.form != nil || m.rename != nil || m.cat != nil || m.confirm != nil || m.menu != nil
}

// popupMax caps a dialog's content width; a form stretched across a wide
// terminal is harder to read, not easier.
const popupMax = 56

// withPopup draws any open dialog as a focused pane over the middle of the
// screen, which stays in view behind it.
func (m Model) withPopup(screen string, cw, h int) string {
	t := m.theme
	pcw := min(cw, popupMax)
	p := pane{w: pcw + paneChrome, focused: true}
	switch {
	case m.form != nil:
		p.title, p.body, p.hints = m.form.title(), m.form.view(t, pcw), m.form.hints()
	case m.rename != nil:
		p.title, p.body, p.hints = "Rename "+m.rename.from, m.rename.view(t, pcw), "enter save · esc cancel"
	case m.cat != nil:
		p.title, p.body, p.hints = m.cat.title, m.cat.view(t, pcw), m.cat.hints()
	case m.confirm != nil:
		p.title, p.body, p.hints = "Delete", m.confirmPrompt(pcw), "y delete · n keep"
	case m.menu != nil:
		name, _ := m.screenBindings()
		p.title, p.body, p.hints = "Keybindings: "+name, m.menuView(pcw, max(1, h-4)), "enter run · esc close"
	default:
		return screen
	}
	popup := p.render(t)

	w := max(lipgloss.Width(screen), lipgloss.Width(popup))
	h = max(lipgloss.Height(screen), lipgloss.Height(popup))
	x := max(0, (lipgloss.Width(screen)-lipgloss.Width(popup))/2)
	y := max(0, (lipgloss.Height(screen)-lipgloss.Height(popup))/2)
	return lipgloss.NewCanvas(w, h).Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(screen),
		lipgloss.NewLayer(popup).X(x).Y(y).Z(1),
	)).Render()
}

// bottomLine is the last line of the screen: the latest status, else the
// keys for what is on screen. A dialog carries its own keys.
func (m Model) bottomLine(w int, hints []hint) string {
	switch {
	case m.status != "":
		style := m.theme.Notice
		if m.isError {
			style = m.theme.Danger
		}
		return style.Render(truncate(m.status, w))
	}
	return keybar(m.theme, w, hints...)
}

func (m *Model) setStatus(format string, args ...any) {
	m.status, m.isError = fmt.Sprintf(format, args...), false
}

func (m *Model) setError(err error) {
	m.status, m.isError = err.Error(), true
}
