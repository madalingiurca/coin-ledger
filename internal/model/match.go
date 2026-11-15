package model

import "strings"

// MatchReason says why a forecast entry is offered as the one a bank row
// fulfils.
type MatchReason int

const (
	// MatchLinked: the user replaced this forecast with the same payee before.
	MatchLinked MatchReason = iota
	// MatchAmount: the forecast and the bank row are for the same amount.
	MatchAmount
)

// Match is a forecast entry a bank row may fulfil.
type Match struct {
	Entry  Entry
	Reason MatchReason
}

// ForecastMatches lists the forecast entries of one kind that imp may be the
// actual for, most convincing first: entries the payee has replaced before,
// then entries for exactly the same amount. Nothing is changed; the caller asks
// the user and then calls ReplaceForecast or AddImported.
func (m *Month) ForecastMatches(kind Kind, imp Imported, rules *Rules) []Match {
	var linked, sameAmount []Match
	label, hasLink := "", false
	if rules != nil {
		label, hasLink = rules.LinkFor(imp.Payee)
	}
	for _, e := range m.Entries(kind) {
		if !e.IsForecast() || e.IsTransfer() {
			continue
		}
		switch {
		case hasLink && strings.EqualFold(strings.TrimSpace(e.Label), label):
			linked = append(linked, Match{Entry: e, Reason: MatchLinked})
		case e.Amount == imp.Amount:
			sameAmount = append(sameAmount, Match{Entry: e, Reason: MatchAmount})
		}
	}
	return append(linked, sameAmount...)
}
