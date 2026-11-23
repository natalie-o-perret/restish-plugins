package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pb33f/go-yaml"
	az "github.com/pb33f/libopenapi/arazzo"
)

func TestRunArazzoVersions(t *testing.T) {
	for _, version := range []string{"1.0.0", "1.0.1", "1.1.0"} {
		t.Run(version, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "workflow.yaml")
			if err := os.WriteFile(file, []byte("arazzo: "+version+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			want := "one-workflow Arazzo document"
			if version == "1.1.0" {
				want = "unsupported Arazzo version"
			}
			if err := run("workflow", []string{"run", file}, nil); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("run = %v, want %q", err, want)
			}
		})
	}
}

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
