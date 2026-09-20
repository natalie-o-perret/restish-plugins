package main

import (
	"fmt"
	"testing"

	"github.com/pb33f/libopenapi"
	"github.com/rest-sh/restish/v2/plugin"
)

type fakeSourceClient map[string][]byte

func (c fakeSourceClient) FetchAPISpec(name string) (*plugin.APISpecResponseMsg, error) {
	raw, ok := c[name]
	if !ok {
		return nil, fmt.Errorf("unknown API %q", name)
	}
	return &plugin.APISpecResponseMsg{Raw: raw}, nil
}

func TestLoadSources(t *testing.T) {
	doc, err := libopenapi.NewArazzoDocument([]byte(`arazzo: 1.0.1
info: {title: Test, version: 1.0.0}
sourceDescriptions:
  - {name: pets, url: https://pets.example/openapi.yaml, type: openapi}
  - {name: owners, url: https://owners.example/openapi.yaml, type: openapi}
workflows:
  - workflowId: test
    steps:
      - {stepId: test, operationPath: "{$sourceDescriptions.pets.url}#/paths/~1pets/get"}
`))
	if err != nil {
		t.Fatal(err)
	}
	spec := []byte("openapi: 3.1.0\ninfo: {title: Test, version: 1.0.0}\npaths: {}\n")
	sources, err := loadSources(fakeSourceClient{"pets": spec, "owners": spec}, doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources[0].Name != "pets" || sources[1].Name != "owners" {
		t.Fatalf("sources = %#v", sources)
	}
	if got := len(doc.GetOpenAPISourceDocuments()); got != 2 {
		t.Fatalf("attached OpenAPI documents = %d, want 2", got)
	}
}
