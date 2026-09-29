package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/rest-sh/restish/v2/plugin"
)

func TestPrettyRendersCollections(t *testing.T) {
	body := map[string]any{
		"source": "fixture",
		"publications": []any{
			map[string]any{"id": "pub-b", "title": "Field Notes", "active": false, "pages": 320, "size_bytes": uint64(32 << 20), "formats": []any{"print", "ebook"}, "featured": true},
			map[string]any{"id": "pub-a", "title": "Short Stories", "active": true, "pages": 144, "size_bytes": uint64(8 << 20), "formats": []any{"ebook"}, "featured": true},
		},
		"warnings": []any{
			map[string]any{"code": "stale", "message": "One source is delayed"},
		},
	}

	var out strings.Builder
	if err := renderPretty(&out, body); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Source=fixture", "Featured: true", "│ Publications", "Active", "Pages",
		"32 MiB", "print, ebook", "│ Warnings", "One source is delayed",
	} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("output omitted %q:\n%s", expected, out.String())
		}
	}
	if strings.Index(out.String(), "pub-b") > strings.Index(out.String(), "pub-a") {
		t.Fatalf("table did not preserve array order:\n%s", out.String())
	}
	ids := []string{
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		"33333333-3333-3333-3333-333333333333",
	}
	formats := []any{"hardcover", "paperback", "ebook", "audiobook", "large-print", "braille", "serial", "archive"}
	var wrapped strings.Builder
	if err := renderRows(&wrapped, "", []map[string]any{
		{"id": ids[0], "sequence": 1, "formats": formats},
		{"id": ids[1], "sequence": 2},
		{"id": ids[2], "sequence": 3},
	}, 0); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if !strings.Contains(wrapped.String(), id) {
			t.Fatalf("wrapped table omitted full ID %q:\n%s", id, wrapped.String())
		}
	}
	for _, format := range formats {
		if !strings.Contains(wrapped.String(), format.(string)) {
			t.Fatalf("wrapped table omitted format %q:\n%s", format, wrapped.String())
		}
	}
}

func TestPrettyFitsTableToTerminalWidth(t *testing.T) {
	var out strings.Builder
	if err := renderRows(&out, "", []map[string]any{
		{"id": "publication-one", "title": "A deliberately long publication title"},
	}, 32); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if width := len([]rune(line)); width > 32 {
			t.Fatalf("rendered line is %d columns wide, want at most 32:\n%s", width, out.String())
		}
	}
	for _, expected := range []string{"deliberately", "publication", "title"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("width-constrained table omitted %q:\n%s", expected, out.String())
		}
	}
}

func TestPrettyFitsTreeLeavesToTerminalWidth(t *testing.T) {
	var out strings.Builder
	if err := renderTree(&out, map[string]any{
		"generated_at": "2026-09-18T20:45:00Z",
	}, 24); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if width := len([]rune(line)); width > 24 {
			t.Fatalf("rendered line is %d columns wide, want at most 24:\n%s", width, out.String())
		}
	}
}

func TestPrettyRendersNestedStructures(t *testing.T) {
	body := map[string]any{
		"catalog": map[string]any{
			"name":  "demo",
			"owner": map[string]any{"name": "Alex"},
			"sections": []any{
				map[string]any{
					"name": "fiction",
					"books": []any{
						map[string]any{"id": "book-1", "status": "available"},
						map[string]any{"id": "book-2", "status": "borrowed"},
					},
				},
			},
		},
	}

	var out strings.Builder
	if err := renderPretty(&out, body); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Details\n└── Catalog", "    ├── Name: demo", "    ├── Owner",
		"    │   └── Name: Alex", "    └── Sections", "        └── Item 1",
		"            ├── Name: fiction", "│ Books", "book-1", "book-2",
	} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("output omitted %q:\n%s", expected, out.String())
		}
	}
}

