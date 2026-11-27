package ui

import (
	"charm.land/lipgloss/v2"
)

// Theme holds every style the app draws with, resolved once for the terminal's
// light or dark background. Nothing else in the package calls lipgloss.Color.
type Theme struct {
	// dark records which background the styles were resolved for.
	dark bool

	Title    lipgloss.Style
	Month    lipgloss.Style
	Section  lipgloss.Style
	Total    lipgloss.Style
	Label    lipgloss.Style
	Selected lipgloss.Style
	Amount   lipgloss.Style
	Marker   lipgloss.Style
	Cursor   lipgloss.Style
	Muted    lipgloss.Style
	Positive lipgloss.Style
	Negative lipgloss.Style
	Rule     lipgloss.Style
	Footer   lipgloss.Style
	Key      lipgloss.Style
	BarFill  lipgloss.Style
	BarEmpty lipgloss.Style
	BarOver  lipgloss.Style
	Notice   lipgloss.Style
	Warning  lipgloss.Style
	Danger   lipgloss.Style
	Field    lipgloss.Style
	FieldOn  lipgloss.Style

	// The pane frame. The focused pane's border and title are Rosso Corsa;
	// every other pane recedes into the line colour.
	BorderActive     lipgloss.Style
	BorderInactive   lipgloss.Style
	TitleActive      lipgloss.Style
	TitleInactive    lipgloss.Style
	SelectedActive   lipgloss.Style
	SelectedInactive lipgloss.Style
	Counter          lipgloss.Style
	BarKey           lipgloss.Style
	BarText          lipgloss.Style
}

// rossoCorsa is Ferrari's Rosso Corsa 322, the focus colour. It reads on both
// light and dark backgrounds, so it is not resolved per background.
const rossoCorsa = "#D40000"

func newTheme(dark bool) Theme {
	c := lipgloss.LightDark(dark)

	var (
		fg     = c(lipgloss.Color("#1f2328"), lipgloss.Color("#e6edf3"))
		muted  = c(lipgloss.Color("#6e7781"), lipgloss.Color("#8b949e"))
		green  = c(lipgloss.Color("#1a7f37"), lipgloss.Color("#56d364"))
		red    = c(lipgloss.Color("#cf222e"), lipgloss.Color("#ff7b72"))
		accent = c(lipgloss.Color("#0969da"), lipgloss.Color("#79c0ff"))
		yellow = c(lipgloss.Color("#9a6700"), lipgloss.Color("#e3b341"))
		line   = c(lipgloss.Color("#d0d7de"), lipgloss.Color("#30363d"))
		bar    = c(lipgloss.Color("#ddf4ff"), lipgloss.Color("#1f3a5f"))
		focus  = lipgloss.Color(rossoCorsa)
	)

	base := lipgloss.NewStyle()
	return Theme{
		dark:     dark,
		Title:    base.Bold(true).Foreground(accent),
		Month:    base.Bold(true).Foreground(fg),
		Section:  base.Bold(true).Foreground(muted),
		Total:    base.Bold(true).Foreground(fg),
		Label:    base.Foreground(fg),
		Selected: base.Foreground(fg).Background(bar),
		Amount:   base.Foreground(fg),
		Marker:   base.Foreground(accent),
		Cursor:   base.Bold(true).Foreground(accent),
		Muted:    base.Foreground(muted),
		Positive: base.Bold(true).Foreground(green),
		Negative: base.Bold(true).Foreground(red),
		Rule:     base.Foreground(line),
		Footer:   base.Foreground(muted),
		Key:      base.Bold(true).Foreground(fg),
		BarFill:  base.Foreground(green),
		BarEmpty: base.Foreground(line),
		BarOver:  base.Foreground(red),
		Notice:   base.Foreground(accent),
		Warning:  base.Foreground(yellow),
		Danger:   base.Bold(true).Foreground(red),
		Field:    base.Foreground(muted),
		FieldOn:  base.Bold(true).Foreground(accent),

		BorderActive:     base.Bold(true).Foreground(focus),
		BorderInactive:   base.Foreground(line),
		TitleActive:      base.Bold(true).Foreground(focus),
		TitleInactive:    base.Foreground(muted),
		SelectedActive:   base.Foreground(fg).Background(bar),
		SelectedInactive: base.Bold(true).Foreground(fg),
		Counter:          base.Foreground(muted),
		BarKey:           base.Bold(true).Foreground(accent),
		BarText:          base.Foreground(muted),
	}
}
