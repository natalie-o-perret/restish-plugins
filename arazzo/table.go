package main

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

func table(value any, columns []string) ([]byte, error) {
	row, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("table output must be an object")
	}
	if len(columns) == 0 {
		for column := range row {
			columns = append(columns, column)
		}
		slices.Sort(columns)
	}
	headings := make([]string, len(columns))
	values := make([]string, len(columns))
	for i, column := range columns {
		headings[i] = strings.ToUpper(column)
		values[i] = fmt.Sprint(row[column])
	}
	widths := make([]int, len(columns))
	for i := range columns {
		widths[i] = max(utf8.RuneCountInString(headings[i]), utf8.RuneCountInString(values[i]))
	}
	var output strings.Builder
	border := func(left, middle, right string) {
		output.WriteString(left)
		for i, width := range widths {
			output.WriteString(strings.Repeat("─", width+2))
			if i < len(widths)-1 {
				output.WriteString(middle)
			}
		}
		output.WriteString(right + "\n")
	}
	writeRow := func(cells []string) {
		output.WriteString("│")
		for i, cell := range cells {
			output.WriteString(" " + cell)
			output.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)+1))
			output.WriteString("│")
		}
		output.WriteByte('\n')
	}
	border("┌", "┬", "┐")
	writeRow(headings)
	border("├", "┼", "┤")
	writeRow(values)
	border("└", "┴", "┘")
	return []byte(output.String()), nil
}
