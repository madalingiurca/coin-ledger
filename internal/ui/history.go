package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/madalingiurca/budget/internal/money"
	"github.com/madalingiurca/budget/internal/store"
)

// histLimit caps how far back the history view reaches.
const histLimit = 24

const (
	histMonthCol = 8
	histAmtCol   = 12
	// histFullWidth is what the three-amount layout needs.
	histFullWidth = histMonthCol + 3*(1+histAmtCol)
)

// hist is the state of the history screen.
type hist struct {
	rows   []store.Summary
	cursor int
	err    error
}

func (m *Model) openHistory() {
	rows, err := m.store.Summaries(histLimit)
	m.hist = hist{rows: rows, err: err}
	// Open on the month that was being viewed, when it is in the list.
	for i, r := range rows {
		if r.Key == m.dash.key {
			m.hist.cursor = i
			break
		}
	}
	m.screen = screenHistory
}

func (m Model) historyKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "h", "left":
		m.screen = screenMonth
	case "up", "k":
		m.hist.cursor = max(0, m.hist.cursor-1)
	case "down", "j":
		m.hist.cursor = min(len(m.hist.rows)-1, m.hist.cursor+1)
	case "g", "home":
		m.hist.cursor = 0
	case "G", "end":
		m.hist.cursor = len(m.hist.rows) - 1
	case "enter", "right":
		if m.hist.cursor < len(m.hist.rows) {
			m.openMonth(m.hist.rows[m.hist.cursor].Key)
			m.screen = screenMonth
		}
	}
	return m, nil
}

func (m Model) historyHints() []hint {
	return []hint{
		keyHint("↑/↓", "select"),
		keyHint("enter", "open month"),
		keyHint("esc", "back"),
		keyHint("q", "quit"),
	}
}

func (m Model) historyView(cw int) string {
	t := m.theme
	lines := []string{t.Title.Render("history")}

	switch {
	case m.hist.err != nil:
		return strings.Join(append(lines, "", wrap(t.Danger, cw, m.hist.err.Error())), "\n")
	case len(m.hist.rows) == 0:
		return strings.Join(append(lines, "",
			wrap(t.Muted, cw, "No months saved yet — press esc, then add your income.")), "\n")
	}

	// Two columns are reserved on the left for the cursor marker, so selected
	// and unselected rows line up exactly.
	inner := cw - 2
	full := inner >= histFullWidth
	barW := 0
	if inner >= histFullWidth+10 {
		barW = inner - histFullWidth - 1
	}
	scale := m.histScale()

	lines = append(lines, "", t.Section.Render(m.histHeader(cw, full)))
	for i, r := range m.hist.rows {
		lines = append(lines, m.histRow(r, i == m.hist.cursor, cw, full, barW, scale))
	}
	if avg, ok := m.histAverage(); ok {
		lines = append(lines, "", t.Muted.Render(truncate(
			"Average left over "+itoa(m.histReadable())+" months: ", cw))+t.Total.Render(avg.Lei()))
	}
	return strings.Join(lines, "\n")
}

func (m Model) histHeader(cw int, full bool) string {
	head := padRight("MONTH", histMonthCol)
	if full {
		head += " " + padLeft("INCOME", histAmtCol) + " " + padLeft("EXPENSES", histAmtCol)
	}
	head += " " + padLeft("LEFT", histAmtCol)
	return "  " + padRight(head, cw-2)
}

func (m Model) histRow(r store.Summary, selected bool, cw int, full bool, barW int, scale money.Amount) string {
	t := m.theme
	marker := "  "
	if selected {
		marker = "› "
	}
	inner := cw - 2 - barW

	if r.Err != nil {
		text := padRight(padRight(r.Key.String(), histMonthCol)+" unreadable", inner)
		if selected {
			return t.Selected.Render(marker + text)
		}
		return t.Warning.Render(marker + text)
	}

	text := padRight(r.Key.String(), histMonthCol)
	if full {
		text += " " + padLeft(r.Income.String(), histAmtCol)
		text += " " + padLeft(r.Expenses.String(), histAmtCol)
	}
	text += " " + padLeft(r.Left.String(), histAmtCol)
	text = padRight(text, inner)

	if selected {
		return t.Selected.Bold(true).Render(marker+text) + m.histBar(r.Left, barW, scale)
	}
	style := t.Label
	if r.Left < 0 {
		style = t.Negative
	}
	return t.Muted.Render(marker) + style.Render(text) + m.histBar(r.Left, barW, scale)
}

// histBar draws one month's leftover relative to the largest in the window.
func (m Model) histBar(left money.Amount, barW int, scale money.Amount) string {
	if barW <= 0 || scale == 0 {
		return ""
	}
	pct := int(left * 100 / scale)
	if pct > 100 {
		pct = 100
	}
	if pct < -100 {
		pct = -100
	}
	return bar(m.theme, barW, pct)
}

// histScale is the largest absolute leftover on screen, so bars stay comparable.
func (m Model) histScale() money.Amount {
	var scale money.Amount
	for _, r := range m.hist.rows {
		if r.Err != nil {
			continue
		}
		if a := absAmount(r.Left); a > scale {
			scale = a
		}
	}
	return scale
}

func (m Model) histReadable() int {
	n := 0
	for _, r := range m.hist.rows {
		if r.Err == nil {
			n++
		}
	}
	return n
}

func (m Model) histAverage() (money.Amount, bool) {
	var sum money.Amount
	n := m.histReadable()
	if n == 0 {
		return 0, false
	}
	for _, r := range m.hist.rows {
		if r.Err == nil {
			sum += r.Left
		}
	}
	return sum / money.Amount(n), true
}
