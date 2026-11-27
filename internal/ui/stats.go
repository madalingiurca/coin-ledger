package ui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// statsView is which breakdown fills the totals pane below its figures. s
// steps to the next one and S back, round in a circle.
type statsView int

const (
	statsCategory statsView = iota
	statsLabel
	statsLargest
	statsWeek
	statsIncome
	statsViews // how many there are
)

func (v statsView) next(step int) statsView {
	return statsView((int(v) + step + int(statsViews)) % int(statsViews))
}

// title heads the breakdown.
func (v statsView) title() string {
	switch v {
	case statsLabel:
		return "BY LABEL"
	case statsLargest:
		return "LARGEST"
	case statsWeek:
		return "BY WEEK"
	case statsIncome:
		return "INCOME"
	}
	return "BY CATEGORY"
}

// statItem is one bar of a breakdown.
type statItem struct {
	name   string
	amount money.Amount
}

// breakdown is what one view has to draw: its bars, what they are a share
// of, and a line to show instead when there is nothing to draw.
type breakdown struct {
	items []statItem
	total money.Amount
	empty string
}

// statsPanel fills the space the totals pane has below its figures with the
// chosen breakdown. It draws only as many bars as fit in h lines, the last
// line saying how many were left out.
func (m Model) statsPanel(cw, h int) string {
	if h < 2 || cw < 24 || m.dash.month == nil {
		return ""
	}
	var b breakdown
	switch m.stats {
	case statsLabel:
		b = m.byLabel()
	case statsLargest:
		b = m.largest()
	case statsWeek:
		b = m.byWeek()
	case statsIncome:
		b = m.bySource()
	default:
		b = m.byCategory()
	}
	return m.drawBreakdown(m.stats, b, cw, h)
}

func (m Model) drawBreakdown(v statsView, b breakdown, cw, h int) string {
	t := m.theme
	counter := itoa(int(v)+1) + "/" + itoa(int(statsViews)) + " s"
	head := t.Section.Render(padRight(v.title(), cw-lipgloss.Width(counter))) + t.Muted.Render(counter)
	if len(b.items) == 0 || b.total <= 0 {
		if b.empty == "" {
			b.empty = "Nothing to show this month"
		}
		return head + "\n" + t.Muted.Render(truncate(b.empty, cw))
	}

	rows := len(b.items)
	more := 0
	if rows > h-1 {
		rows = max(0, h-2)
		more = len(b.items) - rows
	}
	if rows == 0 {
		return head
	}

	amtW, longest := 0, 0
	for _, it := range b.items[:rows] {
		amtW = max(amtW, lipgloss.Width(it.amount.String()))
		longest = max(longest, lipgloss.Width(it.name))
	}
	// The names take what they need, up to 24 cells, as long as the bar
	// keeps at least 6.
	nameW := max(4, min(longest+1, 24, cw-amtW-1-6))
	barW := max(0, cw-nameW-amtW-1)

	lines := []string{head}
	for _, it := range b.items[:rows] {
		pct := int(it.amount * 100 / b.total)
		if pct == 0 && it.amount > 0 {
			pct = 1
		}
		line := t.Label.Render(padRight(truncate(it.name, nameW-1), nameW))
		if barW > 0 {
			line += bar(t, barW, pct) + " "
		}
		lines = append(lines, line+t.Amount.Render(padLeft(it.amount.String(), amtW)))
	}
	if more > 0 {
		lines = append(lines, t.Muted.Render("+ "+itoa(more)+" more"))
	}
	return strings.Join(lines, "\n")
}

// largestFirst sorts the bars by amount, keeping the given order for ties.
func largestFirst(items []statItem) []statItem {
	sort.SliceStable(items, func(i, j int) bool { return items[i].amount > items[j].amount })
	return items
}

// byCategory is where the month's money went: each category that counts,
// largest first, as a share of all expenses.
func (m Model) byCategory() breakdown {
	var items []statItem
	for _, c := range m.dash.month.Categories() {
		if c.Projected > 0 {
			items = append(items, statItem{categoryName(c.Name), c.Projected})
		}
	}
	return breakdown{items: largestFirst(items), total: m.dash.month.Total(model.Expense),
		empty: "No expenses this month"}
}

// counted is one entry and what it adds to the month's expense total.
type counted struct {
	entry  model.Entry
	amount money.Amount
}

