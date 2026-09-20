package main

import (
	"context"
	"testing"

	"github.com/pb33f/libopenapi"
	az "github.com/pb33f/libopenapi/arazzo"
)

func TestEngineRunsQualifiedOperationAndDecodesOutput(t *testing.T) {
	doc, err := libopenapi.NewArazzoDocument([]byte(`arazzo: 1.0.1
info: {title: Test, version: 1.0.0}
sourceDescriptions:
  - {name: pets, url: https://pets.example/openapi.yaml, type: openapi}
  - {name: other, url: https://other.example/openapi.yaml, type: openapi}
workflows:
  - workflowId: test
    steps:
      - stepId: getPet
        operationId: $sourceDescriptions.pets.getPet
        outputs: {pet: $response.body}
    outputs: {selected: $steps.getPet.outputs.pet}
`))
	if err != nil {
		t.Fatal(err)
	}
	if validation := az.Validate(doc); validation != nil && validation.HasErrors() {
		t.Fatal(validation)
	}
	pets := testOpenAPISource(t, "pets", "openapi: 3.1.0\ninfo: {title: Pets, version: 1}\npaths: {/pets: {get: {operationId: getPet, responses: {'200': {description: OK}}}}}\n")
	other := testOpenAPISource(t, "other", "openapi: 3.1.0\ninfo: {title: Other, version: 1}\npaths: {/other: {get: {operationId: getPet, responses: {'200': {description: OK}}}}}\n")
	client := new(fakeRequestClient)
	exec := &executor{
		client: client, sources: []*az.ResolvedSource{other, pets},
		params: indexParameterLocations(doc, doc.Workflows[0]),
	}
	result, err := az.NewEngine(doc, exec, exec.sources).RunWorkflow(context.Background(), "test", nil)
	if err != nil || !result.Success {
		t.Fatalf("workflow = %#v, %v", result, err)
	}
	output, err := workflowOutput(result, nil)
	if err != nil || output.(map[string]any)["selected"].(map[string]any)["id"] != "a/b" {
		t.Fatalf("output = %#v, %v", output, err)
	}
	if client.request.URI != "pets/pets" {
		t.Fatalf("request = %#v", client.request)
	}
}
