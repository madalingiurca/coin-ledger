package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/madalingiurca/budget/internal/store"
)

// payees is the state of the uncategorised payees screen: everyone money went
// to, in any month, that has not been given a category yet.
type payees struct {
	rows   []store.PayeeSummary
	cursor int
	err    error
}

func (m *Model) openPayees() {
	m.payees = payees{}
	m.loadPayees()
	m.screen = screenPayees
}

// loadPayees re-reads the list, keeping the cursor where it was so that
// categorising one payee lands on the next.
func (m *Model) loadPayees() {
	m.payees.rows, m.payees.err = m.store.UncategorisedPayees()
	m.payees.cursor = max(0, min(m.payees.cursor, len(m.payees.rows)-1))
}

func (m Model) payeesKey(key string) (tea.Model, tea.Cmd) {
	p := &m.payees
	switch key {
	case "esc", "u", "left":
		m.screen = screenMonth
	case "up", "k":
		p.cursor = max(0, p.cursor-1)
	case "down", "j":
		p.cursor = max(0, min(len(p.rows)-1, p.cursor+1))
	case "g", "home":
		p.cursor = 0
	case "G", "end":
		p.cursor = max(0, len(p.rows)-1)
	case "enter", "c":
		if p.cursor < len(p.rows) {
			if m.rulesErr != nil {
				m.setError(m.rulesErr)
				return m, nil
			}
			row := p.rows[p.cursor]
			m.cat = newCat(m.theme, "Categorise "+row.Label, "", row.Payee, "", m.categoryNames())
		}
	}
	return m, nil
}

func (m Model) payeesHints() []hint {
	if len(m.payees.rows) == 0 {
		return []hint{keyHint("esc", "back"), keyHint("q", "quit")}
	}
	return []hint{
		keyHint("enter", "categorise"),
		keyHint("↑/↓", "move"),
		keyHint("esc", "back"),
		keyHint("?", "help"),
		keyHint("q", "quit"),
	}
}

func (m Model) payeesView(cw, maxH int) string {
	t := m.theme
	p := m.payees

	count := ""
	if n := len(p.rows); n > 0 {
		count = plural(n, "payee", "payees")
	}
	name, right := spread("uncategorised", count, cw)
	lines := []string{t.Title.Render(name) + t.Month.Render(right)}

	switch {
	case p.err != nil:
		return strings.Join(append(lines, "", wrap(t.Danger, cw, p.err.Error())), "\n")
	case len(p.rows) == 0:
		return strings.Join(append(lines, "",
			wrap(t.Muted, cw, "Every imported expense has a category. Payees show up here after an import brings in someone new.")), "\n")
	}

	amtW := min(amountCol, max(6, cw-4-6))
	timesW := 5
	if cw < 44 {
		timesW = 0 // too narrow for the count column
	}
	labelW := max(4, cw-2-amtW-timesW)

	head := padRight("  PAYEE", 2+labelW)
	if timesW > 0 {
		head += padLeft("TIMES", timesW)
	}
	head += padLeft("TOTAL", amtW)
	lines = append(lines, "", t.Section.Render(head))

	var body []string
	for i, r := range p.rows {
		label := padRight(truncate(r.Label, labelW-1), labelW)
		times := ""
		if timesW > 0 {
			times = padLeft(itoa(r.Count), timesW)
		}
		amount := padLeft(r.Total.String(), amtW)
		if i == p.cursor {
			body = append(body, t.Selected.Bold(true).Render(padRight("› "+label+times+amount, cw)))
			continue
		}
		body = append(body, t.Label.Render("  "+label)+t.Muted.Render(times)+t.Amount.Render(amount))
	}

	tail := []string{"", wrap(t.Muted, cw,
		"Give a payee a category once and every expense from it follows, in every month and on later imports.")}
	if maxH > 0 {
		fixed := lipgloss.Height(strings.Join(lines, "\n")) + lipgloss.Height(strings.Join(tail, "\n"))
		body = scrollWindow(t, body, p.cursor, maxH-fixed, cw)
	}
	lines = append(lines, body...)
	lines = append(lines, tail...)
	return strings.Join(lines, "\n")
}