func TestPrettyRendersShallowNestedRecordsAsTable(t *testing.T) {
	body := []any{
		map[string]any{
			"id": "node-a", "name": "Alpha", "tags": []any{},
			"capacity": map[string]any{
				"cpu":    map[string]any{"total": 100, "used": 40},
				"memory": map[string]any{"total": uint64(8 << 30), "used": uint64(4 << 30)},
			},
			"cluster": map[string]any{"name": "group-a"},
		},
		map[string]any{
			"id": "node-b", "name": "Beta", "tags": []any{"reserved"},
			"capacity": map[string]any{
				"cpu":    map[string]any{"total": 120, "used": 60},
				"memory": map[string]any{"total": uint64(16 << 30), "used": uint64(6 << 30)},
			},
			"cluster": map[string]any{"name": "group-b"},
		},
	}
	rows, _ := recordCollection(body)
	if _, ok := tabularRows(rows, 120, nestedRecordsAuto); !ok {
		t.Fatal("nested rows should be tabular at 120 columns")
	}
	if _, ok := tabularRows(rows, 70, nestedRecordsAuto); !ok {
		t.Fatal("nested rows should remain tabular at 70 columns")
	}
	if _, ok := tabularRows(rows, 55, nestedRecordsAuto); ok {
		t.Fatal("nested rows should remain a tree when columns cannot stay readable")
	}
	if _, ok := tabularRows(rows, 55, nestedRecordsTable); !ok {
		t.Fatal("table mode should ignore the automatic width threshold")
	}
	if _, ok := tabularRows(rows, 120, nestedRecordsTree); ok {
		t.Fatal("tree mode should reject nested tables")
	}

	var out strings.Builder
	if err := renderPretty(&out, body); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Capacity Cpu Total", "Capacity Memory Used", "Cluster Name",
		"node-a", "node-b", "4 GiB", "6 GiB", "[]", "reserved",
	} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("output omitted %q:\n%s", expected, out.String())
		}
	}
	for _, unwanted := range []string{"Items", "Item 1", `{"cpu"`} {
		if strings.Contains(out.String(), unwanted) {
			t.Fatalf("output contains tree or JSON marker %q:\n%s", unwanted, out.String())
		}
	}
	if strings.Index(out.String(), "node-a") > strings.Index(out.String(), "node-b") {
		t.Fatalf("table did not preserve array order:\n%s", out.String())
	}
}

func TestPrettyKeepsDeepNestedRecordsAsTree(t *testing.T) {
	body := []any{
		map[string]any{"id": "node-a", "metrics": map[string]any{"cpu": map[string]any{"usage": map[string]any{"value": 40}}}},
		map[string]any{"id": "node-b", "metrics": map[string]any{"cpu": map[string]any{"usage": map[string]any{"value": 60}}}},
	}

	var out strings.Builder
	if err := renderPretty(&out, body); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Items", "Item 1", "Item 2", "Metrics", "Usage"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("tree output omitted %q:\n%s", expected, out.String())
		}
	}
}

func TestPrettyKeepsAmbiguousNestedRecordsAsTree(t *testing.T) {
	tests := map[string][]any{
		"missing field": {
			map[string]any{"id": "node-a", "stats": map[string]any{"cpu": nil}},
			map[string]any{"id": "node-b"},
		},
		"colliding path": {
			map[string]any{"id": "node-a", "stats": map[string]any{"cpu": 40}},
			map[string]any{"id": "node-b", "stats.cpu": 60},
		},
		"identical rows": {
			map[string]any{"id": "node-a", "stats": map[string]any{"cpu": 40}},
			map[string]any{"id": "node-a", "stats": map[string]any{"cpu": 40}},
		},
		"empty object and scalar": {
			map[string]any{"id": "node-a", "stats": map[string]any{}},
			map[string]any{"id": "node-b", "stats": "unknown"},
		},
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var out strings.Builder
			if err := renderPretty(&out, body); err != nil {
				t.Fatal(err)
			}
			for _, expected := range []string{"Items", "Item 1", "Item 2"} {
				if !strings.Contains(out.String(), expected) {
					t.Fatalf("tree output omitted %q:\n%s", expected, out.String())
				}
			}
		})
	}
}

