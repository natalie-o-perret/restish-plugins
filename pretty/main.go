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
	"golang.org/x/term"
)

const (
	maxCellWidth              = 48
	maxNestedTableDepth       = 2
	maxNestedTableColumns     = 16
	minNestedTableColumnWidth = 4
	nestedColumnPathPrefix    = "\xff"
)

type nestedRecordsMode string

const (
	nestedRecordsAuto  nestedRecordsMode = "auto"
	nestedRecordsTable nestedRecordsMode = "table"
	nestedRecordsTree  nestedRecordsMode = "tree"
)

type formatterRequest struct {
	Type         string                   `cbor:"type"`
	Format       string                   `cbor:"format"`
	Color        bool                     `cbor:"color,omitempty"`
	Event        string                   `cbor:"event"`
	PluginConfig json.RawMessage          `cbor:"plugin_config,omitempty"`
	Response     plugin.FormatterResponse `cbor:"response"`
}

type formatter struct {
	w             io.Writer
	values        []any
	nestedRecords nestedRecordsMode
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

	f := &formatter{w: os.Stdout, nestedRecords: nestedRecordsAuto}
	dec := plugin.NewDecoder(os.Stdin)
	for {
		var req formatterRequest
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

func (f *formatter) Handle(req formatterRequest) error {
	switch req.Event {
	case "start":
		mode, err := parseNestedRecordsMode(req.PluginConfig)
		if err != nil {
			return err
		}
		f.nestedRecords = mode
		if req.Response.Body != nil {
			f.values = append(f.values, req.Response.Body)
		}
		return nil
	case "item":
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
		return renderPrettyWithMode(f.w, value, f.nestedRecords)
	default:
		return fmt.Errorf("unsupported formatter event %q", req.Event)
	}
}

func renderPretty(w io.Writer, value any) error {
	return renderPrettyWithMode(w, value, nestedRecordsAuto)
}

func renderPrettyWithMode(w io.Writer, value any, mode nestedRecordsMode) error {
	value = normalizeRoot(value)
	width := terminalWidth(w)
	if requiresTree(value, width, mode) {
		return renderTreeWithMode(w, value, width, mode)
	}
	metadata, sections, ok := tableSections(value, width, mode)
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
		if err := renderRows(w, title(section.title), section.rows, width); err != nil {
			return err
		}
	}
	return nil
}

func parseNestedRecordsMode(config json.RawMessage) (nestedRecordsMode, error) {
	if len(config) == 0 {
		return nestedRecordsAuto, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(config, &object); err != nil {
		return "", fmt.Errorf("pretty config must be a JSON object: %w", err)
	}
	value, ok := object["nested_records"]
	if !ok {
		return nestedRecordsAuto, nil
	}
	var mode nestedRecordsMode
	if err := json.Unmarshal(value, &mode); err != nil {
		return "", fmt.Errorf("pretty config nested_records must be auto, table, or tree")
	}
	switch mode {
	case nestedRecordsAuto, nestedRecordsTable, nestedRecordsTree:
		return mode, nil
	default:
		return "", fmt.Errorf("pretty config nested_records must be auto, table, or tree")
	}
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

func requiresTree(value any, width int, mode nestedRecordsMode) bool {
	switch data := value.(type) {
	case map[string]any:
		metadata := map[string]any{}
		hasSections := false
		for key, candidate := range data {
			switch nested := candidate.(type) {
			case map[string]any:
				return true
			case []any:
				if rows, ok := recordCollection(nested); ok {
					hasSections = true
					if _, ok := tabularRows(rows, width, mode); !ok {
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
			metadata[key] = candidate
		}
		if hasSections && width > 0 && text.StringWidth(formatMetadata(metadata, inferByteColumns([]map[string]any{metadata}))) > width {
			return true
		}
	case []any:
		if rows, ok := recordCollection(data); ok {
			_, ok := tabularRows(rows, width, mode)
			return !ok
		}
	}
	return false
}

func tabularRows(rows []map[string]any, width int, mode nestedRecordsMode) ([]map[string]any, bool) {
	flattened := make([]map[string]any, len(rows))
	nested := false
	for i, row := range rows {
		flattened[i] = make(map[string]any, len(row))
		rowNested := false
		for key, value := range row {
			if !flattenTableValue(flattened[i], encodeColumnPath([]string{key}), value, 0, &rowNested) {
				return nil, false
			}
		}
		nested = nested || rowNested
	}
	prepared := rows
	if nested {
		if mode == nestedRecordsTree || len(rows) < 2 {
			return nil, false
		}
		for _, row := range flattened[1:] {
			if len(row) != len(flattened[0]) {
				return nil, false
			}
			for key, first := range flattened[0] {
				value, exists := row[key]
				if !exists || tableValueShape(value) != tableValueShape(first) {
					return nil, false
				}
			}
		}

		prepared = flattened
		columns := collectColumns(prepared)
		if len(columns) > maxNestedTableColumns {
			return nil, false
		}
		headings := make(map[string]struct{}, len(columns))
		for _, column := range columns {
			heading := header(column)
			if _, exists := headings[heading]; exists {
				return nil, false
			}
			headings[heading] = struct{}{}
		}
	}

	columns := collectColumns(prepared)
	constants, variable := extractConstants(prepared, append([]string(nil), columns...))
	if len(rows) > 1 && len(variable) == 0 {
		return nil, false
	}
	if width > 0 && (!nested || mode != nestedRecordsTable) {
		if nested && mode != nestedRecordsTable && width < (minNestedTableColumnWidth+3)*len(variable)+1 {
			return nil, false
		}
		byteColumns := inferByteColumns(prepared)
		for key, value := range constants {
			if text.StringWidth("├── "+header(key)+": "+cell(key, value, byteColumns)) > width {
				return nil, false
			}
		}
	}
	return prepared, true
}

func tableValueShape(value any) byte {
	switch value.(type) {
	case map[string]any:
		return 'm'
	case []any:
		return 'a'
	default:
		return 's'
	}
}

func joinColumnPath(path, key string) string {
	if path == "" {
		return encodeColumnPath([]string{key})
	}
	return encodeColumnPath(append(splitColumnPath(path), key))
}

func encodeColumnPath(path []string) string {
	encoded, _ := json.Marshal(path)
	return nestedColumnPathPrefix + string(encoded)
}

func splitColumnPath(column string) []string {
	if !strings.HasPrefix(column, nestedColumnPathPrefix) {
		return []string{column}
	}
	var path []string
	if err := json.Unmarshal([]byte(strings.TrimPrefix(column, nestedColumnPathPrefix)), &path); err != nil || len(path) == 0 {
		return []string{column}
	}
	return path
}

func flattenTableValue(row map[string]any, name string, value any, depth int, nested *bool) bool {
	switch data := value.(type) {
	case map[string]any:
		*nested = true
		if depth >= maxNestedTableDepth {
			return false
		}
		if len(data) == 0 {
			break
		}
		for key, child := range data {
			if !flattenTableValue(row, joinColumnPath(name, key), child, depth+1, nested) {
				return false
			}
		}
		return true
	case []any:
		for _, item := range data {
			if !isScalar(item) {
				return false
			}
		}
	}
	if _, exists := row[name]; exists {
		return false
	}
	row[name] = value
	return true
}

func renderTree(w io.Writer, value any, width int) error {
	return renderTreeWithMode(w, value, width, nestedRecordsAuto)
}

func renderTreeWithMode(w io.Writer, value any, width int, mode nestedRecordsMode) error {
	var out strings.Builder
	byteColumns := inferTreeByteColumns(value)
	switch data := value.(type) {
	case map[string]any:
		out.WriteString("Details\n")
		if err := renderTreeObject(&out, data, "", width, mode, "", byteColumns); err != nil {
			return err
		}
	case []any:
		rows, _ := recordCollection(data)
		out.WriteString("Items\n")
		for i, row := range rows {
			if err := renderTreeNode(&out, fmt.Sprintf("Item %d", i+1), row, "", i == len(rows)-1, width, mode, "", byteColumns); err != nil {
				return err
			}
		}
	}
	_, err := io.WriteString(w, out.String())
	return err
}

func renderTreeObject(out *strings.Builder, object map[string]any, prefix string, width int, mode nestedRecordsMode, path string, byteColumns map[string]bool) error {
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
		column := joinColumnPath(path, key)
		if err := renderTreeNode(out, key, object[key], prefix, i == len(keys)-1, width, mode, column, byteColumns); err != nil {
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

func renderTreeNode(out *strings.Builder, name string, value any, prefix string, last bool, width int, mode nestedRecordsMode, column string, byteColumns map[string]bool) error {
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
		return renderTreeObject(out, data, childPrefix, width, mode, column, byteColumns)
	case []any:
		if rows, ok := recordCollection(data); ok {
			if len(rows) == 0 {
				fmt.Fprintf(out, "%s%s%s: []\n", prefix, connector, title(name))
				return nil
			}
			tableWidth := width
			if tableWidth > 0 {
				tableWidth -= text.StringWidth(prefix)
				if tableWidth < 1 {
					tableWidth = 1
				}
			}
			tableRows, tabular := tabularRows(rows, tableWidth, mode)
			if !tabular {
				fmt.Fprintf(out, "%s%s%s\n", prefix, connector, title(name))
				for i, row := range rows {
					if err := renderTreeNode(out, fmt.Sprintf("Item %d", i+1), row, childPrefix, i == len(rows)-1, width, mode, column, byteColumns); err != nil {
						return err
					}
				}
				return nil
			}

			var table strings.Builder
			if err := renderRows(&table, title(name), tableRows, tableWidth); err != nil {
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

	cellWidth := maxCellWidth
	if width > 0 {
		labelWidth := text.StringWidth(prefix + connector + title(name) + ": ")
		continuationWidth := text.StringWidth(childPrefix + "    ")
		available := width - max(labelWidth, continuationWidth)
		if available < 1 {
			cellWidth = 1
		} else if available < cellWidth {
			cellWidth = available
		}
	}
	lines := strings.Split(text.WrapSoft(cell(column, value, byteColumns), cellWidth), "\n")
	fmt.Fprintf(out, "%s%s%s: %s\n", prefix, connector, title(name), lines[0])
	for _, line := range lines[1:] {
		fmt.Fprintf(out, "%s    %s\n", childPrefix, line)
	}
	return nil
}

func renderRows(w io.Writer, tableTitle string, rows []map[string]any, width int) error {
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
	cellWidth := maxCellWidth
	if available := (width - 3*len(columns) - 1) / len(columns); width > 0 && available > 0 && available < cellWidth {
		cellWidth = available
	}
	for i, column := range columns {
		columnConfigs[i] = prettytable.ColumnConfig{
			Align:            numericColumnAlignment(rows, column),
			Number:           i + 1,
			WidthMax:         cellWidth,
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
			fmt.Fprintf(&details, "├── %s: %s\n", header(key), cell(key, constants[key], byteColumns))
		}
		details.WriteString("│\n├")
		details.WriteString(strings.TrimPrefix(rendered, "╭"))
		rendered = details.String()
	}
	_, err := fmt.Fprintln(w, rendered)
	return err
}

func terminalWidth(w io.Writer) int {
	f, ok := w.(interface{ Fd() uintptr })
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return 0
	}
	width, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return width
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

func tableSections(value any, width int, mode nestedRecordsMode) (map[string]any, []tableSection, bool) {
	if object, ok := value.(map[string]any); ok {
		metadata := map[string]any{}
		var sections []tableSection
		for title, candidate := range object {
			if rows, ok := recordCollection(candidate); ok {
				rows, ok = tabularRows(rows, width, mode)
				if !ok {
					return nil, nil, false
				}
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
	rows, ok = tabularRows(rows, width, mode)
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

func inferTreeByteColumns(value any) map[string]bool {
	values := map[string][]any{}
	var collect func(any, string)
	collect = func(value any, path string) {
		switch data := value.(type) {
		case map[string]any:
			for key, child := range data {
				collect(child, joinColumnPath(path, key))
			}
		case []any:
			if rows, ok := recordCollection(data); ok {
				for _, row := range rows {
					collect(row, path)
				}
			} else if path != "" {
				values[path] = append(values[path], value)
			}
		default:
			if path != "" {
				values[path] = append(values[path], value)
			}
		}
	}
	collect(value, "")

	result := map[string]bool{}
	for column, candidates := range values {
		rows := make([]map[string]any, len(candidates))
		for i, candidate := range candidates {
			rows[i] = map[string]any{column: candidate}
		}
		if inferByteColumns(rows)[column] {
			result[column] = true
		}
	}
	return result
}

func inferByteColumns(rows []map[string]any) map[string]bool {
	result := map[string]bool{}
	for _, column := range collectColumns(rows) {
		path := splitColumnPath(column)
		normalized := strings.ToLower(strings.ReplaceAll(path[len(path)-1], "-", "_"))
		if normalized == "bytes" || strings.HasSuffix(normalized, "_bytes") {
			result[column] = true
			continue
		}
		nestedBytes, nestedMemory := false, false
		for _, parent := range path[:len(path)-1] {
			parent = strings.ToLower(strings.ReplaceAll(parent, "-", "_"))
			nestedBytes = nestedBytes || parent == "bytes" || strings.HasSuffix(parent, "_bytes")
			nestedMemory = nestedMemory || parent == "memory" || strings.HasSuffix(parent, "_memory")
		}
		if nestedBytes {
			result[column] = true
			continue
		}
		if normalized != "memory" && !strings.HasSuffix(normalized, "_memory") && !nestedMemory {
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
			if !ok || math.Abs(n) < 1<<20 || !nestedMemory && math.Mod(math.Abs(n), 1024) != 0 {
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
		if len(data) == 0 {
			return "[]"
		}
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
	return title(strings.Join(splitColumnPath(column), " "))
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
