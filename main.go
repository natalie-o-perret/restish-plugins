package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	prettytable "github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/rest-sh/restish/v2/plugin"
)

const maxCellWidth = 48

type formatter struct {
	w      io.Writer
	values []any
}

type tableSection struct {
	title string
	rows  []map[string]any
}

func main() {
	manifest := plugin.Manifest{
		Name:              "pretty",
		Version:           "0.1.0",
		Description:       "Pretty-print structured responses as trees and tables",
		RestishAPIVersion: 2,
		Hooks:             []string{"formatter"},
		FormatterNames:    []string{"pretty"},
	}
	if plugin.HandleStartupFlags(os.Stdout, manifest, nil) {
		return
	}

	f := &formatter{w: os.Stdout}
	dec := plugin.NewDecoder(os.Stdin)
	for {
		var req plugin.FormatterRequest
		if err := dec.ReadMessage(&req); err != nil {
			fail(fmt.Errorf("read formatter request: %w", err))
		}
		if req.Type != "formatter" || req.Format != "pretty" {
			fail(fmt.Errorf("unexpected formatter request %q/%q", req.Type, req.Format))
		}
		if err := f.Handle(req); err != nil {
			fail(err)
		}
		if req.Event == "end" {
			return
		}
	}
}

func (f *formatter) Handle(req plugin.FormatterRequest) error {
	switch req.Event {
	case "start", "item":
		if req.Response.Body != nil {
			f.values = append(f.values, req.Response.Body)
		}
		return nil
	case "end":
		if len(f.values) == 0 {
			_, err := fmt.Fprintln(f.w, "(empty)")
			return err
		}
		var value any = f.values
		if len(f.values) == 1 {
			value = f.values[0]
		}
		return renderPretty(f.w, value)
	default:
		return fmt.Errorf("unsupported formatter event %q", req.Event)
	}
}

func renderPretty(w io.Writer, value any) error {
	value = normalizeRoot(value)
	if requiresTree(value) {
		return renderTree(w, value)
	}
	metadata, sections, ok := tableSections(value)
	if !ok {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	}
	if len(metadata) > 0 {
		if err := writeMetadata(w, metadata, inferByteColumns([]map[string]any{metadata})); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	for i, section := range sections {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if len(section.rows) == 0 {
			if section.title != "" {
				if _, err := fmt.Fprintln(w, title(section.title)); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(w, "(empty)"); err != nil {
				return err
			}
			continue
		}
		if err := renderRows(w, title(section.title), section.rows); err != nil {
			return err
		}
	}
	return nil
}

func normalizeRoot(value any) any {
	if items, ok := value.([]any); ok && len(items) == 1 {
		if _, ok := items[0].(map[string]any); ok {
			value = items[0]
		}
	}

	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	body, ok := object["body"].(map[string]any)
	if !ok {
		return value
	}
	merged := make(map[string]any, len(body)+len(object)-1)
	for key, candidate := range body {
		merged[key] = candidate
	}
	for key, candidate := range object {
		if key == "body" {
			continue
		}
		if _, exists := merged[key]; exists {
			return value
		}
		merged[key] = candidate
	}
	return merged
}

func requiresTree(value any) bool {
	switch data := value.(type) {
	case map[string]any:
		for _, candidate := range data {
			switch nested := candidate.(type) {
			case map[string]any:
				return true
			case []any:
				if rows, ok := recordCollection(nested); ok {
					if !rowsAreFlat(rows) {
						return true
					}
					continue
				}
				for _, item := range nested {
					switch item.(type) {
					case map[string]any, []any:
						return true
					}
				}
			}
		}
	case []any:
		if rows, ok := recordCollection(data); ok {
			return !rowsAreFlat(rows)
		}
	}
	return false
}

func rowsAreFlat(rows []map[string]any) bool {
	for _, row := range rows {
		for _, value := range row {
			switch nested := value.(type) {
			case map[string]any:
				return false
			case []any:
				for _, item := range nested {
					if !isScalar(item) {
						return false
					}
				}
			}
		}
	}
	return true
}

func renderTree(w io.Writer, value any) error {
	var out strings.Builder
	switch data := value.(type) {
	case map[string]any:
		out.WriteString("Details\n")
		if err := renderTreeObject(&out, data, ""); err != nil {
			return err
		}
	case []any:
		rows, _ := recordCollection(data)
		out.WriteString("Items\n")
		for i, row := range rows {
			if err := renderTreeNode(&out, fmt.Sprintf("Item %d", i+1), row, "", i == len(rows)-1); err != nil {
				return err
			}
		}
	}
	_, err := io.WriteString(w, out.String())
	return err
}

func renderTreeObject(out *strings.Builder, object map[string]any, prefix string) error {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		leftLeaf, rightLeaf := treeLeaf(object[keys[i]]), treeLeaf(object[keys[j]])
		if leftLeaf != rightLeaf {
			return leftLeaf
		}
		return keys[i] < keys[j]
	})
	for i, key := range keys {
		if err := renderTreeNode(out, key, object[key], prefix, i == len(keys)-1); err != nil {
			return err
		}
	}
	return nil
}

