package ui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/madalingiurca/budget/internal/model"
)

// The parts of the categorise dialog the focus can be on.
const (
	catList = iota
	catMatch
	catApply
)

// catState is the categorise dialog. It serves two callers: an expense on the
// month screen (entryID set), and a payee on the uncategorised payees screen
// (entryID empty), where the only possible outcome is a rule.
type catState struct {
	entryID string
	// elsewhere is the month the entry lives in when that is not the month on
	// screen, as when filing a payee's transactions one by one.
	elsewhere *model.Key
	label     string
	title     string
	// payee is the normalised payee a rule can be made for; empty for a
	// hand-typed expense, which has none.
	payee string

	// options are the categories to pick from. In entry mode the first is
	// the empty name, meaning uncategorised. One more row follows them all:
	// a new category, typed into newName.
	options []string
	cursor  int
	newName textinput.Model

	// match is the text a payee must contain for the rule to apply. It starts
	// as the whole payee and can be shortened to cover its siblings.
	match    textinput.Model
	applyAll bool

	focus int
	err   string
}

func newCat(t Theme, title, entryID, payee, current string, categories []string) *catState {
	c := &catState{entryID: entryID, title: title, payee: payee}
	if entryID != "" {
		c.options = append(c.options, "")
	}
	c.options = append(c.options, categories...)
	for i, name := range c.options {
		if name == current {
			c.cursor = i
		}
	}
	if entryID == "" || current == "" {
		// Nothing chosen yet: start on the first category the user made, or
		// on "new" when the only one is the built-in Transfers.
		c.cursor = len(c.options) - len(categories)
		if len(categories) == 0 || categories[0] == model.Transfers {
			c.cursor = len(c.options)
		}
	}

	c.newName = textinput.New()
	c.newName.Prompt = ""
	c.newName.CharLimit = 40
	c.newName.Placeholder = "new category"

	c.match = textinput.New()
	c.match.Prompt = ""
	c.match.CharLimit = 60
	c.match.SetValue(payee)

	c.setFocus(catList)
	c.restyle(t)
	return c
}

func (c *catState) restyle(t Theme) {
	styles := textinput.DefaultStyles(t.dark)
	c.newName.SetStyles(styles)
	c.match.SetStyles(styles)
}

func (c *catState) onNew() bool { return c.cursor == len(c.options) }

// hasRule reports whether the dialog offers to remember the payee at all.
func (c *catState) hasRule() bool { return c.payee != "" }

// stops are the parts the focus cycles through.
func (c *catState) stops() []int {
	switch {
	case !c.hasRule():
		return []int{catList}
	case c.entryID == "":
		return []int{catList, catMatch} // a payee is always remembered
	}
	return []int{catList, catMatch, catApply}
}

func (c *catState) setFocus(part int) {
	c.focus = part
	c.newName.Blur()
	c.match.Blur()
	switch {
	case part == catMatch:
		c.match.Focus()
	case part == catList && c.onNew():
		c.newName.Focus()
	}
}

func (c *catState) step(n int) {
	stops := c.stops()
	at := 0
	for i, s := range stops {
		if s == c.focus {
			at = i
		}
	}
	c.setFocus(stops[((at+n)%len(stops)+len(stops))%len(stops)])
}

func (c *catState) move(n int) {
	c.cursor = min(max(0, c.cursor+n), len(c.options))
	c.setFocus(catList)
}

// chosen validates the dialog and returns the category picked and, when the
// payee is to be remembered, the text the rule matches on.
func (c *catState) chosen() (category, match string, ok bool) {
	if c.onNew() {
		category = strings.TrimSpace(c.newName.Value())
		if category == "" {
			c.err = "Type a name for the new category."
			c.setFocus(catList)
			return "", "", false
		}
	} else {
		category = c.options[c.cursor]
	}

	remember := c.hasRule() && category != "" && (c.applyAll || c.entryID == "")
	if !remember {
		return category, "", true
	}
	match = model.NormalizePayee(c.match.Value())
	switch {
	case match == "":
		c.err = "Say which text identifies this payee."
	case !strings.Contains(c.payee, match):
		c.err = "This payee does not contain that text."
	default:
		return category, match, true
	}
	c.setFocus(catMatch)
	return "", "", false
}

// openCategorise opens the dialog for an expense on the month screen.
func (m *Model) openCategorise(e model.Entry) {
	m.cat = newCat(m.theme, "Categorise "+e.Label, e.ID, e.Payee, e.Category, m.categoryNames())
	m.cat.label = e.Label
}

func (m Model) catKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	c := m.cat
	switch msg.String() {
	case "esc":
		m.cat = nil
		return m, nil
	case "tab":
		c.step(1)
		return m, nil
	case "shift+tab":
		c.step(-1)
		return m, nil
	case "up":
		if c.focus == catList {
			c.move(-1)
		} else {
			c.step(-1)
		}
		return m, nil
	case "down":
		if c.focus == catList {
			c.move(1)
		} else {
			c.step(1)
		}
		return m, nil
	case "space", " ":
		if c.focus == catApply {
			c.applyAll = !c.applyAll
			return m, nil
		}
	case "enter":
		category, match, ok := c.chosen()
		if !ok {
			return m, nil
		}
		m.cat = nil
		m.applyCategory(c.entryID, c.elsewhere, c.label, category, match)
		return m, nil
	}

	c.err = ""
	// Typing on the list starts a new category name rather than being lost:
	// a name that turns out to exist already is that category.
	if c.focus == catList && !c.onNew() && msg.Text != "" && msg.Text != " " {
		c.cursor = len(c.options)
		c.setFocus(catList)
	}
	var cmd tea.Cmd
	switch {
	case c.focus == catMatch:
		c.match, cmd = c.match.Update(msg)
	case c.focus == catList && c.onNew():
		c.newName, cmd = c.newName.Update(msg)
	}
	return m, cmd
}

