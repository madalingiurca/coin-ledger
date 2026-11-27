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
	var hints []hint
	switch {
	case m.help:
		block = pane{title: "Keys", body: m.helpView(cw), w: mainW, h: h, focused: true}.render(t)
		hints = []hint{keyHint("any key", "back")}
	case m.screen == screenHistory:
		block = pane{title: "[2] History", counter: counter(m.hist.cursor, len(m.hist.rows)),
			body: m.historyView(cw), w: mainW, h: h, focused: true}.render(t)
		hints = m.historyHints()
	case m.screen == screenImport:
		block = pane{title: m.importTitle(), counter: m.importCounter(),
			body: m.importView(cw, h-2), w: mainW, h: h, focused: true}.render(t)
		hints = m.importHints()
	case m.screen == screenPayees:
		block, hints = m.stackDialog(cw, h, func(cw, h int) string {
			return pane{title: m.payeesTitle(), counter: m.payeesCounter(),
				body: m.payeesView(cw, h-2), w: cw + paneChrome, h: h, focused: m.cat == nil}.render(t)
		}), m.payeesHints()
	default:
		block, hints = m.monthScreen(cw, h), m.monthHints()
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

// monthScreen lays out the month: the list as the focused pane, the totals
// beside it on a wide terminal and under it on a narrow one, and an open
// dialog beneath both.
func (m Model) monthScreen(cw, h int) string {
	t := m.theme
	if m.dash.month == nil {
		body, _ := m.monthList(cw, 0)
		return pane{title: m.monthTitle(), body: body, w: cw + paneChrome, h: h, focused: true}.render(t)
	}
	if m.width >= sideFrom {
		sideW := min(sideMax, max(sideMin, m.width-cw-paneChrome))
		listW := min(cw+paneChrome, m.width-sideW)
		return m.stackDialog(listW-paneChrome, h, func(lcw, h int) string {
			body, count := m.monthList(lcw, h-2)
			list := pane{title: m.monthTitle(), counter: count, body: body,
				w: lcw + paneChrome, h: h, focused: !m.dialogOpen()}.render(t)
			totals := pane{title: "Totals", body: m.monthTotals(sideW-paneChrome, true), w: sideW}.render(t)
			return lipgloss.JoinHorizontal(lipgloss.Top, list, totals)
		})
	}
	return m.stackDialog(cw, h, func(cw, h int) string {
		totals := pane{title: "Left", body: m.monthTotals(cw, false), w: cw + paneChrome}.render(t)
		listH := max(3, h-lipgloss.Height(totals))
		body, count := m.monthList(cw, listH-2)
		list := pane{title: m.monthTitle(), counter: count, body: body,
			w: cw + paneChrome, h: listH, focused: !m.dialogOpen()}.render(t)
		return list + "\n" + totals
	})
}

// dialogOpen reports whether a dialog has the keyboard.
func (m Model) dialogOpen() bool { return m.form != nil || m.rename != nil || m.cat != nil }

// stackDialog draws a screen with any open dialog in a focused pane beneath
// it, giving the screen whatever height the dialog leaves.
func (m Model) stackDialog(cw, h int, screen func(cw, h int) string) string {
	t := m.theme
	var title, body string
	switch {
	case m.form != nil:
		title, body = m.form.title(), m.form.view(t, cw)
	case m.rename != nil:
		title, body = "Rename "+m.rename.from, m.rename.view(t, cw)
	case m.cat != nil:
		title, body = m.cat.title, m.cat.view(t, cw)
	default:
		return screen(cw, h)
	}
	dialog := pane{title: title, body: body, w: cw + paneChrome, focused: true}.render(t)
	return screen(cw, max(3, h-lipgloss.Height(dialog))) + "\n" + dialog
}

// bottomLine is the last line of the screen: a delete to confirm, else the
// latest status, else the keys for what is on screen.
func (m Model) bottomLine(w int, hints []hint) string {
	switch {
	case m.confirm != nil:
		return m.confirmPrompt(w)
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
