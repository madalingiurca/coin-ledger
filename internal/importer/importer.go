// Package importer reads bank statements into transactions the budget can
// take in. It knows the export formats of the banks it supports and nothing
// about months, categories or files on disk: the caller decides where each
// transaction goes.
//
// Statements carry personal data (names, ID numbers, addresses, account
// numbers). A Statement keeps only what a budget needs: date, amount, a short
// description and a key to recognise the row again.
package importer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// ErrUnknownFormat reports a file that is not a statement of a supported bank.
var ErrUnknownFormat = errors.New("not an ING or Revolut statement export (CSV)")

// Txn is one transaction from a statement.
type Txn struct {
	// Date is the booking date, "2026-10-02". It decides the month.
	Date string
	// Kind is Income for money in, Expense for money out.
	Kind model.Kind
	// Amount is always positive; Kind says which way it went.
	Amount money.Amount
	// Label is the counterparty as the bank spells it, tidied.
	Label string
	// Payee is Label normalised for rule matching.
	Payee string
	// Type is the bank's own name for the kind of transaction.
	Type string
	// Key identifies the row: the same row in a later export of an
	// overlapping period gets the same key.
	Key string
}

// Imported turns the transaction into what a month takes in. The category is
// left for the caller to fill from the rules.
func (t Txn) Imported(source string) model.Imported {
	return model.Imported{
		Label:  t.Label,
		Amount: t.Amount,
		Date:   t.Date,
		Payee:  t.Payee,
		Source: source,
		Key:    t.Key,
	}
}

// Check is the statement checked against the bank's own balances.
type Check struct {
	// Done is false when the file carries nothing to check against.
	Done bool
	OK   bool
	// Note says what was compared, for showing to the user.
	Note string
}

// Statement is a parsed bank file.
type Statement struct {
	// Bank is the bank's display name; Source is what entries record.
	Bank   string
	Source string
	Txns   []Txn
	Check  Check
	// Skipped explains rows that were read but deliberately not imported.
	Skipped []string
}

// In and Out total the statement's money in and money out.
func (s Statement) In() money.Amount  { return s.total(model.Income) }
func (s Statement) Out() money.Amount { return s.total(model.Expense) }

func (s Statement) total(kind model.Kind) money.Amount {
	var sum money.Amount
	for _, t := range s.Txns {
		if t.Kind == kind {
			sum += t.Amount
		}
	}
	return sum
}

// Span returns the first and last booking dates in the statement.
func (s Statement) Span() (from, to string) {
	for _, t := range s.Txns {
		if from == "" || t.Date < from {
			from = t.Date
		}
		if t.Date > to {
			to = t.Date
		}
	}
	return from, to
}

// Parse recognises the bank from the file's contents and reads it.
func Parse(data []byte) (Statement, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // UTF-8 byte order mark
	switch {
	case isRevolut(data):
		return parseRevolut(data)
	case isING(data):
		return parseING(data)
	}
	return Statement{}, ErrUnknownFormat
}

// keyer makes row keys. A key is a hash of everything the bank says about the
// row, so it is stable across exports; two rows that really are identical get
// told apart by how many times the same hash has come up before in the file.
type keyer struct {
	bank string
	seen map[string]int
}

func newKeyer(bank string) *keyer { return &keyer{bank: bank, seen: map[string]int{}} }

func (k *keyer) key(parts ...string) string {
	sum := sha256.Sum256([]byte(k.bank + "\x00" + strings.Join(parts, "\x00")))
	h := hex.EncodeToString(sum[:8])
	k.seen[h]++
	if n := k.seen[h]; n > 1 {
		return fmt.Sprintf("%s-%d", h, n)
	}
	return h
}

// parseDecimal reads a bank amount strictly: digits, an optional thousands
// separator and exactly two decimals after decimalSep. Banks are not people;
// anything else means the format changed and must not be guessed at.
func parseDecimal(s string, decimalSep byte) (money.Amount, error) {
	t := strings.TrimSpace(s)
	neg := strings.HasPrefix(t, "-")
	t = strings.TrimPrefix(t, "-")
	thousands := "."
	if decimalSep == '.' {
		thousands = ","
	}
	t = strings.ReplaceAll(t, thousands, "")
	i := strings.IndexByte(t, decimalSep)
	if i < 1 || len(t)-i-1 != 2 {
		return 0, fmt.Errorf("amount %q is not in the expected form", s)
	}
	digits := t[:i] + t[i+1:]
	var v int64
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("amount %q is not in the expected form", s)
		}
		v = v*10 + int64(r-'0')
		if v > 1e15 {
			return 0, fmt.Errorf("amount %q is too large", s)
		}
	}
	if neg {
		v = -v
	}
	return money.Amount(v), nil
}

// tidy collapses runs of spaces, which bank exports pad names with.
func tidy(s string) string { return strings.Join(strings.Fields(s), " ") }
