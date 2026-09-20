package main

import "testing"

func TestParseOperationPathSource(t *testing.T) {
	name, qualified, err := parseOperationPathSource("{$sourceDescriptions.pets.url}#/paths/~1pets/get")
	if err != nil || !qualified || name != "pets" {
		t.Fatalf("source = %q, %v, %v", name, qualified, err)
	}
	if _, _, err := parseOperationPathSource("{$sourceDescriptions.pets.uri}#/paths/~1pets/get"); err == nil {
		t.Fatal("invalid source field accepted")
	}
}