func treeLeaf(value any) bool {
	switch data := value.(type) {
	case map[string]any:
		return false
	case []any:
		_, records := recordCollection(data)
		return !records
	default:
		return true
	}
}

func renderTreeNode(out *strings.Builder, name string, value any, prefix string, last bool) error {
	connector, childPrefix := "├── ", prefix+"│   "
	if last {
		connector, childPrefix = "└── ", prefix+"    "
	}

	switch data := value.(type) {
	case map[string]any:
		if len(data) == 0 {
			fmt.Fprintf(out, "%s%s%s: {}\n", prefix, connector, title(name))
			return nil
		}
		fmt.Fprintf(out, "%s%s%s\n", prefix, connector, title(name))
		return renderTreeObject(out, data, childPrefix)
	case []any:
		if rows, ok := recordCollection(data); ok {
			if len(rows) == 0 {
				fmt.Fprintf(out, "%s%s%s: []\n", prefix, connector, title(name))
				return nil
			}
			if !rowsAreFlat(rows) {
				fmt.Fprintf(out, "%s%s%s\n", prefix, connector, title(name))
				for i, row := range rows {
					if err := renderTreeNode(out, fmt.Sprintf("Item %d", i+1), row, childPrefix, i == len(rows)-1); err != nil {
						return err
					}
				}
				return nil
			}

			var table strings.Builder
			if err := renderRows(&table, title(name), rows); err != nil {
				return err
			}
			rendered := strings.TrimSuffix(strings.TrimPrefix(table.String(), "Details\n"), "\n")
			lines := strings.Split(rendered, "\n")
			if strings.HasPrefix(lines[0], "╭") {
				lines[0] = "├" + strings.TrimPrefix(lines[0], "╭")
			}
			if !last && strings.HasPrefix(lines[len(lines)-1], "╰") {
				lines[len(lines)-1] = "├" + strings.TrimPrefix(lines[len(lines)-1], "╰")
			}
			for _, line := range lines {
				fmt.Fprintf(out, "%s%s\n", prefix, line)
			}
			return nil
		}
	}

	byteColumns := inferByteColumns([]map[string]any{{name: value}})
	lines := strings.Split(text.WrapSoft(cell(name, value, byteColumns), maxCellWidth), "\n")
	fmt.Fprintf(out, "%s%s%s: %s\n", prefix, connector, title(name), lines[0])
	for _, line := range lines[1:] {
		fmt.Fprintf(out, "%s    %s\n", childPrefix, line)
	}
	return nil
}

