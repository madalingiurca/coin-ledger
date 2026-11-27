package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// binding is one key a screen answers to. The key bar and the keybindings
// menu are both drawn from the same table, so they cannot drift apart.
type binding struct {
	// key is what the menu presses when this binding is run, written as
	// KeyPressMsg.String() reports it. Empty for a binding that only
	// explains, like moving the cursor.
	key string
	// display is how the key is written in the menu.
	display string
	action  string
	// bar is the action's short name in the key bar; empty leaves it out.
	bar string
	// barKey replaces display in the key bar, for a pair of keys shown as
	// one hint ("←/→").
	barKey string
}

func bind(key, display, action, bar string) binding {
	return binding{key: key, display: display, action: action, bar: bar}
}

// screenBindings are the keys of what is on screen now, with the name the
// menu gives them.
func (m Model) screenBindings() (string, []binding) {
	switch m.screen {
	case screenHistory:
		return "history", []binding{
			{display: "↑ ↓", action: "choose a month", bar: "select", barKey: "↑/↓"},
			bind("enter", "enter", "open the month", "open month"),
			bind("esc", "esc", "back to the month", "back"),
		}
	case screenPayees:
		if m.payees.detail != nil {
			return "payee", []binding{
				bind("enter", "enter", "categorise this transaction", "categorise this one"),
				{display: "↑ ↓", action: "move", bar: "move", barKey: "↑/↓"},
				bind("esc", "esc", "back to the payees", "back"),
			}
		}
		if len(m.payees.rows) == 0 {
			return "payees", []binding{bind("esc", "esc", "back to the month", "back")}
		}
		return "payees", []binding{
			bind("enter", "enter", "give the payee a category for good", "categorise all"),
			bind("right", "→", "file its transactions one by one", "one by one"),
			{display: "↑ ↓", action: "move", bar: "move", barKey: "↑/↓"},
			bind("esc", "esc", "back to the month", "back"),
		}
	case screenImport:
		switch m.imp.stage {
		case stageMatches:
			return "import", []binding{
				bind("space", "space", "replace / keep both / unrelated", "replace / keep both / unrelated"),
				bind("n", "n", "count the row for the month after its date", "next month"),
				{display: "↑ ↓", action: "move", bar: "move", barKey: "↑/↓"},
				bind("enter", "enter", "import", "import"),
				bind("esc", "esc", "back to the preview", "back"),
			}
		case stagePreview:
			if m.imp.newCount() == 0 {
				return "import", []binding{
					bind("esc", "esc", "back to the files", "back"),
					bind("enter", "enter", "close", "close"),
				}
			}
			return "import", []binding{
				bind("enter", "enter", "import", "import"),
				bind("esc", "esc", "back to the files", "back"),
			}
		}
		return "import", []binding{
			bind("enter", "enter", "open the folder or file", "open"),
			bind("left", "←", "up a folder", "up a folder"),
			bind("~", "~", "home folder", "home"),
			bind("esc", "esc", "cancel", "cancel"),
		}
	}

	months := []binding{
		{key: "left", display: "← p [", action: "previous month", bar: "month", barKey: "←/→"},
		bind("right", "→ n ]", "next month", ""),
		bind("t", "t", "this month", ""),
		bind("h", "h", "history of the last 24 months", "history"),
		bind("I", "I", "import a bank statement (ING or Revolut CSV)", "import"),
	}
	if m.dash.loadErr != nil {
		return "month", months
	}
	edit := []binding{{key: "e", display: "e enter", action: "edit the selected entry", bar: "edit", barKey: "e"}}
	if r, ok := m.dash.current(); ok && r.header {
		edit = []binding{
			bind("e", "e", "rename the category everywhere", "rename"),
			bind("enter", "enter space", "fold or unfold the category", ""),
		}
	}
	list := []binding{bind("a", "a", "add to the section the cursor is in", "add")}
	list = append(list, edit...)
	list = append(list,
		bind("d", "d", "delete the selected entry", "delete"),
		bind("r", "r", "carry the entry into future months", "recurring"),
		bind("c", "c", "put the selected expense in a category", "categorise"),
		bind("u", "u", "payees that have no category yet", "payees"),
		binding{key: ">", display: ">", action: "move the entry to the next month", bar: "move month", barKey: "</>"},
		bind("<", "<", "move the entry to the previous month", ""),
		bind("i", "i", "add income", ""),
		bind("x", "x", "add an expense", ""),
		bind("tab", "tab", "switch between income and expenses", "income/expenses"),
		binding{display: "↑ ↓ j k", action: "move the cursor"},
		bind("g", "g G", "first / last row", ""),
	)
	return "month", append(list, months...)
}

// globalBindings work on every screen.
func globalBindings() []binding {
	return []binding{
		bind("1", "1", "month", ""),
		bind("2", "2", "history", ""),
		bind("3", "3", "uncategorised payees", ""),
		bind("4", "4", "import", ""),
		bind("q", "q", "quit", "quit"),
		bind("?", "?", "this menu", "keys"),
	}
}

