package ui

import "github.com/madalingiurca/budget/internal/model"

// togglePaidBack flips whether a category's expenses are expected to come
// back. Its expenses with no refund linked then show as awaiting one.
func (m *Model) togglePaidBack(category string) {
	switch {
	case m.rulesErr != nil:
		m.setError(m.rulesErr)
		return
	case category == model.Transfers:
		m.setStatus("Transfers are never paid back")
		return
	}
	on := m.rules.TogglePaidBack(category)
	if err := m.store.SaveRules(m.rules); err != nil {
		m.setError(err)
		return
	}
	if on {
		m.setStatus("%s: expenses are paid back later; those with no refund show as open", category)
		return
	}
	m.setStatus("%s: expenses are no longer expected back", category)
}
