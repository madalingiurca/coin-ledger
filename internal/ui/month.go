package ui

import (
	"sort"
	"strings"
	"time"

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
	// expanded names the expense categories whose entries are shown.
	// Categories start folded, showing only their header and total. The set
	// belongs to the app rather than the month, so an unfolded category stays
	// unfolded while paging through months.
	expanded map[string]bool
}

// row addresses one selectable line. index is -1 for the placeholder line an
// empty section still shows, so every section can be the target of "add" and
// the cursor is never homeless. A header row stands for a whole expense
// category; cat names the category of a header or of an expense entry.
type row struct {
	kind   model.Kind
	index  int
	header bool
	cat    string
}

// grouped reports whether expenses are shown under category headers. A month
// where nothing has a category yet reads as one plain list.
func (d dash) grouped() bool {
	if d.month == nil {
		return false
	}
	cats := d.month.Categories()
	return len(cats) > 1 || (len(cats) == 1 && cats[0].Name != "")
}

func (d dash) rows() []row {
	if d.month == nil {
		return nil
	}
	var out []row
	if len(d.month.Income) == 0 {
		out = append(out, row{kind: model.Income, index: -1})
	}
	// Money moved in from your own accounts is listed after real income.
	for _, transfers := range []bool{false, true} {
		for _, i := range byDay(d.month.Income, func(e model.Entry) bool { return e.IsTransfer() == transfers }) {
			out = append(out, row{kind: model.Income, index: i})
		}
	}

	switch {
	case len(d.month.Expenses) == 0:
		out = append(out, row{kind: model.Expense, index: -1})
	case !d.grouped():
		for _, i := range byDay(d.month.Expenses, func(model.Entry) bool { return true }) {
			out = append(out, row{kind: model.Expense, index: i})
		}
	default:
		for _, c := range d.month.Categories() {
			out = append(out, row{kind: model.Expense, index: -1, header: true, cat: c.Name})
			if !d.expanded[c.Name] {
				continue
			}
			for _, i := range byDay(d.month.Expenses, func(e model.Entry) bool { return e.Category == c.Name }) {
				out = append(out, row{kind: model.Expense, index: i, cat: c.Name})
			}
		}
	}
	return out
}