// barHints are the key bar for what is on screen: the screen's keys, then
// quit, then the way to all of them.
func (m Model) barHints() []hint {
	_, list := m.screenBindings()
	var out []hint
	for _, b := range append(list, globalBindings()...) {
		if b.bar == "" {
			continue
		}
		key := b.display
		if b.barKey != "" {
			key = b.barKey
		}
		out = append(out, keyHint(key, b.bar))
	}
	return out
}

// menuState is the keybindings menu: every key the screen answers to,
// narrowed by what is typed, and run with enter.
type menuState struct {
	filter string
	cursor int
}

// menuRow is one line of the menu: a heading, or a binding to run.
type menuRow struct {
	heading string
	b       binding
}

func (m Model) menuRows() []menuRow {
	name, list := m.screenBindings()
	want := strings.ToLower(m.menu.filter)
	keep := func(b binding) bool {
		return want == "" || strings.Contains(strings.ToLower(b.action), want) ||
			strings.Contains(strings.ToLower(b.display), want)
	}
	var rows []menuRow
	for _, group := range []struct {
		name string
		list []binding
	}{{capitalize(name), list}, {"Global", globalBindings()}} {
		var picked []menuRow
		for _, b := range group.list {
			if keep(b) {
				picked = append(picked, menuRow{b: b})
			}
		}
		if len(picked) > 0 {
			rows = append(rows, menuRow{heading: group.name})
			rows = append(rows, picked...)
		}
	}
	return rows
}

// menuChoices are the indexes of the rows the cursor can be on.
func (m Model) menuChoices() []int {
	var out []int
	for i, r := range m.menuRows() {
		if r.heading == "" {
			out = append(out, i)
		}
	}
	return out
}

func (m Model) menuKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	mn := m.menu
	choices := m.menuChoices()
	switch msg.String() {
	case "esc", "?":
		m.menu = nil
		return m, nil
	case "up", "ctrl+p":
		mn.cursor = max(0, mn.cursor-1)
		return m, nil
	case "down", "ctrl+n":
		mn.cursor = max(0, min(len(choices)-1, mn.cursor+1))
		return m, nil
	case "backspace":
		if r := []rune(mn.filter); len(r) > 0 {
			mn.filter = string(r[:len(r)-1])
			mn.cursor = 0
		}
		return m, nil
	case "enter":
		var b binding
		if mn.cursor < len(choices) {
			b = m.menuRows()[choices[mn.cursor]].b
		}
		m.menu = nil
		if b.key == "" {
			return m, nil
		}
		return m.onKey(keyPress(b.key))
	}
	if msg.Text != "" {
		mn.filter += msg.Text
		mn.cursor = 0
	}
	return m, nil
}

// menuView draws the menu for a popup cw wide and at most h lines tall.
func (m Model) menuView(cw, h int) string {
	t := m.theme
	filter := t.Muted.Render("type to filter")
	if m.menu.filter != "" {
		filter = t.FieldOn.Render(m.menu.filter) + t.Cursor.Render("▏")
	}
	head := []string{t.Field.Render("Filter  ") + filter, ""}

	rows := m.menuRows()
	choices := m.menuChoices()
	var body []string
	cursorLine := 0
	keyW := 12
	for i, r := range rows {
		if r.heading != "" {
			if len(body) > 0 {
				body = append(body, "")
			}
			body = append(body, t.Section.Render(r.heading))
			continue
		}
		line := padRight(truncate(r.b.display, keyW-1), keyW) + truncate(r.b.action, max(1, cw-keyW))
		if len(choices) > 0 && m.menu.cursor < len(choices) && choices[m.menu.cursor] == i {
			cursorLine = len(body)
			body = append(body, t.SelectedActive.Render(padRight(line, cw)))
			continue
		}
		body = append(body, t.Key.Render(padRight(truncate(r.b.display, keyW-1), keyW))+
			t.Footer.Render(truncate(r.b.action, max(1, cw-keyW))))
	}
	if len(rows) == 0 {
		body = append(body, t.Muted.Render("no key does that"))
	}

	notes := []string{"",
		wrap(t.Muted, cw, "Recurring entries (↻) are copied into each new month you open. "+
			"Edit or delete one there and the change follows forward, not back."),
		t.Muted.Render(truncate("Months are saved in "+m.store.Dir(), cw)),
	}
	// The notes are worth showing only when they cost no binding its row.
	room := h - len(head)
	if len(body)+linesHeight(notes) <= room {
		body = append(body, notes...)
	} else {
		body = scrollWindow(body, cursorLine, room)
	}
	return strings.Join(append(head, body...), "\n")
}

func linesHeight(lines []string) int {
	return strings.Count(strings.Join(lines, "\n"), "\n") + 1
}

// keyPress builds the keystroke a binding stands for, so running it from the
// menu goes through exactly the path pressing it would.
func keyPress(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	}
	r := []rune(k)
	return tea.KeyPressMsg{Code: r[0], Text: k}
}
