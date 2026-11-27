package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// linkReach is how many months apart a refund and its expense may be: a
// refund is looked for up to two months after the expense.
const linkReach = 2

// linkCandidate is one entry the link dialog offers.
type linkCandidate struct {
	month model.Key
	entry model.Entry
	// linked is how much is linked now, before the dialog changes anything.
	linked money.Amount
}

// linkState is the dialog that links a refund to the expenses it pays back,
// or an expense to the refunds that paid it back. It lists the entries of the
// other kind within reach, ticks the ones already linked, and applies the
// difference on enter.
type linkState struct {
	kind   model.Kind
	entry  model.Entry
	cands  []linkCandidate
	picked map[int]bool
	filter string
	cursor int
}

// startLink opens the link dialog for the selected entry.
func (m *Model) startLink() {
	kind, e, ok := m.dash.currentEntry()
	switch {
	case !ok:
		return
	case e.IsTransfer():
		m.setStatus("A transfer is not a refund")
		return
	}
	cands, err := m.linkCandidates(kind, e)
	if err != nil {
		m.setError(err)
		return
	}
	if len(cands) == 0 {
		if kind == model.Income {
			m.setStatus("No expense from the last %d months is waiting for money back", linkReach+1)
		} else {
			m.setStatus("No income from this month or the next %d is free to pay it back", linkReach)
		}
		return
	}
	l := &linkState{kind: kind, entry: e, cands: cands, picked: map[int]bool{}}
	for i, c := range cands {
		if c.linked > 0 {
			l.picked[i] = true
		}
	}
	m.link = l
}

// linkCandidates lists what the entry can be linked to: for a refund, the
// expenses of its month and the two before; for an expense, the income of its
// month and the two after. Entries already linked to it come first, then
// those whose open amount matches, then expenses in paid-back categories,
// then the rest, newest first.
func (m *Model) linkCandidates(kind model.Kind, e model.Entry) ([]linkCandidate, error) {
	var out []linkCandidate
	for n := 0; n <= linkReach; n++ {
		k := m.dash.key.Shift(n)
		if kind == model.Income {
			k = m.dash.key.Shift(-n)
		}
		mo, err := m.monthAt(k)
		if err != nil {
			return nil, err
		}
		if mo == nil {
			continue
		}
		for _, c := range mo.Entries(kind.Other()) {
			if c.IsTransfer() {
				continue
			}
			linked := e.LinkedTo(k, c.ID)
			if linked == 0 && c.Open() <= 0 {
				continue
			}
			out = append(out, linkCandidate{month: k, entry: c, linked: linked})
		}
	}
	rank := func(c linkCandidate) int {
		switch {
		case c.linked > 0:
			return 0
		case c.entry.Open() == e.Open():
			return 1
		case kind == model.Income && m.paidBack(c.entry.Category):
			return 2
		}
		return 3
	}
	slices.SortStableFunc(out, func(a, b linkCandidate) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return strings.Compare(b.entry.Date, a.entry.Date)
	})
	return out, nil
}

// monthAt returns a month to read links from: the one on screen, or one on
// disk. A month that has no file yet holds nothing to link, and is nil.
func (m *Model) monthAt(k model.Key) (*model.Month, error) {
	if k == m.dash.key {
		return m.dash.month, nil
	}
	if !m.store.Exists(k) {
		return nil, nil
	}
	return m.store.Load(k)
}

// visible lists the candidates the filter lets through, by index.
func (l *linkState) visible() []int {
	want := strings.ToLower(l.filter)
	var out []int
	for i, c := range l.cands {
		hay := strings.ToLower(c.entry.Label + " " + c.entry.Payee + " " + c.entry.Category + " " + c.entry.Amount.String())
		if want == "" || strings.Contains(hay, want) {
			out = append(out, i)
		}
	}
	return out
}

func (m Model) linkKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	l := m.link
	vis := l.visible()
	switch msg.String() {
	case "esc":
		m.link = nil
		return m, nil
	case "up", "ctrl+p":
		l.cursor = max(0, l.cursor-1)
		return m, nil
	case "down", "ctrl+n":
		l.cursor = max(0, min(len(vis)-1, l.cursor+1))
		return m, nil
	case "space":
		if l.cursor < len(vis) {
			i := vis[l.cursor]
			l.picked[i] = !l.picked[i]
			l.cursor = min(len(vis)-1, l.cursor+1)
		}
		return m, nil
	case "backspace":
		if r := []rune(l.filter); len(r) > 0 {
			l.filter = string(r[:len(r)-1])
			l.cursor = 0
		}
		return m, nil
	case "enter":
		m.link = nil
		m.applyLinks(l)
		return m, nil
	}
	if msg.Text != "" && msg.Text != " " {
		l.filter += msg.Text
		l.cursor = 0
	}
	return m, nil
}

