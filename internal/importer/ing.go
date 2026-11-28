package importer

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/madalingiurca/budget/internal/model"
	"github.com/madalingiurca/budget/internal/money"
)

// The ING "Extras de cont" CSV is the printed statement poured into cells.
// Each transaction is a start row (date in the first column, the type of
// transaction and an amount further right) followed by detail rows of the
// form "Key:Value". Every page repeats the account holder's name, ID number
// and address, the column headers and two signature rows; the last page adds
// the opening and closing balance. Pages differ in how many columns they
// have, so the debit and credit columns are found again at every header.
//
// From November 2026 the last page also lists the card payments that are
// still pending ("tranzactii in asteptare") under a header of their own:
// Data, Beneficiar, Valoare tranzactie. They are not booked yet and come
// back as ordinary rows in a later statement, so they are not imported.

var ingDate = regexp.MustCompile(`^(\d{1,2}) (\p{L}+) (\d{4})$`)

var ingMonths = map[string]int{
	"ianuarie": 1, "februarie": 2, "martie": 3, "aprilie": 4, "mai": 5, "iunie": 6,
	"iulie": 7, "august": 8, "septembrie": 9, "octombrie": 10, "noiembrie": 11, "decembrie": 12,
}

// isING looks for the statement's column header row.
func isING(data []byte) bool {
	head := data[:min(len(data), 4096)]
	return bytes.Contains(head, []byte("Detalii tranzactie")) &&
		bytes.Contains(head, []byte("Debit")) && bytes.Contains(head, []byte("Credit"))
}

// ingRow is a transaction while its detail rows are still being read.
type ingRow struct {
	line    int
	date    string
	typ     string
	amount  money.Amount
	credit  bool
	details []ingDetail
}

type ingDetail struct{ key, value string }

func (r *ingRow) detail(key string) string {
	for _, d := range r.details {
		if d.key == key {
			return d.value
		}
	}
	return ""
}

