// Package money represents RON amounts as exact integer counts of bani.
//
// Amounts are never held as float64: every value entering the program (typed by
// the user or decoded from a month file) is parsed from its decimal text into
// int64 bani, and every value leaving is rendered back from that integer. All
// arithmetic in between is integer arithmetic.
package money

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Amount is a quantity of bani. 1 leu == 100 bani.
type Amount int64

// maxLei bounds parsing so that Amount arithmetic cannot silently overflow.
const maxLei = 1_000_000_000_000 // 1e12 lei

var (
	ErrEmpty   = errors.New("enter an amount")
	ErrTooBig  = errors.New("amount is too large")
	ErrDecimal = errors.New("use at most two decimals")
)

// FromLei builds an Amount from whole lei. Useful in tests and defaults.
func FromLei(lei int64) Amount { return Amount(lei * 100) }

// Parse reads an amount the way a person types it. It is deliberately lenient:
// "2800", "2.800", "2800,50", "2 800.50" and "2800 lei" all parse to the same
// value.
//
// Both '.' and ',' may act as either separator. When both appear, the rightmost
// is the decimal point. When only one kind appears, it is a decimal point only
// if it occurs once with one or two digits after it — so "2.800" is two
// thousand eight hundred, while "2,50" is two and a half.
func Parse(s string) (Amount, error) {
	t := s

	// Drop currency words and any spacing used as a thousands separator.
	for _, word := range []string{"lei", "LEI", "Lei", "RON", "ron", "Ron"} {
		t = strings.ReplaceAll(t, word, "")
	}
	for _, space := range []string{" ", "\t", " ", " ", " "} {
		t = strings.ReplaceAll(t, space, "")
	}

	neg := false
	switch {
	case strings.HasPrefix(t, "-"), strings.HasPrefix(t, "−"):
		neg = true
		t = strings.TrimPrefix(strings.TrimPrefix(t, "-"), "−")
	case strings.HasPrefix(t, "+"):
		t = strings.TrimPrefix(t, "+")
	}

	if t == "" {
		return 0, ErrEmpty
	}
	for _, r := range t {
		if (r < '0' || r > '9') && r != '.' && r != ',' {
			return 0, fmt.Errorf("%q is not a number", strings.TrimSpace(s))
		}
	}

	whole, frac, err := split(t)
	if err != nil {
		return 0, err
	}
	return assemble(whole, frac, neg)
}

// split decides which separator (if any) is the decimal point and returns the
// digits on either side of it. Whatever separators remain in the integer part
// must form well-shaped thousands groups, so "1.2345" and "1..2.3" are
// rejected rather than quietly read as 123,45 and 1,23.
func split(t string) (whole, frac string, err error) {
	lastDot, lastComma := strings.LastIndex(t, "."), strings.LastIndex(t, ",")

	sep := -1
	switch {
	case lastDot >= 0 && lastComma >= 0:
		sep = max(lastDot, lastComma)
	case lastDot >= 0:
		sep = decimalIf(t, lastDot, '.')
	case lastComma >= 0:
		sep = decimalIf(t, lastComma, ',')
	}

	intPart := t
	if sep >= 0 {
		intPart = t[:sep]
		frac = t[sep+1:]
		if len(frac) > 2 {
			return "", "", ErrDecimal
		}
		if strings.ContainsAny(frac, ".,") {
			return "", "", notANumber(t)
		}
	}

	whole, ok := grouped(intPart)
	if !ok {
		// A single separator that failed as a thousands mark was almost
		// certainly meant as a decimal point with too many digits after it.
		if sep < 0 && strings.Count(t, ".")+strings.Count(t, ",") == 1 {
			return "", "", ErrDecimal
		}
		return "", "", notANumber(t)
	}
	return whole, frac, nil
}

// decimalIf reports the index of sep when it reads as a decimal point rather
// than a thousands separator, or -1 otherwise.
func decimalIf(t string, idx int, sep rune) int {
	if strings.Count(t, string(sep)) != 1 {
		return -1
	}
	if n := len(t) - idx - 1; n < 1 || n > 2 {
		return -1
	}
	return idx
}