// applyCategory carries out the dialog: files the expense, and when match is
// set remembers the payee and files every other uncategorised expense from it,
// in every month.
func (m *Model) applyCategory(entryID string, elsewhere *model.Key, label, category, match string) {
	category = m.useCategory(category)
	shown := category
	if shown == "" {
		shown = "Uncategorised"
	}

	if label == "" {
		label = match
	}
	switch {
	case entryID != "" && elsewhere != nil:
		if err := m.store.SetCategory(*elsewhere, entryID, category); err != nil {
			m.setError(err)
			return
		}
		if m.dash.key == *elsewhere && m.dash.onDisk {
			m.openMonth(m.dash.key)
		}
		if m.payees.detail != nil {
			m.loadPayeeDetail()
		}
	case entryID != "":
		e, ok := m.dash.month.Find(model.Expense, entryID)
		if !ok {
			return
		}
		label = e.Label
		if err := m.dash.month.SetCategory(entryID, category); err != nil {
			m.setError(err)
			return
		}
		if !m.save() {
			return
		}
		m.selectEntry(model.Expense, entryID)
	}
	if match == "" {
		m.setStatus("%s → %s", label, shown)
		if m.screen == screenPayees && m.payees.detail == nil {
			m.loadPayees()
		}
		return
	}

	if m.rulesErr != nil {
		m.setError(m.rulesErr)
		return
	}
	rule := m.rules.SetRule(match, category)
	if err := m.store.SaveRules(m.rules); err != nil {
		m.setError(err)
		return
	}
	sweep, err := m.store.ApplyRule(rule, m.rules)
	if err != nil {
		m.setError(err)
		return
	}

	// The sweep rewrote months on disk, possibly this one.
	key := m.dash.key
	if m.dash.onDisk {
		m.openMonth(key)
		if entryID != "" {
			m.selectEntry(model.Expense, entryID)
		}
	}
	if m.screen == screenPayees {
		m.loadPayees()
		if m.payees.detail != nil {
			m.loadPayeeDetail()
		}
	}

	switch {
	case elsewhere != nil:
		m.setStatus("%s → %s · %s filed", label, shown, plural(sweep.Entries+1, "entry", "entries"))
	case entryID == "":
		// From the payees screen every filed entry is news, not "more".
		m.setStatus("%s → %s · %s filed", label, shown, plural(sweep.Entries, "entry", "entries"))
	case sweep.Entries == 0:
		m.setStatus("%s → %s · remembered for next time", label, shown)
	default:
		m.setStatus("%s → %s · %s filed", label, shown, plural(sweep.Entries, "more entry", "more entries"))
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

// catVisible is how many category rows the dialog shows at once.
const catVisible = 8

func (c *catState) view(t Theme, cw int) string {
	var lines []string

	// The list scrolls around the cursor; the "new" row is its last line.
	total := len(c.options) + 1
	start := 0
	if total > catVisible {
		start = min(max(0, c.cursor-catVisible/2), total-catVisible)
	}
	end := min(total, start+catVisible)
	for i := start; i < end; i++ {
		on := i == c.cursor
		caret, style := "  ", t.Label
		if on {
			caret, style = "› ", t.FieldOn
		}
		switch {
		case i == len(c.options):
			c.newName.SetWidth(max(10, cw-7))
			if on && c.focus == catList {
				lines = append(lines, caret+style.Render("+ ")+c.newName.View())
			} else if v := strings.TrimSpace(c.newName.Value()); v != "" {
				lines = append(lines, caret+style.Render("+ "+truncate(v, cw-4)))
			} else {
				lines = append(lines, caret+t.Muted.Render("+ new category"))
			}
		case c.options[i] == "":
			lines = append(lines, caret+style.Render("Uncategorised"))
		default:
			lines = append(lines, caret+style.Render(truncate(c.options[i], cw-2)))
		}
	}
	if start > 0 || end < total {
		lines = append(lines, t.Muted.Render("  "+itoa(c.cursor+1)+" of "+itoa(total)))
	}

	if c.hasRule() {
		c.match.SetWidth(max(10, cw-12))
		caret, style := "  ", t.Field
		if c.focus == catMatch {
			caret, style = "› ", t.FieldOn
		}
		lines = append(lines, "", caret+style.Render(padRight("Match", 9))+c.match.View())

		if c.entryID != "" {
			box, caret, style := "[ ]", "  ", t.Field
			if c.applyAll {
				box = "[×]"
			}
			if c.focus == catApply {
				caret, style = "› ", t.FieldOn
			}
			lines = append(lines, caret+style.Render(truncate(box+" Every expense whose payee contains this", cw-2)))
		} else {
			lines = append(lines, wrap(t.Muted, cw, "Every expense whose payee contains this text goes to the category, now and on later imports."))
		}
	}

	if c.err != "" {
		lines = append(lines, "", t.Danger.Render(truncate(c.err, cw)))
	}
	return strings.Join(lines, "\n")
}

// hints are the dialog's keys, shown in its bottom border.
func (c *catState) hints() string {
	if c.hasRule() {
		return "↑↓ choose · tab next · enter save · esc cancel"
	}
	return "↑↓ choose · enter save · esc cancel"
}