// applyLinks unlinks what was unticked and links what was ticked, in list
// order, so a refund covers the first expenses ticked in full and the last
// one with what is left. Every other month touched is written before the one
// on screen.
func (m *Model) applyLinks(l *linkState) {
	months := map[model.Key]*model.Month{m.dash.key: m.dash.month.Clone()}
	get := func(k model.Key) (*model.Month, error) {
		if mo, ok := months[k]; ok {
			return mo, nil
		}
		mo, err := m.store.Load(k)
		if err != nil {
			return nil, err
		}
		months[k] = mo
		return mo, nil
	}
	pair := func(c linkCandidate) (refundMonth *model.Month, refundID string, expenseMonth *model.Month, expenseID string, err error) {
		here := months[m.dash.key]
		there, err := get(c.month)
		if err != nil {
			return nil, "", nil, "", err
		}
		if l.kind == model.Income {
			return here, l.entry.ID, there, c.entry.ID, nil
		}
		return there, c.entry.ID, here, l.entry.ID, nil
	}

	var linked, unlinked []string
	var total money.Amount
	for i, c := range l.cands {
		if c.linked == 0 || l.picked[i] {
			continue
		}
		rm, rid, em, eid, err := pair(c)
		if err != nil {
			m.setError(err)
			return
		}
		model.UnlinkRefund(rm, rid, em, eid)
		unlinked = append(unlinked, c.entry.Label)
	}
	for i, c := range l.cands {
		if c.linked > 0 || !l.picked[i] {
			continue
		}
		rm, rid, em, eid, err := pair(c)
		if err != nil {
			m.setError(err)
			return
		}
		amount, err := model.LinkRefund(rm, rid, em, eid)
		if err != nil {
			m.setError(err)
			return
		}
		linked = append(linked, c.entry.Label)
		total += amount
	}
	if len(linked)+len(unlinked) == 0 {
		return
	}
	for k, mo := range months {
		if k == m.dash.key {
			continue
		}
		if err := m.store.Save(mo); err != nil {
			m.setError(err)
			return
		}
	}
	m.dash.month = months[m.dash.key]
	if !m.save() {
		return
	}
	m.selectEntry(l.kind, l.entry.ID)

	var parts []string
	if len(linked) > 0 {
		verb := "pays back"
		if l.kind == model.Expense {
			verb = "paid back by"
		}
		parts = append(parts, fmt.Sprintf("%s %s %s · %s", l.entry.Label, verb, strings.Join(linked, ", "), total.Lei()))
	}
	if len(unlinked) > 0 {
		parts = append(parts, "unlinked "+strings.Join(unlinked, ", "))
	}
	m.setStatus("%s", strings.Join(parts, "; "))
}

// unlinkAll clears every reimbursement the entry takes part in, in the month
// on screen and in the months on the other side, which are written straight
// away. A month that cannot be read keeps its half; this side lets go anyway,
// so the entry can still be deleted.
func (m *Model) unlinkAll(kind model.Kind, e model.Entry) error {
	for _, r := range e.Links() {
		if r.Month == m.dash.key {
			if kind == model.Income {
				model.UnlinkRefund(m.dash.month, e.ID, m.dash.month, r.ID)
			} else {
				model.UnlinkRefund(m.dash.month, r.ID, m.dash.month, e.ID)
			}
			continue
		}
		other, err := m.monthAt(r.Month)
		if err == nil && other != nil {
			other.DropLink(kind.Other(), r.ID, m.dash.key, e.ID)
			if err := m.store.Save(other); err != nil {
				return err
			}
		}
		m.dash.month.DropLink(kind, e.ID, r.Month, r.ID)
	}
	return nil
}

// linkTitle names the dialog after what is being linked.
func (m Model) linkTitle() string {
	if m.link.kind == model.Income {
		return "What does " + m.link.entry.Label + " pay back?"
	}
	return "What paid back " + m.link.entry.Label + "?"
}

// linkView draws the dialog for a popup cw wide and at most h lines tall.
func (m Model) linkView(cw, h int) string {
	t := m.theme
	l := m.link
	filter := t.Muted.Render("type to filter")
	if l.filter != "" {
		filter = t.FieldOn.Render(l.filter) + t.Cursor.Render("▏")
	}
	what := "Refund " + l.entry.Amount.Lei()
	if l.kind == model.Expense {
		what = "Expense " + l.entry.Amount.Lei()
	}
	if open := l.entry.Open(); open != l.entry.Amount {
		what += " · " + open.String() + " free"
	}
	head := []string{t.Muted.Render(truncate(what, cw)), t.Field.Render("Filter  ") + filter, ""}

	vis := l.visible()
	var body []string
	cursorLine := 0
	amtW, _ := entryColumns(cw)
	for n, i := range vis {
		c := l.cands[i]
		tick := "  "
		if l.picked[i] {
			tick = "✓ "
		}
		label := c.entry.Label
		if when, err := time.Parse("2006-01-02", c.entry.Date); err == nil {
			label = when.Format("02 Jan") + " " + label
		} else if c.month != m.dash.key {
			label = c.month.Label() + " " + label
		}
		if c.entry.Category != "" {
			label += " · " + c.entry.Category
		}
		if c.linked == 0 && c.entry.Open() != c.entry.Amount {
			label += " · " + c.entry.Open().String() + " open"
		}
		amount := padLeft(c.entry.Amount.String(), amtW)
		labelW := max(1, cw-2-amtW-1)
		line := tick + padRight(truncate(label, labelW), labelW) + " " + amount
		if n == l.cursor {
			cursorLine = len(body)
			body = append(body, t.SelectedActive.Render(padRight(line, cw)))
			continue
		}
		body = append(body, t.Marker.Render(tick)+t.Label.Render(padRight(truncate(label, labelW), labelW))+" "+t.Amount.Render(amount))
	}
	if len(vis) == 0 {
		body = append(body, t.Muted.Render("nothing matches"))
	}
	body = scrollWindow(body, cursorLine, max(minScroll, h-len(head)))
	return strings.Join(append(head, body...), "\n")
}