func parseING(data []byte) (Statement, error) {
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	records, err := cr.ReadAll()
	if err != nil {
		return Statement{}, fmt.Errorf("reading the ING file: %w", err)
	}

	st := Statement{Bank: "ING", Source: model.SourceING}
	var (
		rows              []*ingRow
		cur               *ingRow
		debitCol          = -1
		creditCol         = -1
		opening, closing  money.Amount
		hasOpen, hasClose bool
		pending           bool // inside the pending card payments table
		pendingRows       int
		pendingSum        money.Amount
	)
	cell := func(rec []string, i int) string {
		if i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}

	for n, rec := range records {
		line := n + 1
		first := cell(rec, 0)

		// Column header: where debit and credit sit on this page.
		if d, c := index(rec, "Debit"), index(rec, "Credit"); d >= 0 && c > d {
			debitCol, creditCol = d, c
			pending = false
			continue
		}

		// Header of the pending card payments table.
		if index(rec, "Beneficiar") >= 0 && hasPrefix(rec, "Valoare tranzac") {
			pending, cur = true, nil
			continue
		}

		// Balances at the end of the statement.
		if strings.HasPrefix(first, "Sold ini") || strings.HasPrefix(first, "Sold final") {
			v, ok, err := firstAmount(rec[1:])
			if err != nil {
				return Statement{}, fmt.Errorf("ING line %d: %w", line, err)
			}
			if ok && strings.HasPrefix(first, "Sold ini") {
				opening, hasOpen = v, true
			} else if ok {
				closing, hasClose = v, true
			}
			continue
		}

		// A new transaction.
		if m := ingDate.FindStringSubmatch(first); m != nil && pending {
			if v, ok, err := lastAmount(rec[1:]); err != nil {
				return Statement{}, fmt.Errorf("ING line %d: %w", line, err)
			} else if ok {
				pendingSum += v
			}
			pendingRows++
			continue
		}
		if m := ingDate.FindStringSubmatch(first); m != nil {
			date, err := ingDateOf(m)
			if err != nil {
				return Statement{}, fmt.Errorf("ING line %d: %w", line, err)
			}
			if debitCol < 0 {
				return Statement{}, fmt.Errorf("ING line %d: a transaction before any Debit/Credit header", line)
			}
			typeCol := -1
			for i := 1; i < len(rec); i++ {
				if cell(rec, i) != "" {
					typeCol = i
					break
				}
			}
			if typeCol < 0 {
				return Statement{}, fmt.Errorf("ING line %d: a date with no transaction", line)
			}
			amtCol := -1
			for i := typeCol + 1; i < len(rec); i++ {
				if cell(rec, i) != "" {
					if amtCol >= 0 {
						return Statement{}, fmt.Errorf("ING line %d: more than one amount", line)
					}
					amtCol = i
				}
			}
			if amtCol < 0 {
				return Statement{}, fmt.Errorf("ING line %d: a transaction with no amount", line)
			}
			credit, ok := ingSide(amtCol, debitCol, creditCol)
			if !ok {
				return Statement{}, fmt.Errorf("ING line %d: cannot tell whether the amount is a debit or a credit", line)
			}
			amount, err := parseDecimal(cell(rec, amtCol), ',')
			if err != nil {
				return Statement{}, fmt.Errorf("ING line %d: %w", line, err)
			}
			cur = &ingRow{line: line, date: date, typ: tidy(cell(rec, typeCol)), amount: amount, credit: credit}
			rows = append(rows, cur)
			continue
		}

		// Detail rows have only the details column filled in. Anything else
		// (holder block, signatures, the bank's name) is page furniture.
		if cur == nil || first != "" || cell(rec, 1) != "" || cell(rec, 2) != "" {
			continue
		}
		text := ""
		for i := 3; i < len(rec); i++ {
			if v := cell(rec, i); v != "" {
				text = v
				break
			}
		}
		if text == "" {
			continue
		}
		if k, v, ok := strings.Cut(text, ":"); ok && !strings.Contains(k, " ") || ok && ingDetailKey(k) {
			cur.details = append(cur.details, ingDetail{key: tidy(k), value: tidy(v)})
		} else if len(cur.details) > 0 {
			// A value that wrapped onto the next row.
			last := &cur.details[len(cur.details)-1]
			last.value = tidy(last.value + " " + text)
		}
	}

	if len(rows) == 0 {
		return Statement{}, fmt.Errorf("the ING file has no transactions")
	}
	if pendingRows > 0 {
		st.Skipped = append(st.Skipped, fmt.Sprintf("%s pending (%s), not booked yet",
			plural(pendingRows, "card payment is", "card payments are"), pendingSum.Lei()))
	}

	keys := newKeyer(model.SourceING)
	var net money.Amount
	for _, r := range rows {
		label := ingLabel(r)
		kind := model.Expense
		if r.credit {
			kind = model.Income
			net += r.amount
		} else {
			net -= r.amount
		}
		parts := []string{r.date, strconv.FormatBool(r.credit), r.amount.String(), r.typ}
		for _, d := range r.details {
			parts = append(parts, d.key+"="+d.value)
		}
		st.Txns = append(st.Txns, Txn{
			Date:   r.date,
			Kind:   kind,
			Amount: r.amount,
			Label:  label,
			Payee:  model.NormalizePayee(label),
			Type:   r.typ,
			Key:    keys.key(parts...),
		})
	}

	if hasOpen && hasClose {
		st.Check.Done = true
		st.Check.OK = opening+net == closing
		st.Check.Note = fmt.Sprintf("opening %s + transactions %s = closing %s",
			opening.Lei(), signed(net), (opening + net).Lei())
		if !st.Check.OK {
			st.Check.Note = fmt.Sprintf("transactions add up to %s but the balance moved %s",
				signed(net), signed(closing-opening))
		}
	}
	return st, nil
}

// ingSide decides whether an amount in column i is a credit. In the exports
// seen so far the amount sits one column left of its header; the header
// column itself is accepted too, in case a page lines up.
func ingSide(i, debitCol, creditCol int) (credit, ok bool) {
	switch i {
	case creditCol - 1:
		return true, true
	case debitCol - 1:
		return false, true
	case creditCol:
		return true, true
	case debitCol:
		return false, true
	}
	return false, false
}

// ingDetailKey lists the multi-word detail keys, so a wrapped value that
// happens to contain a colon is not mistaken for a new detail.
func ingDetailKey(k string) bool {
	switch tidy(k) {
	case "Numar card", "Data autorizarii", "Numar autorizare", "In contul", "Din contul",
		"Platita la", "Tranzactie la", "Suma transmisa spre decontare", "Rata ING", "Cod Fiscal Platitor":
		return true
	}
	return false
}