// grouped validates the integer part and returns it stripped of separators. A
// separator is only legitimate there as a thousands mark: one separator
// character used consistently, a leading group of one to three digits, and
// every later group exactly three.
func grouped(s string) (string, bool) {
	if s == "" {
		return "", true
	}
	hasDot, hasComma := strings.Contains(s, "."), strings.Contains(s, ",")
	if hasDot && hasComma {
		return "", false // mixed thousands separators
	}
	if !hasDot && !hasComma {
		return s, allDigits(s)
	}

	sep := ","
	if hasDot {
		sep = "."
	}
	parts := strings.Split(s, sep)
	for i, part := range parts {
		if !allDigits(part) {
			return "", false
		}
		switch {
		case i == 0 && (len(part) < 1 || len(part) > 3):
			return "", false
		case i > 0 && len(part) != 3:
			return "", false
		}
	}
	return strings.Join(parts, ""), true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func notANumber(s string) error { return fmt.Errorf("%q is not a number", s) }

func assemble(whole, frac string, neg bool) (Amount, error) {
	if whole == "" && frac == "" {
		return 0, ErrEmpty
	}
	if whole == "" {
		whole = "0"
	}
	lei, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || lei > maxLei {
		return 0, ErrTooBig
	}

	bani := int64(0)
	if frac != "" {
		f, err := strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a number", frac)
		}
		if len(frac) == 1 {
			f *= 10 // "2,5" means fifty bani, not five
		}
		bani = f
	}

	total := lei*100 + bani
	if neg {
		total = -total
	}
	return Amount(total), nil
}

// String renders the amount in Romanian convention, without a currency suffix:
// "12.500,00", "-1.500,50".
func (a Amount) String() string {
	sign := ""
	n := int64(a)
	if n < 0 {
		sign, n = "-", -n
	}
	return fmt.Sprintf("%s%s,%02d", sign, group(n/100), n%100)
}

// Lei renders the amount with its currency suffix: "12.500,00 lei".
func (a Amount) Lei() string { return a.String() + " lei" }

// group inserts '.' every three digits: 12500 -> "12.500".
func group(lei int64) string {
	d := strconv.FormatInt(lei, 10)
	if len(d) <= 3 {
		return d
	}
	var b strings.Builder
	lead := len(d) % 3
	if lead > 0 {
		b.WriteString(d[:lead])
	}
	for i := lead; i < len(d); i += 3 {
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(d[i : i+3])
	}
	return b.String()
}

// plain renders the amount as a bare JSON number: "2800.50", "-1500.00".
func (a Amount) plain() string {
	sign := ""
	n := int64(a)
	if n < 0 {
		sign, n = "-", -n
	}
	return fmt.Sprintf("%s%d.%02d", sign, n/100, n%100)
}

// MarshalJSON writes the amount as a readable decimal number so month files
// stay hand-editable.
func (a Amount) MarshalJSON() ([]byte, error) { return []byte(a.plain()), nil }

// UnmarshalJSON accepts either a JSON number (2800.5) or a quoted amount
// ("2.800,50 lei"), so a hand-edited file is forgiving.
func (a *Amount) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*a = 0
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		unquoted, err := strconv.Unquote(s)
		if err != nil {
			return fmt.Errorf("amount %s: %w", s, err)
		}
		parsed, err := Parse(unquoted)
		if err != nil {
			return fmt.Errorf("amount %s: %w", s, err)
		}
		*a = parsed
		return nil
	}

	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, found := strings.Cut(s, ".")
	if !found {
		frac = ""
	}
	if len(frac) > 2 {
		return fmt.Errorf("amount %s: %w", string(b), ErrDecimal)
	}
	for _, part := range []string{whole, frac} {
		for _, r := range part {
			if r < '0' || r > '9' {
				return fmt.Errorf("amount %s is not a plain decimal number", string(b))
			}
		}
	}
	parsed, err := assemble(whole, frac, neg)
	if err != nil {
		return fmt.Errorf("amount %s: %w", string(b), err)
	}
	*a = parsed
	return nil
}
