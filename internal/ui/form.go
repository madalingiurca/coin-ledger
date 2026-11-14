package ui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// form field order.
const (
	fieldLabel = iota
	fieldAmount
	fieldRecurring
	fieldCount
)

// formState is the add/edit modal. One modal serves both because an entry and
// a new entry have the same shape.
type formState struct {
	kind model.Kind
	// id is empty when adding.
	id        string
	label     textinput.Model
	amount    textinput.Model
	recurring bool
	focus     int
	err       string
}

func newForm(t Theme, kind model.Kind, editing *model.Entry) *formState {
	f := &formState{kind: kind}

	f.label = textinput.New()
	f.label.Prompt = ""
	f.label.CharLimit = 60
	f.label.Placeholder = placeholderLabel(kind)

	f.amount = textinput.New()
	f.amount.Prompt = ""
	f.amount.CharLimit = 20
	f.amount.Placeholder = "0,00"

	if editing != nil {
		f.id = editing.ID
		f.label.SetValue(editing.Label)
		f.amount.SetValue(editing.Amount.String())
		f.recurring = editing.Recurring
	}

	f.setFocus(fieldLabel)
	f.restyle(t)
	return f
}

func placeholderLabel(kind model.Kind) string {
	if kind == model.Income {
		return "Salary"
	}
	return "Rent"
}

// restyle re-resolves the inputs for a light or dark terminal.
func (f *formState) restyle(t Theme) {
	styles := textinput.DefaultStyles(t.dark)
	f.label.SetStyles(styles)
	f.amount.SetStyles(styles)
}

func (f *formState) setFocus(field int) {
	f.focus = ((field % fieldCount) + fieldCount) % fieldCount
	f.label.Blur()
	f.amount.Blur()
	switch f.focus {
	case fieldLabel:
		f.label.Focus()
	case fieldAmount:
		f.amount.Focus()
	}
}

// parse validates the fields, returning the values to commit.
func (f *formState) parse() (string, money.Amount, bool) {
	label := strings.TrimSpace(f.label.Value())
	if label == "" {
		f.err = "Give it a name."
		f.setFocus(fieldLabel)
		return "", 0, false
	}
	amount, err := money.Parse(f.amount.Value())
	if err != nil {
		f.err = capitalize(err.Error()) + "."
		f.setFocus(fieldAmount)
		return "", 0, false
	}
	if amount == 0 {
		f.err = "An amount of zero would not change anything."
		f.setFocus(fieldAmount)
		return "", 0, false
	}
	if amount < 0 {
		f.err = "Enter a positive amount; expenses are already subtracted."
		f.setFocus(fieldAmount)
		return "", 0, false
	}
	return label, amount, true
}

func (m Model) formKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := m.form
	switch msg.String() {
	case "esc":
		m.form = nil
		return m, nil

	case "tab", "down":
		f.setFocus(f.focus + 1)
		return m, nil
	case "shift+tab", "up":
		f.setFocus(f.focus - 1)
		return m, nil

	case "space", " ":
		if f.focus == fieldRecurring {
			f.recurring = !f.recurring
			return m, nil
		}

	case "enter":
		label, amount, ok := f.parse()
		if !ok {
			return m, nil
		}
		kind, id, recurring := f.kind, f.id, f.recurring
		m.form = nil
		m.commitForm(kind, id, label, amount, recurring)
		return m, nil
	}

	f.err = ""
	var cmd tea.Cmd
	switch f.focus {
	case fieldLabel:
		f.label, cmd = f.label.Update(msg)
	case fieldAmount:
		f.amount, cmd = f.amount.Update(msg)
	}
	return m, cmd
}

func (f *formState) view(t Theme, cw int) string {
	title := "Add "
	if f.id != "" {
		title = "Edit "
	}
	title += f.kind.String()

	fieldW := max(10, cw-10)
	f.label.SetWidth(fieldW)
	f.amount.SetWidth(fieldW)

	lines := []string{
		t.Title.Render(title),
		"",
		f.fieldLine(t, "Label", f.label.View(), f.focus == fieldLabel),
		f.fieldLine(t, "Amount", f.amount.View(), f.focus == fieldAmount),
		"",
		f.recurringLine(t, cw),
	}
	if f.err != "" {
		lines = append(lines, "", t.Danger.Render(truncate(f.err, cw)))
	}
	lines = append(lines, "", t.Footer.Render(truncate(
		"tab next · space toggle · enter save · esc cancel", cw)))
	return strings.Join(lines, "\n")
}

func (f *formState) fieldLine(t Theme, name, input string, focused bool) string {
	style := t.Field
	caret := "  "
	if focused {
		style = t.FieldOn
		caret = "› "
	}
	return caret + style.Render(padRight(name, 8)) + input
}

func (f *formState) recurringLine(t Theme, cw int) string {
	box, caret, style := "[ ]", "  ", t.Field
	if f.recurring {
		box = "[×]"
	}
	if f.focus == fieldRecurring {
		caret, style = "› ", t.FieldOn
	}
	text := box + " Recurring"
	hint := " — carries into every new month"
	if len(text)+len(hint)+2 > cw {
		hint = ""
	}
	return caret + style.Render(text) + t.Muted.Render(hint)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
