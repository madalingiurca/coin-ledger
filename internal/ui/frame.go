package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// paneChrome is what a pane costs around its content: a border column and a
// column of padding on each side.
const paneChrome = 4

// pane is one bordered box on screen. Its title sits in the top border and
// its counter ("3 of 20") in the bottom one, so neither costs a row.
type pane struct {
	title   string
	counter string
	// hints, when set, sit in the bottom border in place of a counter. A
	// popup uses them for its own keys.
	hints string
	body  string
	// w and h are the outer size. h of zero fits the body.
	w, h    int
	focused bool
}

// render draws the pane. Body lines wider than the pane are cut rather than
// wrapped, since the caller laid them out for exactly this width; extra lines
// are dropped, and a short body is padded out to h.
func (p pane) render(t Theme) string {
	w := max(paneChrome, p.w)
	cw := w - paneChrome

	border, title := t.BorderInactive, t.TitleInactive
	if p.focused {
		border, title = t.BorderActive, t.TitleActive
	}

	lines := strings.Split(p.body, "\n")
	if p.body == "" {
		lines = nil
	}
	if p.h > 0 {
		inner := max(0, p.h-2)
		if len(lines) > inner {
			lines = lines[:inner]
		}
		for len(lines) < inner {
			lines = append(lines, "")
		}
	}

	var out strings.Builder
	out.WriteString(borderLine(t, border, title, "╭", "╮", p.title, w))
	out.WriteByte('\n')
	side := border.Render("│")
	for _, l := range lines {
		if lipgloss.Width(l) > cw {
			l = ansi.Truncate(l, cw, "…")
		}
		out.WriteString(side + " " + padRight(l, cw) + " " + side + "\n")
	}
	tail := p.counter
	if p.hints != "" {
		tail = p.hints
	}
	out.WriteString(borderLine(t, border, t.Counter, "╰", "╯", tail, w))
	return out.String()
}

// borderLine draws a top or bottom edge with text let into it: at the left
// for a title, at the right for a counter. Text that does not fit is cut.
func borderLine(t Theme, border, text lipgloss.Style, left, right, label string, w int) string {
	room := w - 4 // the corners and one rule on each side of the label
	if room <= 0 || label == "" {
		return border.Render(left + strings.Repeat("─", max(0, w-2)) + right)
	}
	label = truncate(label, room)
	fill := strings.Repeat("─", w-2-1-lipgloss.Width(label))
	if left == "╭" {
		return border.Render(left+"─") + text.Render(label) + border.Render(fill+right)
	}
	return border.Render(left+fill) + text.Render(label) + border.Render("─"+right)
}

// keybar lays hints out on one line, lazygit style: "Add: a | Edit: e". When
// they do not all fit, hints are dropped from the end, but the last one, the
// way to the full list, always stays.
func keybar(t Theme, w int, hints ...hint) string {
	if len(hints) == 0 || w <= 0 {
		return ""
	}
	sep := t.BarText.Render(" | ")
	const sepW = 3

	last := hints[len(hints)-1]
	lastText, lastW := last.bar(t)
	if lastW > w {
		return t.BarKey.Render(truncate(last.key, w))
	}

	var parts []string
	used := lastW
	for _, h := range hints[:len(hints)-1] {
		text, width := h.bar(t)
		if used+sepW+width > w {
			break
		}
		parts = append(parts, text)
		used += sepW + width
	}
	parts = append(parts, lastText)
	return strings.Join(parts, sep)
}

// bar renders a hint for the key bar: the action, then its key.
func (h hint) bar(t Theme) (string, int) {
	action := capitalize(h.action)
	return t.BarText.Render(action+": ") + t.BarKey.Render(h.key),
		lipgloss.Width(action) + 2 + lipgloss.Width(h.key)
}

// counter is a list position as the bottom border shows it.
func counter(cursor, n int) string {
	if n <= 0 {
		return ""
	}
	return itoa(min(cursor, n-1)+1) + " of " + itoa(n)
}