func TestTableModeAllowsOptionalNestedRecordFields(t *testing.T) {
	body := []any{
		map[string]any{"id": "node-a", "stats": map[string]any{"cpu": 40}, "platform": "linux"},
		map[string]any{"id": "node-b", "stats": map[string]any{"cpu": 60}},
	}
	rows, _ := recordCollection(body)
	if _, ok := tabularRows(rows, 120, nestedRecordsAuto); ok {
		t.Fatal("auto mode should reject optional nested-record fields")
	}
	if _, ok := tabularRows(rows, 120, nestedRecordsTable); !ok {
		t.Fatal("table mode should allow optional nested-record fields")
	}

	var out strings.Builder
	if err := renderPrettyWithMode(&out, body, nestedRecordsTable); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Platform", "Stats Cpu", "linux", "node-a", "node-b"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("table output omitted %q:\n%s", expected, out.String())
		}
	}
	if strings.Contains(out.String(), "Item 1") {
		t.Fatalf("table mode rendered a tree:\n%s", out.String())
	}
}

func TestFormatterNestedRecordsConfig(t *testing.T) {
	body := []any{
		map[string]any{"id": "node-a", "stats": map[string]any{"cpu": 40, "memory": map[string]any{"total": uint64(8 << 30)}}},
		map[string]any{"id": "node-b", "stats": map[string]any{"cpu": 60, "memory": map[string]any{"total": uint64(16 << 30)}}},
	}
	var out strings.Builder
	f := formatter{w: &out, nestedRecords: nestedRecordsAuto}
	if err := f.Handle(formatterRequest{
		Event:        "start",
		PluginConfig: json.RawMessage(`{"nested_records":"tree"}`),
		Response:     plugin.FormatterResponse{Body: body},
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.Handle(formatterRequest{Event: "end"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Items\n├── Item 1") {
		t.Fatalf("tree config was not applied:\n%s", out.String())
	}
	for _, expected := range []string{"8 GiB", "16 GiB"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("tree output omitted memory value %q:\n%s", expected, out.String())
		}
	}

	if err := (&formatter{w: &strings.Builder{}}).Handle(formatterRequest{
		Event:        "start",
		PluginConfig: json.RawMessage(`{"nested_records":"wide"}`),
	}); err == nil {
		t.Fatal("invalid nested_records mode was accepted")
	}
}

func TestPrettyPreservesIdenticalFlatRows(t *testing.T) {
	body := []any{
		map[string]any{"id": "same"},
		map[string]any{"id": "same"},
	}

	var out strings.Builder
	if err := renderPretty(&out, body); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Items", "Item 1", "Item 2"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("output omitted %q:\n%s", expected, out.String())
		}
	}
}

func TestPrettyUsesTreeWhenMetadataWouldOverflow(t *testing.T) {
	body := map[string]any{
		"description": strings.Repeat("wide ", 8),
		"nodes": []any{
			map[string]any{"id": "a"},
			map[string]any{"id": "b"},
		},
	}
	if !requiresTree(body, 24, nestedRecordsAuto) {
		t.Fatal("wide root metadata should select tree output")
	}
}

func TestPrettyUsesTreeWhenFlatConstantsWouldOverflow(t *testing.T) {
	body := []any{
		map[string]any{"id": "a", "description": strings.Repeat("wide ", 8)},
		map[string]any{"id": "b", "description": strings.Repeat("wide ", 8)},
	}
	if !requiresTree(body, 24, nestedRecordsAuto) {
		t.Fatal("wide table constants should select tree output")
	}
}

func TestTreeByteInferenceUsesWholeCollection(t *testing.T) {
	body := []any{
		map[string]any{"id": "a", "capacity": map[string]any{"memory": map[string]any{"total": uint64(8 << 30)}}},
		map[string]any{"id": "b", "capacity": map[string]any{"memory": map[string]any{"total": 512}}},
	}

	var out strings.Builder
	if err := renderPrettyWithMode(&out, body, nestedRecordsTree); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "8 GiB") || !strings.Contains(out.String(), "8589934592") {
		t.Fatalf("tree inferred bytes from only one row:\n%s", out.String())
	}
}

func TestTreeWidthUsesDisplayCellsAndContinuationPrefix(t *testing.T) {
	var out strings.Builder
	if err := renderTree(&out, map[string]any{"x": strings.Repeat("a", 30)}, 12); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if width := text.StringWidth(line); width > 12 {
			t.Fatalf("rendered line is %d columns wide, want at most 12:\n%s", width, out.String())
		}
	}
}

