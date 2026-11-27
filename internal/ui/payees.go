package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/madalingiurca/budget/internal/store"
)

// payees is the state of the uncategorised payees screen: everyone money went
// to, in any month, that has not been given a category yet. A payee can be
// opened to file its transactions one by one, for shops where one visit is
// fuel and the next is coffee.
type payees struct {
	rows   []store.PayeeSummary
	cursor int
	err    error
	detail *payeeDetail
}

type payeeDetail struct {
	payee, label string
	rows         []store.PayeeEntry
	cursor       int
	err          error
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

// loadPayeeDetail re-reads the open payee's transactions. When none are left
// uncategorised it goes back to the list.
func (m *Model) loadPayeeDetail() {
	d := m.payees.detail
	d.rows, d.err = m.store.PayeeEntries(d.payee)
	if d.err == nil && len(d.rows) == 0 {
		m.payees.detail = nil
		m.loadPayees()
		return
	}
	d.cursor = max(0, min(d.cursor, len(d.rows)-1))
}

func (m Model) payeesKey(key string) (tea.Model, tea.Cmd) {
	if m.payees.detail != nil {
		return m.payeeDetailKey(key)
	}
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
	case "right", "l", "o":
		if p.cursor < len(p.rows) {
			row := p.rows[p.cursor]
			p.detail = &payeeDetail{payee: row.Payee, label: row.Label}
			m.loadPayeeDetail()
		}
	case "enter", "c":
		if p.cursor < len(p.rows) {
			if m.rulesErr != nil {
				m.setError(m.rulesErr)
				return m, nil
			}
			row := p.rows[p.cursor]
			m.cat = newCat(m.theme, "Categorise "+row.Label, "", row.Payee, "", m.categoryNames())
			m.cat.label = row.Label
		}
	}
	return m, nil
}

func (m Model) payeeDetailKey(key string) (tea.Model, tea.Cmd) {
	d := m.payees.detail
	switch key {
	case "esc", "left", "h":
		m.payees.detail = nil
		m.loadPayees()
	case "up", "k":
		d.cursor = max(0, d.cursor-1)
	case "down", "j":
		d.cursor = max(0, min(len(d.rows)-1, d.cursor+1))
	case "enter", "c":
		if d.cursor < len(d.rows) {
			r := d.rows[d.cursor]
			k := r.Key
			m.cat = newCat(m.theme, "Categorise "+r.Entry.Label+" · "+r.Entry.Amount.Lei(),
				r.Entry.ID, r.Entry.Payee, "", m.categoryNames())
			m.cat.elsewhere = &k
			m.cat.label = r.Entry.Label
			// Filing one at a time is the point here; a rule is the exception.
			m.cat.applyAll = false
		}
	}
	return m, nil
}

// payeesTitle names the payees pane, or the payee open in it.
func (m Model) payeesTitle() string {
	if d := m.payees.detail; d != nil {
		return "[3] " + d.label + " · " + plural(len(d.rows), "uncategorised", "uncategorised")
	}
	if n := len(m.payees.rows); n > 0 {
		return "[3] Uncategorised payees · " + plural(n, "payee", "payees")
	}
	return "[3] Uncategorised payees"
}

func (m Model) payeesCounter() string {
	if d := m.payees.detail; d != nil {
		return counter(d.cursor, len(d.rows))
	}
	return counter(m.payees.cursor, len(m.payees.rows))
}

func (m Model) payeesView(cw, maxH int) string {
	if m.payees.detail != nil {
		return m.payeeDetailView(cw, maxH)
	}
	t := m.theme
	p := m.payees

	var lines []string

	switch {
	case p.err != nil:
		return wrap(t.Danger, cw, p.err.Error())
	case len(p.rows) == 0:
		return wrap(t.Muted, cw, "Every imported expense has a category. Payees show up here after an import brings in someone new.")
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
	lines = append(lines, t.Section.Render(head))

	var body []string
	for i, r := range p.rows {
		label := padRight(truncate(r.Label, labelW-1), labelW)
		times := ""
		if timesW > 0 {
			times = padLeft(itoa(r.Count), timesW)
		}
		amount := padLeft(r.Total.String(), amtW)
		if i == p.cursor {
			body = append(body, t.Selected.Bold(true).Render(padRight("  "+label+times+amount, cw)))
			continue
		}
		body = append(body, t.Label.Render("  "+label)+t.Muted.Render(times)+t.Amount.Render(amount))
	}

	tail := []string{"", wrap(t.Muted, cw,
		"enter gives a payee one category for good: every expense from it follows, in every month and on later imports. "+
			"For a shop that sells several things, press → and file its transactions one by one.")}
	if maxH > 0 {
		fixed := lipgloss.Height(strings.Join(lines, "\n")) + lipgloss.Height(strings.Join(tail, "\n"))
		body = scrollWindow(body, p.cursor, maxH-fixed)
	}
	lines = append(lines, body...)
	lines = append(lines, tail...)
	return strings.Join(lines, "\n")
}

func (m Model) payeeDetailView(cw, maxH int) string {
	t := m.theme
	d := m.payees.detail
	var lines []string
	if d.err != nil {
		return wrap(t.Danger, cw, d.err.Error())
	}

	amtW := min(amountCol, max(6, cw-4-6))
	labelW := max(4, cw-2-amtW)
	var body []string
	for i, r := range d.rows {
		when := r.Entry.Date
		if len(when) == len("2026-10-02") {
			when = when[8:] + " " + r.Key.Month.String()[:3] + " " + when[:4]
		} else {
			when = r.Key.Label()
		}
		label := padRight(truncate(when, labelW-1), labelW)
		amount := padLeft(r.Entry.Amount.String(), amtW)
		if i == d.cursor {
			body = append(body, t.Selected.Bold(true).Render(padRight("  "+label+amount, cw)))
			continue
		}
		body = append(body, t.Label.Render("  "+label)+t.Amount.Render(amount))
	}
	tail := []string{"", wrap(t.Muted, cw, "Each transaction you file here keeps its own category; nothing is remembered for the payee unless you tick it in the dialog.")}
	if maxH > 0 {
		fixed := lipgloss.Height(strings.Join(lines, "\n")) + lipgloss.Height(strings.Join(tail, "\n"))
		body = scrollWindow(body, d.cursor, maxH-fixed)
	}
	lines = append(lines, body...)
	lines = append(lines, tail...)
	return strings.Join(lines, "\n")
}
