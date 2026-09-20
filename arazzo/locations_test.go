package main

import (
	"testing"

	high "github.com/pb33f/libopenapi/datamodel/high/arazzo"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

func TestValidateParameterLocations(t *testing.T) {
	operation := &resolvedOperation{parameters: map[string]*v3.Parameter{
		"region": {Name: "region", In: "query"},
	}}
	locations := parameterLocations{"id:list": {"region": "header"}}
	if err := validateParameterLocations("id:list", map[string]any{"region": "gva"}, operation, locations); err == nil {
		t.Fatal("mismatched parameter location accepted")
	}
}

func TestIndexParameterLocationsMergesRepeatedOperations(t *testing.T) {
	workflow := &high.Workflow{Steps: []*high.Step{
		{OperationId: "list", Parameters: []*high.Parameter{{Name: "first", In: "query"}}},
		{OperationId: "list", Parameters: []*high.Parameter{{Name: "second", In: "header"}}},
	}}
	locations := indexParameterLocations(&high.Arazzo{}, workflow)["id:list"]
	if locations["first"] != "query" || locations["second"] != "header" {
		t.Fatalf("locations = %#v", locations)
	}
}
