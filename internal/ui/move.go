package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// moveState asks what to do when an entry moved to another month looks like
// something planned there: replace the plan, as an import would, or sit
// beside it.
type moveState struct {
	kind   model.Kind
	id     string
	label  string
	amount money.Amount
	target *model.Month
	match  model.Entry
}

// startMove moves the selected entry by n months: a salary paid early counts
// for the month after the one the bank booked it in.
func (m *Model) startMove(n int) {
	kind, entry, ok := m.dash.currentEntry()
	if !ok {
		return
	}
	to := m.dash.key.Shift(n)
	res, err := m.store.Get(to)
	if err != nil {
		m.setError(err)
		return
	}
	if !entry.IsForecast() {
		row := entry
		if entry.Planned > 0 {
			row, _ = m.dash.month.Clone().Take(kind, entry.ID)
		}
		if ms := res.Month.ForecastMatches(kind, row.AsImported(), m.rules); len(ms) > 0 {
			m.move = &moveState{kind: kind, id: entry.ID, label: entry.Label, amount: entry.Amount,
				target: res.Month, match: ms[0].Entry}
			return
		}
	}
	m.moveEntry(kind, entry.ID, res.Month, "")
}

func (m Model) moveKey(key string) (tea.Model, tea.Cmd) {
	mv := m.move
	switch key {
	case "r", "R", "enter", "y":
		m.move = nil
		m.moveEntry(mv.kind, mv.id, mv.target, mv.match.ID)
	case "k", "K":
		m.move = nil
		m.moveEntry(mv.kind, mv.id, mv.target, "")
	case "esc", "n", "N", "q":
		m.move = nil
	}
	return m, nil
}

func (m Model) movePrompt(cw int) string {
	mv := m.move
	t := m.theme
	return wrap(t.Label, cw, "Move "+mv.label+" ("+mv.amount.Lei()+") to "+mv.target.Key.Label()+"?") + "\n\n" +
		wrap(t.Muted, cw, "It looks like "+mv.match.Label+" ("+mv.match.Amount.Lei()+"), planned there. "+
			"Replace takes the bank's amount and date and keeps the plan's label; keep both adds it beside the plan.")
}

// moveEntry takes an entry out of the month on screen and puts it into
// target, replacing the forecast replace names when it is not empty. The
// target is written first: should the second write fail, the entry is in
// both months rather than in neither.
func (m *Model) moveEntry(kind model.Kind, id string, target *model.Month, replace string) {
	from := m.dash.key
	src := m.dash.month.Clone()
	e, err := src.Take(kind, id)
	if err != nil {
		m.setError(err)
		return
	}
	dst := target.Clone()
	if replace != "" {
		err = dst.ReplaceForecast(kind, replace, e.AsImported())
	} else {
		_, err = dst.Put(kind, e)
	}
	if err != nil {
		m.setError(err)
		return
	}
	if err := m.store.Save(dst); err != nil {
		m.setError(err)
		return
	}
	m.dash.month = src
	if !m.save() {
		return
	}
	m.dash.clamp()

	// A bank row moved to the month after its date teaches the payee to
	// count there; moved back home, the payee is forgotten.
	if t, err := time.Parse("2006-01-02", e.Date); err == nil && !e.IsForecast() && m.rulesErr == nil {
		booked := model.KeyOf(t)
		changed := false
		switch {
		case booked == from && dst.Key == from.Next():
			changed = m.rules.SetNextMonth(e.Payee, t.Day())
		case booked == dst.Key && from == booked.Next():
			changed = m.rules.DropNextMonth(e.Payee)
		}
		if changed {
			if err := m.store.SaveRules(m.rules); err != nil {
				m.setError(err)
				return
			}
		}
	}
	if replace != "" {
		m.setStatus("Moved %s to %s, in place of the plan", e.Label, dst.Key.Label())
		return
	}
	m.setStatus("Moved %s to %s", e.Label, dst.Key.Label())
}