// byDay lists the indexes of the entries that pass keep, in the order they
// are shown: planned entries first, as they have no day, then bank rows by
// day. An import adds one bank's rows after another's, so file order would
// cluster them by statement rather than by when the money moved.
func byDay(entries []model.Entry, keep func(model.Entry) bool) []int {
	var idx []int
	for i, e := range entries {
		if keep(e) {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool { return entries[idx[a]].Date < entries[idx[b]].Date })
	return idx
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
	if !ok || r.index < 0 || r.header {
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
		m.dash = dash{key: k, loadErr: err, expanded: m.expanded}
		return
	}
	m.dash = dash{
		expanded:   m.expanded,
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
	case "I":
		m.openImport()
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
	case "tab":
		m.dash.jumpSection()

	case "a":
		if r, ok := m.dash.current(); ok {
			// Adding from inside a category files the new expense there.
			m.form = newForm(m.theme, r.kind, nil, r.cat, m.categoryNames())
		}
	case "i":
		m.form = newForm(m.theme, model.Income, nil, "", nil)
	case "x":
		m.form = newForm(m.theme, model.Expense, nil, "", m.categoryNames())

	case "enter", "space", " ":
		if r, ok := m.dash.current(); ok && r.header {
			m.dash.expanded[r.cat] = !m.dash.expanded[r.cat]
			return m, nil
		}
		if key != "enter" {
			return m, nil
		}
		fallthrough
	case "e":
		if r, ok := m.dash.current(); ok && r.header {
			m.openRename(r.cat)
			return m, nil
		}
		if kind, entry, ok := m.dash.currentEntry(); ok {
			m.form = newForm(m.theme, kind, &entry, entry.Category, m.categoryNames())
		}
	case "c":
		kind, entry, ok := m.dash.currentEntry()
		switch {
		case !ok:
		case kind == model.Income:
			m.setStatus("Only expenses have categories")
		default:
			m.openCategorise(entry)
		}
	case "u":
		m.openPayees()
	case ">":
		m.startMove(1)
	case "<":
		m.startMove(-1)
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
func (m *Model) commitForm(kind model.Kind, id, label string, amount money.Amount, recurring bool, category string) {
	if kind == model.Expense {
		category = m.useCategory(category)
	} else {
		category = ""
	}

	if id == "" {
		newID := m.dash.month.Add(kind, label, amount, recurring)
		if kind == model.Expense {
			m.dash.month.SetCategory(newID, category)
		}
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
	if kind == model.Expense {
		m.dash.month.SetCategory(id, category)
	}
	if m.save() {
		m.selectEntry(kind, id)
		m.setStatus("Updated %s", label)
	}
}

// categoryNames lists the categories to offer, by name.
func (m Model) categoryNames() []string {
	if m.rules == nil {
		return nil
	}
	return m.rules.SortedCategories()
}

// useCategory returns a category name as it should be stored, remembering a
// new one for next time. A category that only differs from a known one by
// case is that known one.
func (m *Model) useCategory(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || m.rules == nil {
		return name
	}
	before := len(m.rules.Categories)
	name = m.rules.AddCategory(name)
	if len(m.rules.Categories) != before && m.rulesErr == nil {
		if err := m.store.SaveRules(m.rules); err != nil {
			m.setError(err)
		}
	}
	return name
}

// selectEntry moves the cursor onto a specific entry, unfolding its category
// if that is what hides it.
func (m *Model) selectEntry(kind model.Kind, id string) {
	if e, ok := m.dash.month.Find(kind, id); ok && kind == model.Expense {
		m.dash.expanded[e.Category] = true
	}
	for i, r := range m.dash.rows() {
		if r.kind == kind && r.index >= 0 && !r.header && m.dash.month.Entries(kind)[r.index].ID == id {
			m.dash.cursor = i
			return
		}
	}
	m.dash.clamp()
}

// spot remembers where the cursor is within its list, so that it can stay
// there when the entries under it are filed elsewhere.
type spot struct {
	kind model.Kind
	cat  string
	// ids are the entries to try in order: the one under the cursor, then
	// those after it in the same list, then those before it.
	ids []string
	at  int
}

func (d dash) spot() spot {
	rows := d.rows()
	if d.cursor < 0 || d.cursor >= len(rows) {
		return spot{at: d.cursor}
	}
	cur := rows[d.cursor]
	s := spot{kind: cur.kind, cat: cur.cat, at: d.cursor}
	add := func(i int) {
		r := rows[i]
		if !r.header && r.index >= 0 && r.kind == cur.kind && r.cat == cur.cat {
			s.ids = append(s.ids, d.month.Entries(r.kind)[r.index].ID)
		}
	}
	for i := d.cursor; i < len(rows); i++ {
		add(i)
	}
	for i := d.cursor - 1; i >= 0; i-- {
		add(i)
	}
	return s
}

// returnTo puts the cursor back on the spot: on the first of its entries
// still in the list it was in, or failing that on the row it was on.
func (m *Model) returnTo(s spot) {
	for _, id := range s.ids {
		if e, ok := m.dash.month.Find(s.kind, id); ok && e.Category == s.cat {
			m.selectEntry(s.kind, id)
			return
		}
	}
	m.dash.cursor = s.at
	m.dash.clamp()
}

// monthTitle names the month pane: its jump key and the month.
func (m Model) monthTitle() string { return "[1] " + m.dash.key.Label() }

// monthError explains a month whose file cannot be read.
func (m Model) monthError(cw int) string {
	t := m.theme
	return strings.Join([]string{
		wrap(t.Danger, cw, m.dash.loadErr.Error()),
		"",
		wrap(t.Muted, cw, "Fix the file by hand, or move it aside. Nothing here overwrites it."),
	}, "\n")
}

// monthSection draws one section of the month, income or expenses, for a
// pane h lines tall. When it is longer than that it scrolls around the
// cursor. Income carries the month's notes above it, as the first pane.
// count is the cursor's place in the section, empty when the cursor is in
// the other one; lines is how tall the section is unscrolled. An h of zero
// means no limit.
func (m Model) monthSection(kind model.Kind, cw, h int) (body, count string, lines int) {
	t := m.theme
	var head []string
	if kind == model.Income {
		if note := m.monthNote(); note != "" {
			head = append(head, t.Muted.Render(truncate(note, cw)))
		}
		if m.dash.warning != nil {
			head = append(head, wrap(t.Warning, cw, m.dash.warning.Error()))
		}
		if len(head) > 0 {
			head = append(head, "")
		}
	}
	head = append(head, m.sectionHeader(kind, cw))

	var out []string
	cursorLine, place, n := 0, -1, 0
	totals := map[string]model.CategoryTotal{}
	for _, c := range m.dash.month.Categories() {
		totals[c.Name] = c
	}
	days := m.dayBuckets()
	lastDay, lastCat := "\x00", "\x00"
	for i, r := range m.dash.rows() {
		if r.kind != kind {
			continue
		}
		selected := i == m.dash.cursor
		if selected {
			place = n
		}
		n++
		if r.header {
			lastDay, lastCat = "\x00", r.cat
			if selected {
				cursorLine = len(out)
			}
			out = append(out, m.categoryLine(totals[r.cat], selected, cw))
			continue
		}
		bucketed := false
		if kind == model.Expense && r.index >= 0 {
			e := m.dash.month.Expenses[r.index]
			if days.used[e.Category] {
				bucketed = true
				if r.cat != lastCat {
					lastDay, lastCat = "\x00", r.cat
				}
				if e.Date != lastDay {
					lastDay = e.Date
					out = append(out, m.dayLine(e, days, cw))
				}
			}
		}
		if selected {
			cursorLine = len(out)
		}
		out = append(out, m.entryLine(r, selected, bucketed, cw))
	}

	lines = lipgloss.Height(strings.Join(head, "\n")) + len(out)
	if h > 0 {
		out = scrollWindow(out, cursorLine, h-lipgloss.Height(strings.Join(head, "\n")))
	}
	if place >= 0 {
		count = counter(place, n)
	}
	return strings.Join(append(head, out...), "\n"), count, lines
}

// cursorKind is the section the cursor is in.
func (m Model) cursorKind() model.Kind {
	if r, ok := m.dash.current(); ok {
		return r.kind
	}
	return model.Income
}

// jumpSection moves the cursor to the first row of the other section.
func (d *dash) jumpSection() {
	r, ok := d.current()
	if !ok {
		return
	}
	for i, other := range d.rows() {
		if other.kind != r.kind {
			d.cursor = i
			return
		}
	}
}

// monthTotals is what the totals pane shows. full adds the income and
// expense totals, for a pane beside the list where they are not crowded.
func (m Model) monthTotals(cw int, full bool) string {
	t := m.theme
	var lines []string
	if full {
		for _, kind := range []model.Kind{model.Income, model.Expense} {
			name := "Income"
			if kind == model.Expense {
				name = "Expenses"
			}
			amount := m.dash.month.Total(kind).String()
			lines = append(lines, t.Label.Render(padRight(name, cw-lipgloss.Width(amount)))+t.Amount.Render(amount))
		}
	}
	if m.dash.month.HasActuals() {
		lines = append(lines, m.spentLine(cw))
	}
	if full {
		lines = append(lines, t.Rule.Render(rule(cw)))
	}
	lines = append(lines, m.leftLine(cw))
	if b := m.leftBar(cw); b != "" {
		lines = append(lines, b)
	}
	return strings.Join(lines, "\n")
}

// minScroll is the shortest list worth scrolling. On a terminal too short to
// show even that, the list is drawn whole and the pane cuts it.
const minScroll = 3

// scrollWindow cuts lines down to at most h, centred on the cursor line.
func scrollWindow(lines []string, cursor, h int) []string {
	if h < minScroll || len(lines) <= h {
		return lines
	}
	start := min(max(0, cursor-h/2), len(lines)-h)
	return lines[start : start+h]
}

// categoryLine is the header of one expense category: how many entries it
// holds and what it comes to, actual against forecast when it has both.
func (m Model) categoryLine(c model.CategoryTotal, selected bool, cw int) string {
	t := m.theme
	arrow := "▸"
	if m.dash.expanded[c.Name] {
		arrow = "▾"
	}
	name := c.Name
	if name == "" {
		name = "Uncategorised"
	}
	left := arrow + " " + name + " (" + itoa(c.Count) + ")"

	var right string
	switch {
	case c.Name == model.Transfers:
		right = "not counted " + (c.Forecast + c.Actual).String()
	case c.Forecast > 0 && c.Actual > 0:
		right = c.Actual.String() + " of " + c.Forecast.String()
	default:
		right = c.Projected.String()
	}
	if lipgloss.Width(right) > cw/2 {
		right = c.Projected.String()
	}
	leftW := max(0, cw-lipgloss.Width(right))
	left = padRight(truncate(left, max(0, leftW-1)), leftW)

	switch {
	case selected:
		return t.Selected.Bold(true).Render(padRight(left+right, cw))
	case c.Name == model.Transfers:
		return t.Muted.Render(left + right)
	case c.Name == "" && c.Actual > 0:
		return t.Warning.Render(left) + t.Total.Render(right)
	case c.Forecast > 0 && c.Actual > c.Forecast:
		return t.Section.Render(left) + t.Negative.Render(right)
	}
	return t.Section.Render(left) + t.Total.Render(right)
}

// spentLine separates what the bank has already reported from the projection
// in the line below it.
func (m Model) spentLine(cw int) string {
	t := m.theme
	amount := m.dash.month.Spent().Lei()
	return t.Muted.Render(padRight("spent so far", cw-lipgloss.Width(amount)) + amount)
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

// dayTotals is what the bank booked on each day within each expense category,
// and which categories are laid out by day at all: those with a bank row.
type dayTotals struct {
	sum  map[string]money.Amount
	used map[string]bool
}

func (m Model) dayBuckets() dayTotals {
	d := dayTotals{sum: map[string]money.Amount{}, used: map[string]bool{}}
	if m.dash.month == nil {
		return d
	}
	for _, e := range m.dash.month.Expenses {
		if len(e.Date) == len("2026-10-02") {
			d.used[e.Category] = true
			d.sum[e.Category+"|"+e.Date] += e.Amount
		}
	}
	return d
}

// dayLine is the heading over the entries of one day, "Wed 04 Nov" and what
// was spent that day; entries with no day, the plan, sit under "Planned".
func (m Model) dayLine(e model.Entry, d dayTotals, cw int) string {
	t := m.theme
	name, right := "Planned", ""
	if when, err := time.Parse("2006-01-02", e.Date); err == nil {
		name = when.Format("Mon 02 Jan")
		if e.Category != model.Transfers {
			right = d.sum[e.Category+"|"+e.Date].String()
		}
	}
	amtW, _ := entryColumns(cw)
	right = padLeft(right, amtW+2)
	return t.Section.Render("  "+padRight(truncate(name, max(1, cw-2-amtW-2)), max(1, cw-2-amtW-2))) + t.Muted.Render(right)
}

func (m Model) entryLine(r row, selected, bucketed bool, cw int) string {
	t := m.theme
	amtW, labelW := entryColumns(cw)

	if r.index < 0 {
		text := padRight("  nothing here yet — press a to add", cw)
		if selected {
			return t.Selected.Render(padRight("  "+strings.TrimSpace(text), cw))
		}
		return t.Muted.Render(text)
	}

	e := m.dash.month.Entries(r.kind)[r.index]
	marker := "  "
	if e.Recurring {
		marker = " ↻"
	}
	text := e.Label
	switch {
	case bucketed:
		text = "  " + text // under its day's heading
	case len(e.Date) == len("2026-10-02"):
		text = e.Date[8:] + " " + text // day of the month the bank booked it
		if e.Date[:7] != m.dash.key.String() {
			// Booked in another month and counted in this one.
			if when, err := time.Parse("2006-01-02", e.Date); err == nil {
				text = when.Format("02 Jan") + " " + e.Label
			}
		}
	}
	if e.Planned > 0 && e.Planned != e.Amount {
		text += " · planned " + e.Planned.String()
	}
	if r.kind == model.Income && e.IsTransfer() {
		text += " · transfer, not counted"
	}
	label := padRight(truncate(text, labelW), labelW)
	amount := padLeft(e.Amount.String(), amtW)

	if selected {
		return t.Selected.Bold(true).Render(padRight("  "+label+marker+amount, cw))
	}
	if (e.IsForecast() && m.dash.month.HasActuals()) || (r.kind == model.Income && e.IsTransfer()) {
		// Still only expected: the bank has not reported it yet.
		return t.Muted.Render("  "+label) + t.Marker.Render(marker) + t.Muted.Render(amount)
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
