package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// confirmState is the delete prompt. It lives in the status line rather than a
// dialog: one question, one keypress.
type confirmState struct {
	kind   model.Kind
	id     string
	label  string
	amount money.Amount
}

func (m Model) confirmKey(key string) (tea.Model, tea.Cmd) {
	c := m.confirm
	switch key {
	case "y", "Y", "enter":
		m.confirm = nil
		if err := m.dash.month.Remove(c.kind, c.id); err != nil {
			m.setError(err)
			return m, nil
		}
		m.dash.clamp()
		if m.save() {
			m.setStatus("Deleted %s", c.label)
		}
	case "n", "N", "esc", "q":
		m.confirm = nil
	}
	return m, nil
}

func (m Model) confirmPrompt(cw int) string {
	c := m.confirm
	t := m.theme
	question := "Delete " + c.label + " (" + c.amount.Lei() + ")?"
	return t.Danger.Render(truncate(question, cw)) + " " +
		t.Key.Render("y") + t.Footer.Render(" / ") + t.Key.Render("n")
}
