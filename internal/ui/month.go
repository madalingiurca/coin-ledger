package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// dash is the state of the month screen.
type dash struct {
	key        model.Key
	month      *model.Month
	onDisk     bool
	seededFrom *model.Key
	warning    error
	// loadErr means this month's file exists but could not be read. The month
	// is shown read-only and is never written over.
	loadErr error
	cursor  int
}

// row addresses one selectable line. index is -1 for the placeholder line an
// empty section still shows, so every section can be the target of "add" and
// the cursor is never homeless.
type row struct {
	kind  model.Kind
	index int
}

func (d dash) rows() []row {
	if d.month == nil {
		return nil
	}
	var out []row
	for _, kind := range []model.Kind{model.Income, model.Expense} {
		entries := d.month.Entries(kind)
		if len(entries) == 0 {
			out = append(out, row{kind: kind, index: -1})
			continue
		}
		for i := range entries {
			out = append(out, row{kind: kind, index: i})
		}
	}
	return out
}

func (d dash) current() (row, bool) {
	rows := d.rows()
	if d.cursor < 0 || d.cursor >= len(rows) {
		return row{}, false
	}
	return rows[d.cursor], true
}

// currentEntry returns the selected entry, if the cursor is on a real one.
func (d dash) currentEntry() (model.Kind, model.Entry, bool) {
	r, ok := d.current()
	if !ok || r.index < 0 {
		return 0, model.Entry{}, false
	}
	return r.kind, d.month.Entries(r.kind)[r.index], true
}

func (d *dash) clamp() {
	if n := len(d.rows()); d.cursor >= n {
		d.cursor = max(0, n-1)
	}
	if d.cursor < 0 {
		d.cursor = 0
	}
}

// openMonth loads a month and makes it the current screen.
func (m *Model) openMonth(k model.Key) {
	res, err := m.store.Get(k)
	if err != nil {
		m.dash = dash{key: k, loadErr: err}
		return
	}
	m.dash = dash{
		key:        k,
		month:      res.Month,
		onDisk:     res.OnDisk,
		seededFrom: res.SeededFrom,
		warning:    res.Warning,
	}
}

// save writes the current month, reporting failure in the status line.
func (m *Model) save() bool {
	if err := m.store.Save(m.dash.month); err != nil {
		m.setError(err)
		return false
	}
	m.dash.onDisk = true
	m.dash.seededFrom = nil
	return true
}

func (m Model) monthKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "left", "[", "p":
		m.openMonth(m.dash.key.Prev())
		return m, nil
	case "right", "]", "n":
		m.openMonth(m.dash.key.Next())
		return m, nil
	case "t":
		m.openMonth(model.Today())
		return m, nil
	case "h":
		m.openHistory()
		return m, nil
	}

	if m.dash.loadErr != nil {
		return m, nil // damaged month: navigation only
	}

	switch key {
	case "up", "k":
		m.dash.cursor = max(0, m.dash.cursor-1)
	case "down", "j":
		m.dash.cursor = min(len(m.dash.rows())-1, m.dash.cursor+1)
	case "g", "home":
		m.dash.cursor = 0
	case "G", "end":
		m.dash.cursor = len(m.dash.rows()) - 1

	case "a":
		if r, ok := m.dash.current(); ok {
			m.form = newForm(m.theme, r.kind, nil)
		}
	case "i":
		m.form = newForm(m.theme, model.Income, nil)
	case "x":
		m.form = newForm(m.theme, model.Expense, nil)

	case "e", "enter":
		if kind, entry, ok := m.dash.currentEntry(); ok {
			m.form = newForm(m.theme, kind, &entry)
		}
	case "d", "delete", "backspace":
		if kind, entry, ok := m.dash.currentEntry(); ok {
			m.confirm = &confirmState{kind: kind, id: entry.ID, label: entry.Label, amount: entry.Amount}
		}
	case "r":
		if kind, entry, ok := m.dash.currentEntry(); ok {
			if err := m.dash.month.SetRecurring(kind, entry.ID, !entry.Recurring); err != nil {
				m.setError(err)
				return m, nil
			}
			if m.save() {
				if entry.Recurring {
					m.setStatus("%s no longer carries to next month", entry.Label)
				} else {
					m.setStatus("%s will carry to next month", entry.Label)
				}
			}
		}
	}
	return m, nil
}

// commitForm applies an add or edit and saves.
func (m *Model) commitForm(kind model.Kind, id, label string, amount money.Amount, recurring bool) {
	if id == "" {
		newID := m.dash.month.Add(kind, label, amount, recurring)
		if !m.save() {
			return
		}
		m.selectEntry(kind, newID)
		m.setStatus("Added %s · %s", label, amount.Lei())
		return
	}
	if err := m.dash.month.Update(kind, id, label, amount, recurring); err != nil {
		m.setError(err)
		return
	}
	if m.save() {
		m.setStatus("Updated %s", label)
	}
}