// countedExpenses are the expenses that make up the month's total, each for
// what it counts. A category counts for its plan or for what the bank
// reported, whichever is more, so only that side is taken: a groceries
// budget and the shop rows that spend it are not both counted, and a bank
// row that replaced a smaller plan counts for the plan. Uncategorised
// expenses count both sides, as in the total. Transfers count for nothing.
// What the amounts add up to is the month's expense total.
func countedExpenses(mo *model.Month) []counted {
	useActual := map[string]bool{}
	for _, c := range mo.Categories() {
		useActual[c.Name] = c.Actual >= c.Forecast
	}
	var out []counted
	for _, e := range mo.Expenses {
		var a money.Amount
		switch {
		case e.IsTransfer():
		case e.Category == "":
			a = e.Forecast() + e.Actual()
		case useActual[e.Category]:
			a = e.Actual()
		default:
			a = e.Forecast()
		}
		if a > 0 {
			out = append(out, counted{e, a})
		}
	}
	return out
}

// labelKey groups entries under one bar: bank rows by payee, so the many
// rows of one shop add up, and typed entries by their label.
func labelKey(e model.Entry) string {
	if e.Payee != "" {
		return e.Payee
	}
	return strings.ToLower(strings.TrimSpace(e.Label))
}

// byLabel is who the money went to: expenses added up by payee or label,
// largest first, as a share of all expenses.
func (m Model) byLabel() breakdown {
	return breakdown{items: largestFirst(sumByLabel(countedExpenses(m.dash.month))),
		total: m.dash.month.Total(model.Expense), empty: "No expenses this month"}
}

// sumByLabel adds entries up by labelKey, in the order each first appears,
// named after the first entry's label.
func sumByLabel(entries []counted) []statItem {
	at := map[string]int{}
	var items []statItem
	for _, c := range entries {
		k := labelKey(c.entry)
		i, ok := at[k]
		if !ok {
			i = len(items)
			at[k] = i
			items = append(items, statItem{name: strings.TrimSpace(c.entry.Label)})
		}
		items[i].amount += c.amount
	}
	return items
}

// largest are the month's single biggest expenses, each for what it counts.
func (m Model) largest() breakdown {
	var items []statItem
	for _, c := range countedExpenses(m.dash.month) {
		items = append(items, statItem{strings.TrimSpace(c.entry.Label), c.amount})
	}
	return breakdown{items: largestFirst(items), total: m.dash.month.Total(model.Expense),
		empty: "No expenses this month"}
}

// byWeek is when the money went: what the bank booked in each week of the
// month, the 1st to the 7th, the 8th to the 14th and so on, in order. Plans
// have no date, so only bank rows are counted, as a share of what they add
// up to. A row booked in the month before or after but counted in this one,
// like an early rent, gets a bar of its own at that end.
func (m Model) byWeek() breakdown {
	mo := m.dash.month
	days := time.Date(mo.Key.Year, mo.Key.Month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	weeks := (days + 6) / 7
	sums := make([]money.Amount, weeks)
	var before, after, total money.Amount
	for _, e := range mo.Expenses {
		d, err := time.Parse("2006-01-02", e.Date)
		if err != nil || e.IsTransfer() || e.Actual() <= 0 {
			continue
		}
		total += e.Actual()
		switch k := model.KeyOf(d); {
		case k.Before(mo.Key):
			before += e.Actual()
		case k.After(mo.Key):
			after += e.Actual()
		default:
			sums[min(weeks-1, (d.Day()-1)/7)] += e.Actual()
		}
	}
	if total <= 0 {
		return breakdown{empty: "No bank rows yet"}
	}
	month := mo.Key.Month.String()[:3]
	var items []statItem
	if before > 0 {
		items = append(items, statItem{"in " + mo.Key.Prev().Month.String()[:3], before})
	}
	for w := range weeks {
		from, to := w*7+1, min(days, w*7+7)
		items = append(items, statItem{strconv.Itoa(from) + "–" + strconv.Itoa(to) + " " + month, sums[w]})
	}
	if after > 0 {
		items = append(items, statItem{"in " + mo.Key.Next().Month.String()[:3], after})
	}
	return breakdown{items: items, total: total}
}

// bySource is where the month's money came from: income added up by payee or
// label, largest first, without transfers from your own accounts.
func (m Model) bySource() breakdown {
	var entries []counted
	for _, e := range m.dash.month.Income {
		if !e.IsTransfer() && e.Counted() > 0 {
			entries = append(entries, counted{e, e.Counted()})
		}
	}
	return breakdown{items: largestFirst(sumByLabel(entries)), total: m.dash.month.Total(model.Income),
		empty: "No income this month"}
}
