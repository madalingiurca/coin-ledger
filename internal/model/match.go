package model

import (
	"strings"

	"github.com/madalingiurca/budget/internal/money"
)

// MatchReason says why a forecast entry is offered as the one a bank row
// fulfils.
type MatchReason int

const (
	// MatchLinked: the user replaced this forecast with the same payee before.
	MatchLinked MatchReason = iota
	// MatchAmount: the forecast and the bank row are for the same amount.
	MatchAmount
	// MatchClose: the amounts are within closeShare of each other, as a
	// salary or a bill that varies a little from month to month.
	MatchClose
)

// closeShare is how near, in percent of the forecast, a bank amount must be
// to be offered as its actual; closeFloor keeps small forecasts out, where
// any coffee would be "close".
const (
	closeShare = 10
	closeFloor = 100_00 // 100 lei, in bani
)

func isClose(forecast, actual money.Amount) bool {
	if forecast < closeFloor || actual <= 0 {
		return false
	}
	d := forecast - actual
	if d < 0 {
		d = -d
	}
	return d*100 <= forecast*closeShare
}

// Match is a forecast entry a bank row may fulfil.
type Match struct {
	Entry  Entry
	Reason MatchReason
}

// ForecastMatches lists the forecast entries of one kind that imp may be the
// actual for, most convincing first: entries the payee has replaced before,
// then entries for exactly the same amount, then entries for a close one. Nothing is changed; the caller asks
// the user and then calls ReplaceForecast or AddImported.
func (m *Month) ForecastMatches(kind Kind, imp Imported, rules *Rules) []Match {
	var linked, sameAmount, close []Match
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
		case isClose(e.Amount, imp.Amount):
			close = append(close, Match{Entry: e, Reason: MatchClose})
		}
	}
	return append(append(linked, sameAmount...), close...)
}