// selectEntry moves the cursor onto a specific entry.
func (m *Model) selectEntry(kind model.Kind, id string) {
	for i, r := range m.dash.rows() {
		if r.kind == kind && r.index >= 0 && m.dash.month.Entries(kind)[r.index].ID == id {
			m.dash.cursor = i
			return
		}
	}
}

func (m Model) monthHints() []hint {
	if m.dash.loadErr != nil {
		return []hint{keyHint("←/→", "month"), keyHint("h", "history"), keyHint("q", "quit")}
	}
	return []hint{
		keyHint("a", "add"),
		keyHint("e", "edit"),
		keyHint("d", "delete"),
		keyHint("r", "recurring"),
		keyHint("←/→", "month"),
		keyHint("h", "history"),
		keyHint("?", "help"),
		keyHint("q", "quit"),
	}
}

func (m Model) monthView(cw int) string {
	t := m.theme
	var lines []string

	name, when := spread("budget", m.dash.key.Label(), cw)
	lines = append(lines, t.Title.Render(name)+t.Month.Render(when))

	if m.dash.loadErr != nil {
		lines = append(lines,
			"",
			wrap(t.Danger, cw, m.dash.loadErr.Error()),
			"",
			wrap(t.Muted, cw, "Fix the file by hand, or move it aside. Nothing here overwrites it."),
		)
		return strings.Join(lines, "\n")
	}

	if note := m.monthNote(); note != "" {
		lines = append(lines, t.Muted.Render(truncate(note, cw)))
	}
	if m.dash.warning != nil {
		lines = append(lines, wrap(t.Warning, cw, m.dash.warning.Error()))
	}

	rows := m.dash.rows()
	for _, kind := range []model.Kind{model.Income, model.Expense} {
		lines = append(lines, "", m.sectionHeader(kind, cw))
		for i, r := range rows {
			if r.kind != kind {
				continue
			}
			lines = append(lines, m.entryLine(r, i == m.dash.cursor, cw))
		}
	}

	lines = append(lines, "", t.Rule.Render(rule(cw)))
	lines = append(lines, m.leftLine(cw))
	if b := m.leftBar(cw); b != "" {
		lines = append(lines, b)
	}
	return strings.Join(lines, "\n")
}

// monthNote explains an unsaved month: where its entries came from.
func (m Model) monthNote() string {
	switch {
	case m.dash.onDisk:
		return ""
	case m.dash.seededFrom != nil:
		return "new month · recurring entries carried from " + m.dash.seededFrom.Label()
	case m.dash.month.IsEmpty():
		return "new month · nothing saved yet"
	default:
		return "new month · unsaved"
	}
}

func (m Model) sectionHeader(kind model.Kind, cw int) string {
	t := m.theme
	name := "INCOME"
	if kind == model.Expense {
		name = "EXPENSES"
	}
	total := m.dash.month.Total(kind).Lei()
	return t.Section.Render(padRight(name, cw-lipgloss.Width(total))) + t.Total.Render(total)
}

func (m Model) entryLine(r row, selected bool, cw int) string {
	t := m.theme
	amtW, labelW := entryColumns(cw)

	if r.index < 0 {
		text := padRight("  nothing here yet — press a to add", cw)
		if selected {
			return t.Selected.Render(padRight("› "+strings.TrimSpace(text), cw))
		}
		return t.Muted.Render(text)
	}

	e := m.dash.month.Entries(r.kind)[r.index]
	marker := "  "
	if e.Recurring {
		marker = " ↻"
	}
	label := padRight(truncate(e.Label, labelW), labelW)
	amount := padLeft(e.Amount.String(), amtW)

	if selected {
		return t.Selected.Bold(true).Render(padRight("› "+label+marker+amount, cw))
	}
	return t.Label.Render("  "+label) + t.Marker.Render(marker) + t.Amount.Render(amount)
}

// entryColumns splits a row between its amount and its label. The amount
// column shrinks before the label does, and both stay positive however narrow
// the terminal gets, so a row is always exactly cw cells wide.
func entryColumns(cw int) (amtW, labelW int) {
	amtW = min(amountCol, max(6, cw-4-6))
	labelW = max(4, cw-4-amtW)
	return amtW, labelW
}

func (m Model) leftLine(cw int) string {
	t := m.theme
	left := m.dash.month.Left()

	name, style := "LEFT", t.Positive
	if left < 0 {
		name, style = "OVER BUDGET", t.Negative
	}
	amount := left.Lei()
	return t.Total.Render(padRight(name, cw-lipgloss.Width(amount))) + style.Render(amount)
}

func (m Model) leftBar(cw int) string {
	t := m.theme
	pct, ok := m.dash.month.LeftShare()
	if !ok {
		return wrap(t.Muted, cw, "set your income to see how much of it is still yours")
	}

	caption := " " + itoa(abs(pct)) + "% of income"
	if pct < 0 {
		caption = " overspent by " + itoa(abs(pct)) + "% of income"
	}
	width := cw - lipgloss.Width(caption)
	if width < 8 {
		return t.Muted.Render(truncate(strings.TrimSpace(caption), cw))
	}
	return bar(t, width, pct) + t.Muted.Render(caption)
}
