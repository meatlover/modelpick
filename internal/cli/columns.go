package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// writeColumns prints rows as aligned columns: cells are joined by two
// spaces and padded to the column width, on the left for the columns in
// rightAligned. A row's last cell never sets a width and is never padded, so
// a short row (fewer cells than the header) cannot shift the rows after it.
// Trailing spaces are trimmed from every line.
func writeColumns(w io.Writer, rows [][]string, rightAligned map[int]bool) {
	var widths []int
	for _, r := range rows {
		for i, c := range r[:max(len(r)-1, 0)] {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], utf8.RuneCountInString(c))
		}
	}
	for _, r := range rows {
		cells := make([]string, len(r))
		for i, c := range r {
			pad := 0
			if i < len(r)-1 {
				pad = widths[i] - utf8.RuneCountInString(c)
			}
			if rightAligned[i] {
				cells[i] = strings.Repeat(" ", pad) + c
			} else {
				cells[i] = c + strings.Repeat(" ", pad)
			}
		}
		fmt.Fprintln(w, strings.TrimRight(strings.Join(cells, "  "), " "))
	}
}
