package money

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Amount
	}{
		{"2800", 280000},
		{"2.800", 280000},        // '.' with three digits after is a thousands separator
		{"2800,50", 280050},      // ',' with two digits after is a decimal point
		{"2 800.50", 280050},     // spaces are separators too
		{"2800 lei", 280000},     // currency words are ignored
		{"12.500,00", 1250000},   // both separators: the rightmost decides
		{"1.234.567", 123456700}, // repeated separator is never a decimal point
		{"1,234", 123400},
		{"2,5", 250}, // one digit after the point means tenths
		{"2.5", 250},
		{"  40  ", 4000},
		{",50", 50},
		{"0", 0},
		{"-1500", -150000},
		{"−1.500,50", -150050}, // unicode minus
		{"+90", 9000},
		{"1450 RON", 145000},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q) returned error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseRejects(t *testing.T) {
	cases := []struct {
		in   string
		want error
	}{
		{"", ErrEmpty},
		{"   ", ErrEmpty},
		{"lei", ErrEmpty},
		{"1.2345", ErrDecimal},
		{"2.800,123", ErrDecimal},
		{"12345678901234", ErrTooBig},
	}
	for _, c := range cases {
		_, err := Parse(c.in)
		if err == nil {
			t.Errorf("Parse(%q) succeeded, want error", c.in)
			continue
		}
		if c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("Parse(%q) error = %v, want %v", c.in, err, c.want)
		}
	}
	for _, in := range []string{"abc", "12a", "1..2.3"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", in)
		}
	}
}

func TestString(t *testing.T) {
	cases := []struct {
		in   Amount
		want string
	}{
		{0, "0,00"},
		{50, "0,50"},
		{5, "0,05"},
		{280000, "2.800,00"},
		{1250000, "12.500,00"},
		{123456700, "1.234.567,00"},
		{100000, "1.000,00"},
		{-150050, "-1.500,50"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("Amount(%d).String() = %q, want %q", c.in, got, c.want)
		}
	}
	if got := Amount(280050).Lei(); got != "2.800,50 lei" {
		t.Errorf("Lei() = %q", got)
	}
}

// Every amount a user can type must survive display and re-parsing unchanged.
func TestParseStringRoundTrip(t *testing.T) {
	for _, a := range []Amount{0, 1, 50, 999, 280050, 1250000, -150050, -5, 123456789} {
		got, err := Parse(a.String())
		if err != nil {
			t.Errorf("Parse(%q) returned error: %v", a.String(), err)
			continue
		}
		if got != a {
			t.Errorf("round trip of %d via %q gave %d", a, a.String(), got)
		}
	}
}

func TestJSON(t *testing.T) {
	for _, a := range []Amount{0, 50, 280050, 1250000, -150050} {
		b, err := json.Marshal(a)
		if err != nil {
			t.Fatalf("marshal %d: %v", a, err)
		}
		var got Amount
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if got != a {
			t.Errorf("JSON round trip of %d gave %d (via %s)", a, got, b)
		}
	}

	if b, _ := json.Marshal(Amount(280050)); string(b) != "2800.50" {
		t.Errorf("marshal 280050 = %s, want 2800.50", b)
	}

	// Hand-edited files: shortened decimals, bare integers, quoted amounts.
	for _, c := range []struct {
		in   string
		want Amount
	}{
		{"2800.5", 280050},
		{"2800", 280000},
		{"0", 0},
		{"-1500.00", -150000},
		{`"2.800,50 lei"`, 280050},
	} {
		var got Amount
		if err := json.Unmarshal([]byte(c.in), &got); err != nil {
			t.Errorf("unmarshal %s: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("unmarshal %s = %d, want %d", c.in, got, c.want)
		}
	}

	for _, in := range []string{"2800.505", `"abc"`, "1e5", "true"} {
		var got Amount
		if err := json.Unmarshal([]byte(in), &got); err == nil {
			t.Errorf("unmarshal %s succeeded (%d), want error", in, got)
		}
	}
}

// Integer arithmetic must not drift the way float64 does.
func TestArithmeticIsExact(t *testing.T) {
	var sum Amount
	for range 10 {
		sum += 10 // 0.10 lei
	}
	if sum != 100 {
		t.Errorf("ten times 0,10 = %s, want 1,00", sum)
	}
}
