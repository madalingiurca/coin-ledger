package ui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/madalingiurca/budget/internal/model"
)

// renameState is the dialog that renames a category everywhere: in the list,
// in every payee rule, and in every month.
type renameState struct {
	from  string
	name  textinput.Model
	count int
	err   string
}

func newRename(t Theme, from string, count int) *renameState {
	r := &renameState{from: from, count: count}
	r.name = textinput.New()
	r.name.Prompt = ""
	r.name.CharLimit = 40
	r.name.SetValue(from)
	r.name.CursorEnd()
	r.name.Focus()
	r.restyle(t)
	return r
}

func (r *renameState) restyle(t Theme) { r.name.SetStyles(textinput.DefaultStyles(t.dark)) }

func (r *renameState) view(t Theme, cw int) string {
	r.name.SetWidth(max(10, cw-9))
	lines := []string{
		t.FieldOn.Render("› "+padRight("Name", 5)) + r.name.View(),
		"",
		wrap(t.Muted, cw, "Every entry in every month, and every payee rule, follows the new name. A name that already exists merges the two."),
	}
	if r.err != "" {
		lines = append(lines, "", t.Danger.Render(truncate(r.err, cw)))
	}
	lines = append(lines, "", t.Footer.Render(truncate("enter save · esc cancel", cw)))
	return strings.Join(lines, "\n")
}

// openRename opens the dialog for the category under the cursor.
func (m *Model) openRename(name string) {
	if name == "" || name == model.Transfers {
		return
	}
	var count int
	for _, c := range m.dash.month.Categories() {
		if c.Name == name {
			count = c.Count
		}
	}
	m.rename = newRename(m.theme, name, count)
}

func (m Model) renameKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r := m.rename
	switch msg.String() {
	case "esc":
		m.rename = nil
		return m, nil
	case "enter":
		to := strings.TrimSpace(r.name.Value())
		switch {
		case to == "":
			r.err = "Type a name."
		case strings.EqualFold(to, model.Transfers):
			r.err = "Transfers is built in; pick another name."
		default:
			m.rename = nil
			m.renameCategory(r.from, to)
			return m, nil
		}
		return m, nil
	}
	r.err = ""
	var cmd tea.Cmd
	r.name, cmd = r.name.Update(msg)
	return m, cmd
}

// renameCategory carries the rename out: rules first, then every month.
func (m *Model) renameCategory(from, to string) {
	if from == to {
		return
	}
	// A different spelling of another category is that category; changing
	// only the case of this one is a rename.
	if !strings.EqualFold(from, to) {
		to = m.rules.AddCategory(to)
	}
	if m.rulesErr != nil {
		m.setError(m.rulesErr)
		return
	}
	m.rules.RenameCategory(from, to)
	if err := m.store.SaveRules(m.rules); err != nil {
		m.setError(err)
		return
	}
	sweep, err := m.store.RenameCategory(from, to)
	if err != nil {
		m.setError(err)
		return
	}

	key := m.dash.key
	if m.dash.onDisk {
		m.openMonth(key)
	} else {
		m.dash.month.RenameCategory(from, to) // a month not saved yet is not on disk to sweep
	}
	delete(m.collapsed, from)
	for i, r := range m.dash.rows() {
		if r.header && r.cat == to {
			m.dash.cursor = i
		}
	}
	m.setStatus("%s → %s · %s renamed", from, to, plural(sweep.Entries, "entry", "entries"))
}