func TestTreeDoesNotInterpretLiteralPathSeparator(t *testing.T) {
	body := map[string]any{
		"details":           map[string]any{"active": true},
		"memory\x1fpercent": uint64(2 << 20),
	}
	var out strings.Builder
	if err := renderPretty(&out, body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "2 MiB") || !strings.Contains(out.String(), "2097152") {
		t.Fatalf("literal key was interpreted as a nested memory path:\n%s", out.String())
	}

	out.Reset()
	if err := renderPretty(&out, map[string]any{
		"memory": map[string]any{"\x1ftotal": uint64(8 << 30)},
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "8 GiB") {
		t.Fatalf("separator at a path boundary hid the memory ancestor:\n%s", out.String())
	}
}

func TestNestedTableModeIgnoresConstantWidth(t *testing.T) {
	rows := []map[string]any{
		{"id": "a", "description": strings.Repeat("wide ", 8), "stats": map[string]any{"cpu": 40}},
		{"id": "b", "description": strings.Repeat("wide ", 8), "stats": map[string]any{"cpu": 60}},
	}
	if _, ok := tabularRows(rows, 24, nestedRecordsAuto); ok {
		t.Fatal("auto mode accepted an overflowing constant")
	}
	if _, ok := tabularRows(rows, 24, nestedRecordsTable); !ok {
		t.Fatal("table mode applied the automatic constant-width threshold")
	}
}

func TestByteInferenceDoesNotBroadenFlatColumnNames(t *testing.T) {
	if inferByteColumns([]map[string]any{{"network_bytes_count": 42}})["network_bytes_count"] {
		t.Fatal("network_bytes_count should not be inferred as bytes")
	}
}

func TestMemoryFormattingAllowsZeroAndRounds(t *testing.T) {
	column := encodeColumnPath([]string{"capacity", "memory", "used"})
	rows := []map[string]any{{column: 0}, {column: 379030863872.0}}
	if !inferByteColumns(rows)[column] {
		t.Fatal("zero memory usage prevented byte inference")
	}
	if got, want := humanBytes(496174483375), "462.1 GiB"; got != want {
		t.Fatalf("humanBytes() = %q, want %q", got, want)
	}
}

func TestNumericColumnHeadersStayLeftAligned(t *testing.T) {
	column := encodeColumnPath([]string{"memory", "used"})
	var out strings.Builder
	if err := renderRows(&out, "", []map[string]any{{column: 40}, {column: 60}}, 12); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\n│ Used") {
		t.Fatalf("numeric column header is not left-aligned:\n%s", out.String())
	}
}

func TestPrettyUnwrapsDisplayEnvelope(t *testing.T) {
	body := []any{
		map[string]any{
			"body": map[string]any{
				"records": []any{
					map[string]any{"id": "record-1", "name": "example"},
				},
			},
			"transport": map[string]any{"status": 200, "region": "region-a"},
		},
	}

	var out strings.Builder
	if err := renderPretty(&out, body); err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"Items", "Item 1", "Body"} {
		if strings.Contains(out.String(), unwanted) {
			t.Fatalf("output contains redundant %q envelope:\n%s", unwanted, out.String())
		}
	}
	for _, expected := range []string{"Transport", "│ Records", "record-1"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("output omitted %q:\n%s", expected, out.String())
		}
	}
}

func TestTitleSplitsCommonIdentifierStyles(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"snake_case", "Snake Case"},
		{"spinal-case", "Spinal Case"},
		{"camelCase", "Camel Case"},
		{"PascalCase", "Pascal Case"},
		{"HTTPServerID", "HTTP Server ID"},
		{"IPv6Address", "IPv6 Address"},
	}
	for _, test := range tests {
		if got := title(test.input); got != test.want {
			t.Errorf("title(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestPrettyRendersEmptyContainersInline(t *testing.T) {
	body := map[string]any{
		"alerts":   []any{},
		"metadata": map[string]any{},
	}

	var out strings.Builder
	if err := renderPretty(&out, body); err != nil {
		t.Fatal(err)
	}
	want := "Details\n├── Alerts: []\n└── Metadata: {}\n"
	if got := out.String(); got != want {
		t.Fatalf("output mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}
