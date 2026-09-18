package main

import (
	"strings"
	"testing"
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
	}); err != nil {
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