func ingDateOf(m []string) (string, error) {
	day, _ := strconv.Atoi(m[1])
	month, ok := ingMonths[strings.ToLower(m[2])]
	if !ok || day < 1 || day > 31 {
		return "", fmt.Errorf("date %q is not a date", m[0])
	}
	return fmt.Sprintf("%s-%02d-%02d", m[3], month, day), nil
}

// ingCity strips the country code and city ING appends to a card terminal:
// "FAST MARKET RO BUCURESTI" is "FAST MARKET".
var ingCity = regexp.MustCompile(`^(.+)\s+[A-Z]{2}\s+\S.*$`)

// ingLabel names the counterparty of a transaction.
func ingLabel(r *ingRow) string {
	if r.typ == "Tranzactie Round Up" {
		return "Round Up" // the shop it rounded up from is not where it went
	}
	if r.typ == "Retragere numerar" {
		return "Cash withdrawal" // the terminal is the ATM, not a payee
	}
	// The card terminal: "Terminal" up to October 2026, "Tranzactie la" after.
	t := r.detail("Terminal")
	if t == "" {
		t = r.detail("Tranzactie la")
	}
	if t != "" {
		if m := ingCity.FindStringSubmatch(t); m != nil {
			t = m[1]
		}
		return shopName(t)
	}
	for _, key := range []string{"Beneficiar", "Ordonator"} {
		if v := r.detail(key); v != "" {
			return v
		}
	}
	return r.typ
}

// ingOrder is an order number glued to a merchant's name, as Bolt does:
// "BOLT.EUO2601010001" and "BOLT.EU/O/2601020002" are both "BOLT.EU".
var ingOrder = regexp.MustCompile(`(?:/?O)?/?\d{6,}$`)

// shopName drops what makes one shop look like many: a store or branch
// number and whatever follows it ("MOL 91022 Bucuresti", "PENNY 4498 BUC
// BDTIMISOAR"), a booking reference ("RYANAIR Q47ZRT2") and an order number.
// The first word is always kept, so a name that is only digits survives.
func shopName(s string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	keep := []string{words[0]}
	for _, w := range words[1:] {
		if allDigits(w) || isReference(w) {
			break
		}
		keep = append(keep, w)
	}
	if name := ingOrder.ReplaceAllString(keep[0], ""); name != "" {
		keep[0] = name
	}
	return strings.Join(keep, " ")
}

func allDigits(w string) bool {
	for _, r := range w {
		if r < '0' || r > '9' {
			return false
		}
	}
	return w != ""
}

// isReference reports a code of letters and digits, such as a booking
// reference, as opposed to a word or a short code like "C2" or "16B".
func isReference(w string) bool {
	if len(w) < 6 {
		return false
	}
	var letters, digits bool
	for _, r := range w {
		switch {
		case r >= '0' && r <= '9':
			digits = true
		case unicode.IsLetter(r):
			letters = true
		}
	}
	return letters && digits
}

func index(rec []string, want string) int {
	for i, c := range rec {
		if strings.TrimSpace(c) == want {
			return i
		}
	}
	return -1
}

// hasPrefix reports whether any cell starts with prefix.
func hasPrefix(rec []string, prefix string) bool {
	for _, c := range rec {
		if strings.HasPrefix(strings.TrimSpace(c), prefix) {
			return true
		}
	}
	return false
}

// lastAmount reads the rightmost filled-in cell as an amount.
func lastAmount(rec []string) (money.Amount, bool, error) {
	for i := len(rec) - 1; i >= 0; i-- {
		if c := strings.TrimSpace(rec[i]); c != "" {
			v, err := parseDecimal(c, ',')
			return v, err == nil, err
		}
	}
	return 0, false, nil
}

func firstAmount(rec []string) (money.Amount, bool, error) {
	for _, c := range rec {
		if c = strings.TrimSpace(c); c != "" {
			v, err := parseDecimal(c, ',')
			return v, err == nil, err
		}
	}
	return 0, false, nil
}

// signed writes an amount with its sign. A minus sign rather than a hyphen,
// so that wrapping the note never splits it from its number.
func signed(a money.Amount) string {
	if a >= 0 {
		return "+" + a.Lei()
	}
	return "−" + (-a).Lei()
}
