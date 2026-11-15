package ui

import "strings"

func (m Model) helpView(cw int) string {
	t := m.theme
	lines := []string{t.Title.Render("keys"), ""}

	section := func(name string, pairs ...[2]string) {
		lines = append(lines, t.Section.Render(name))
		for _, p := range pairs {
			lines = append(lines, "  "+t.Key.Render(padRight(p[0], 12))+
				t.Footer.Render(truncate(p[1], max(6, cw-14))))
		}
		lines = append(lines, "")
	}

	section("MONTH",
		[2]string{"a", "add to the section the cursor is in"},
		[2]string{"i / x", "add income / add expense directly"},
		[2]string{"e, enter", "edit the selected entry"},
		[2]string{"d", "delete the selected entry"},
		[2]string{"r", "carry this entry into future months"},
		[2]string{"c", "put the selected expense in a category"},
		[2]string{"u", "payees that have no category yet (→ on one to file it one by one)"},
		[2]string{"enter, space", "on a category: fold or unfold it"},
		[2]string{"↑ ↓, j k", "move the cursor"},
	)
	section("MOVING AROUND",
		[2]string{"← →", "previous / next month"},
		[2]string{"t", "jump back to this month"},
		[2]string{"h", "history"},
		[2]string{"I", "import a bank statement (ING or Revolut CSV)"},
	)
	section("ANYWHERE",
		[2]string{"?", "this list"},
		[2]string{"q", "quit"},
	)

	lines = append(lines, wrap(t.Muted, cw,
		"Recurring entries (↻) are copied into each new month you open. "+
			"Edit or delete one there and the change follows forward, not back."))
	lines = append(lines, "", t.Muted.Render(truncate("Months are saved in "+m.store.Dir(), cw)))
	lines = append(lines, "", t.Footer.Render("press any key to go back"))
	return strings.Join(lines, "\n")
}