func renderRows(w io.Writer, tableTitle string, rows []map[string]any) error {
	byteColumns := inferByteColumns(rows)
	columns := collectColumns(rows)
	constants, columns := extractConstants(rows, columns)
	sortColumns(rows, columns)

	if len(columns) == 0 {
		return writeMetadata(w, constants, byteColumns)
	}

	headings := make(prettytable.Row, len(columns))
	for i, column := range columns {
		headings[i] = header(column)
	}
	tableRows := make([]prettytable.Row, len(rows))
	for i, row := range rows {
		tableRows[i] = make(prettytable.Row, len(columns))
		for j, column := range columns {
			tableRows[i][j] = tableCell(column, row[column], byteColumns)
		}
	}

	tw := prettytable.NewWriter()
	tw.SetStyle(prettytable.StyleRounded)
	tw.Style().Format.Header = text.FormatDefault
	tw.Style().Options.SeparateRows = true
	tw.AppendHeader(headings)
	tw.AppendRows(tableRows)
	if tableTitle != "" {
		tw.SetTitle(tableTitle)
	}
	columnConfigs := make([]prettytable.ColumnConfig, len(columns))
	for i, column := range columns {
		columnConfigs[i] = prettytable.ColumnConfig{
			Align:            numericColumnAlignment(rows, column),
			Number:           i + 1,
			WidthMax:         maxCellWidth,
			WidthMaxEnforcer: text.WrapSoft,
		}
	}
	tw.SetColumnConfigs(columnConfigs)
	rendered := tw.Render()
	if len(constants) > 0 {
		keys := make([]string, 0, len(constants))
		for key := range constants {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		var details strings.Builder
		details.WriteString("Details\n")
		for _, key := range keys {
			fmt.Fprintf(&details, "├── %s: %s\n", title(key), cell(key, constants[key], byteColumns))
		}
		details.WriteString("│\n├")
		details.WriteString(strings.TrimPrefix(rendered, "╭"))
		rendered = details.String()
	}
	_, err := fmt.Fprintln(w, rendered)
	return err
}

func numericColumnAlignment(rows []map[string]any, column string) text.Align {
	seen := false
	for _, row := range rows {
		value, exists := row[column]
		if !exists || value == nil {
			continue
		}
		if _, ok := number(value); !ok {
			return text.AlignDefault
		}
		seen = true
	}
	if seen {
		return text.AlignRight
	}
	return text.AlignDefault
}

func tableSections(value any) (map[string]any, []tableSection, bool) {
	if object, ok := value.(map[string]any); ok {
		metadata := map[string]any{}
		var sections []tableSection
		for title, candidate := range object {
			if rows, ok := recordCollection(candidate); ok {
				sections = append(sections, tableSection{title: title, rows: rows})
			} else {
				metadata[title] = candidate
			}
		}
		if len(sections) > 0 {
			sort.Slice(sections, func(i, j int) bool { return sections[i].title < sections[j].title })
			return metadata, sections, true
		}
	}
	rows, ok := tableRows(value)
	if !ok {
		return nil, nil, false
	}
	return nil, []tableSection{{rows: rows}}, true
}

func recordCollection(value any) ([]map[string]any, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		rows = append(rows, row)
	}
	return rows, true
}

func tableRows(value any) ([]map[string]any, bool) {
	switch data := value.(type) {
	case map[string]any:
		return []map[string]any{data}, true
	case []any:
		return recordCollection(data)
	default:
		return nil, false
	}
}

func writeMetadata(w io.Writer, metadata map[string]any, byteColumns map[string]bool) error {
	_, err := fmt.Fprintln(w, formatMetadata(metadata, byteColumns))
	return err
}

func formatMetadata(metadata map[string]any, byteColumns map[string]bool) string {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = header(key) + "=" + cell(key, metadata[key], byteColumns)
	}
	return strings.Join(parts, "  ")
}

func collectColumns(rows []map[string]any) []string {
	seen := map[string]struct{}{}
	for _, row := range rows {
		for key := range row {
			seen[key] = struct{}{}
		}
	}
	columns := make([]string, 0, len(seen))
	for key := range seen {
		columns = append(columns, key)
	}
	return columns
}

func extractConstants(rows []map[string]any, columns []string) (map[string]any, []string) {
	constants := map[string]any{}
	if len(rows) < 2 {
		return constants, columns
	}
	variable := columns[:0]
	for _, column := range columns {
		first, exists := rows[0][column]
		if !exists || first == nil || !isScalar(first) {
			variable = append(variable, column)
			continue
		}
		constant := true
		for _, row := range rows[1:] {
			value, exists := row[column]
			if !exists || !reflect.DeepEqual(value, first) {
				constant = false
				break
			}
		}
		if constant {
			constants[column] = first
		} else {
			variable = append(variable, column)
		}
	}
	return constants, variable
}

