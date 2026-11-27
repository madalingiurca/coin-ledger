package ui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/madalingiurca/budget/internal/money"
)

const (
	// contentMax keeps long lines readable on a wide terminal.
	contentMax = 68
	// contentMin is the narrowest layout that still makes sense; below it the
	// app drops decoration rather than wrapping into nonsense.
	contentMin = 34
	// amountCol is the fixed right-hand column every amount is aligned in.
	amountCol = 13
)

// contentWidth is the usable width inside the frame for a terminal of w
// columns. It never returns more than the terminal can hold: a frame wider
// than the screen tears every line it draws.
func contentWidth(w int) int {
	inner := w - paneChrome
	if inner > contentMax {
		inner = contentMax
	}
	if inner < contentMin {
		inner = contentMin
	}
	if fits := w - paneChrome; inner > fits {
		inner = max(1, fits)
	}
	return inner
}

// truncate shortens s to at most w display cells, marking the cut with an
// ellipsis. It measures cells, not bytes, so accented labels stay aligned.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	out := []rune(s)
	for len(out) > 0 && lipgloss.Width(string(out))+1 > w {
		out = out[:len(out)-1]
	}
	return string(out) + "…"
}

// padRight pads s with spaces to exactly w display cells.
func padRight(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return truncate(s, w)
}

// padLeft right-aligns s within w display cells.
func padLeft(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return truncate(s, w)
}

// spread puts left and right on one line of exactly w cells, pushed apart.
// It may truncate, so it takes plain text: slicing a styled string would cut
// an escape sequence in half.
func spread(left, right string, w int) (string, string) {
	rw := lipgloss.Width(right)
	if rw > w {
		return "", truncate(right, w)
	}
	return padRight(truncate(left, w-rw), w-rw), right
}

// wrap renders prose across as many lines as it needs. Sentences are wrapped
// rather than truncated: an explanation that loses its tail to an ellipsis has
// stopped explaining anything.
func wrap(style lipgloss.Style, w int, text string) string {
	return style.Width(max(1, w)).Render(text)
}

// rule draws a horizontal divider.
func rule(w int) string { return strings.Repeat("─", max(0, w)) }

// bar renders a proportion as a meter. pct is clamped into 0..100; a negative
// pct means overspending and fills from the left in the "over" style.
func bar(t Theme, w, pct int) string {
	if w <= 0 {
		return ""
	}
	over := pct < 0
	if over {
		pct = -pct
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct * w / 100
	if filled == 0 && pct > 0 {
		filled = 1 // never render a visible share as nothing at all
	}

	fill := t.BarFill
	if over {
		fill = t.BarOver
	}
	return fill.Render(strings.Repeat("█", filled)) +
		t.BarEmpty.Render(strings.Repeat("░", w-filled))
}

// hint is one "key action" pair in the key bar. The bar takes hints rather
// than rendered strings so it can leave one out when the terminal is too
// narrow to hold it; a styled string could not be cut safely.
type hint struct{ key, action string }

func keyHint(key, action string) hint { return hint{key: key, action: action} }

// itoa renders a small non-negative integer without pulling in strconv at
// every call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func absAmount(a money.Amount) money.Amount {
	if a < 0 {
		return -a
	}
	return a
}
