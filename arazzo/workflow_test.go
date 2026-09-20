package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	az "github.com/pb33f/libopenapi/arazzo"
	"go.yaml.in/yaml/v4"
)

func TestTable(t *testing.T) {
	got, err := table(map[string]any{"id": 42, "name": "Mochi"}, []string{"id", "name"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "┌────┬───────┐\n│ ID │ NAME  │\n├────┼───────┤\n│ 42 │ Mochi │\n└────┴───────┘\n"; string(got) != want {
		t.Fatalf("table = %q, want %q", got, want)
	}
}

func TestWorkflowOutput(t *testing.T) {
	fallback := "raw"
	if got, err := workflowOutput(&az.WorkflowResult{}, fallback); err != nil || got != fallback {
		t.Fatalf("fallback = %#v", got)
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("items:\n  - id: 42\n"), &node); err != nil {
		t.Fatal(err)
	}
	result := &az.WorkflowResult{Outputs: map[string]any{"payload": map[string]any{"nested": &node}}}
	output, err := workflowOutput(result, fallback)
	nested := output.(map[string]any)["payload"].(map[string]any)["nested"].(map[string]any)
	if err != nil || nested["items"] == nil {
		t.Fatalf("workflow output = %#v, %v", output, err)
	}
}

func TestGoFilesStayWithin100Lines(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lines := bytes.Count(data, []byte{'\n'})
		if len(data) > 0 && data[len(data)-1] != '\n' {
			lines++
		}
		if lines > 100 {
			t.Errorf("%s has %d lines", file, lines)
		}
	}
}
