package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// form fields. Income has no category, so the fields a form shows are listed
// per form rather than assumed.
const (
	fieldLabel = iota
	fieldAmount
	fieldCategory
	fieldRecurring
)

// formState is the add/edit modal. One modal serves both because an entry and
// a new entry have the same shape.
type formState struct {
	kind model.Kind
	// id is empty when adding.
	id        string
	label     textinput.Model
	amount    textinput.Model
	category  textinput.Model
	recurring bool
	// fields are the fields this form has, in tab order; focus is one of them.
	fields []int
	focus  int
	err    string
}

// newForm opens the modal. category prefills the category of an expense and
// categories are the names offered as completions while typing one.
func newForm(t Theme, kind model.Kind, editing *model.Entry, category string, categories []string) *formState {
	f := &formState{kind: kind, fields: []int{fieldLabel, fieldAmount, fieldRecurring}}
	if kind == model.Expense {
		f.fields = []int{fieldLabel, fieldAmount, fieldCategory, fieldRecurring}
	}

	f.category = textinput.New()
	f.category.Prompt = ""
	f.category.CharLimit = 40
	f.category.Placeholder = "none"
	f.category.ShowSuggestions = true
	f.category.SetSuggestions(categories)
	// Tab moves between fields here, so a completion is taken with →.
	f.category.KeyMap.AcceptSuggestion = key.NewBinding(key.WithKeys("right"))
	f.category.SetValue(category)

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
	f.category.SetStyles(styles)
}

func (f *formState) setFocus(field int) {
	f.focus = field
	f.label.Blur()
	f.amount.Blur()
	f.category.Blur()
	switch f.focus {
	case fieldLabel:
		f.label.Focus()
	case fieldAmount:
		f.amount.Focus()
	case fieldCategory:
		f.category.Focus()
	}
}

// step moves the focus by n fields, wrapping round.
func (f *formState) step(n int) {
	at := 0
	for i, field := range f.fields {
		if field == f.focus {
			at = i
		}
	}
	f.setFocus(f.fields[((at+n)%len(f.fields)+len(f.fields))%len(f.fields)])
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
		f.step(1)
		return m, nil
	case "shift+tab", "up":
		f.step(-1)
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
		kind, id, recurring, category := f.kind, f.id, f.recurring, f.category.Value()
		m.form = nil
		m.commitForm(kind, id, label, amount, recurring, category)
		return m, nil
	}

	f.err = ""
	var cmd tea.Cmd
	switch f.focus {
	case fieldLabel:
		f.label, cmd = f.label.Update(msg)
	case fieldAmount:
		f.amount, cmd = f.amount.Update(msg)
	case fieldCategory:
		f.category, cmd = f.category.Update(msg)
	}
	return m, cmd
}

// title names the dialog: what it adds or edits.
func (f *formState) title() string {
	if f.id != "" {
		return "Edit " + f.kind.String()
	}
	return "Add " + f.kind.String()
}

func (f *formState) view(t Theme, cw int) string {
	fieldW := max(10, cw-12)
	f.label.SetWidth(fieldW)
	f.amount.SetWidth(fieldW)
	f.category.SetWidth(fieldW)

	lines := []string{
		f.fieldLine(t, "Label", f.label.View(), f.focus == fieldLabel),
		f.fieldLine(t, "Amount", f.amount.View(), f.focus == fieldAmount),
	}
	if f.kind == model.Expense {
		lines = append(lines, f.fieldLine(t, "Category", f.category.View(), f.focus == fieldCategory))
	}
	lines = append(lines, "", f.recurringLine(t, cw))
	if f.err != "" {
		lines = append(lines, "", t.Danger.Render(truncate(f.err, cw)))
	}
	return strings.Join(lines, "\n")
}

// hints are the dialog's keys, shown in its bottom border.
func (f *formState) hints() string {
	if f.focus == fieldCategory {
		return "→ complete · tab next · enter save · esc cancel"
	}
	return "tab next · space toggle · enter save · esc cancel"
}

func (f *formState) fieldLine(t Theme, name, input string, focused bool) string {
	style := t.Field
	caret := "  "
	if focused {
		style = t.FieldOn
		caret = "› "
	}
	return caret + style.Render(padRight(name, 9)) + input
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
