package main

import (
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestDecodeValueRecurses(t *testing.T) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("id: 42\n"), &node); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeValue(map[string]any{"wrapper": []any{&node}})
	if err != nil {
		t.Fatal(err)
	}
	wrapper := decoded.(map[string]any)["wrapper"].([]any)[0].(map[string]any)
	if wrapper["id"] != 42 {
		t.Fatalf("decoded = %#v", decoded)
	}
}