func isScalar(value any) bool {
	switch value.(type) {
	case []any, map[string]any:
		return false
	default:
		return true
	}
}

func sortColumns(rows []map[string]any, columns []string) {
	sort.Slice(columns, func(i, j int) bool {
		left, right := columns[i], columns[j]
		leftNested := columnIsNested(rows, left)
		rightNested := columnIsNested(rows, right)
		if leftNested != rightNested {
			return !leftNested
		}
		return left < right
	})
}

func columnIsNested(rows []map[string]any, column string) bool {
	for _, row := range rows {
		switch row[column].(type) {
		case []any, map[string]any:
			return true
		}
	}
	return false
}

func inferByteColumns(rows []map[string]any) map[string]bool {
	result := map[string]bool{}
	for _, column := range collectColumns(rows) {
		normalized := strings.ToLower(strings.ReplaceAll(column, "-", "_"))
		if normalized == "bytes" || strings.HasSuffix(normalized, "_bytes") {
			result[column] = true
			continue
		}
		if normalized != "memory" && !strings.HasSuffix(normalized, "_memory") {
			continue
		}
		seen := false
		valid := true
		for _, row := range rows {
			value, exists := row[column]
			if !exists || value == nil {
				continue
			}
			n, ok := number(value)
			if !ok || math.Abs(n) < 1<<20 || math.Mod(math.Abs(n), 1024) != 0 {
				valid = false
				break
			}
			seen = true
		}
		if seen && valid {
			result[column] = true
		}
	}
	return result
}

func number(value any) (float64, bool) {
	switch n := value.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		parsed, err := n.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func cell(column string, value any, byteColumns map[string]bool) string {
	if byteColumns[column] {
		if n, ok := number(value); ok {
			return humanBytes(n)
		}
	}
	var result string
	switch data := value.(type) {
	case nil:
		return ""
	case string:
		result = data
	case bool:
		result = strconv.FormatBool(data)
	case []any:
		parts := make([]string, len(data))
		for i, item := range data {
			if !isScalar(item) {
				result = compactJSON(value)
				break
			}
			parts[i] = fmt.Sprint(item)
		}
		if result == "" {
			result = strings.Join(parts, ", ")
		}
	case map[string]any:
		result = compactJSON(value)
	default:
		result = fmt.Sprint(value)
	}
	result = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(result)
	return result
}

func tableCell(column string, value any, byteColumns map[string]bool) any {
	if !byteColumns[column] {
		if _, ok := number(value); ok {
			return value
		}
	}
	return cell(column, value, byteColumns)
}

func compactJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

func humanBytes(bytes float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	value := bytes
	unit := 0
	for math.Abs(value) >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return strconv.FormatFloat(value, 'f', -1, 64) + " " + units[unit]
}

func header(column string) string {
	return title(column)
}

func title(value string) string {
	runes := []rune(value)
	words := make([]string, 0, 4)
	start, uppercaseRun := -1, 0
	for i, current := range runes {
		if !unicode.IsLetter(current) && !unicode.IsDigit(current) {
			if start >= 0 {
				words = append(words, string(runes[start:i]))
			}
			start, uppercaseRun = -1, 0
			continue
		}
		if start < 0 {
			start = i
			if unicode.IsUpper(current) {
				uppercaseRun = 1
			}
			continue
		}

		previous := runes[i-1]
		nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
		boundary := unicode.IsUpper(current) && (unicode.IsLower(previous) || unicode.IsDigit(previous) ||
			unicode.IsUpper(previous) && nextIsLower && uppercaseRun > 1)
		if boundary {
			words = append(words, string(runes[start:i]))
			start, uppercaseRun = i, 1
			continue
		}
		if unicode.IsUpper(current) {
			uppercaseRun++
		} else {
			uppercaseRun = 0
		}
	}
	if start >= 0 {
		words = append(words, string(runes[start:]))
	}
	for i, word := range words {
		first, size := utf8.DecodeRuneInString(word)
		words[i] = string(unicode.ToUpper(first)) + word[size:]
	}
	return strings.Join(words, " ")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
