package main

import (
	"context"
	"testing"

	"github.com/pb33f/libopenapi"
	az "github.com/pb33f/libopenapi/arazzo"
	"github.com/rest-sh/restish/v2/plugin"
	"go.yaml.in/yaml/v4"
)

type fakeRequestClient struct{ request *plugin.HTTPRequestMsg }

func (c *fakeRequestClient) Do(request *plugin.HTTPRequestMsg) (*plugin.HTTPResponseMsg, error) {
	c.request = request
	return &plugin.HTTPResponseMsg{Status: 200, URL: "https://pets.example/pets/a%2Fb", Body: map[string]any{"id": "a/b"}}, nil
}

func TestExecutorRoutesOperationIDAndParameters(t *testing.T) {
	source := testOpenAPISource(t, "pets", `openapi: 3.1.0
info: {title: Pets, version: 1.0.0}
components:
  parameters:
    PetID: {name: petId, in: path, required: true, schema: {type: string}}
    Trace: {name: X-Trace, in: header, schema: {type: string}}
paths:
  /pets/{petId}:
    parameters:
      - {$ref: "#/components/parameters/PetID"}
    get:
      operationId: getPet
      parameters:
        - {name: tags, in: query, schema: {type: array, items: {type: string}}}
        - {$ref: "#/components/parameters/Trace"}
      responses:
        "200": {description: OK}
`)
	other := testOpenAPISource(t, "other", "openapi: 3.1.0\ninfo: {title: Other, version: 1.0.0}\npaths: {/other: {get: {operationId: getPet, responses: {'200': {description: OK}}}}}\n")
	client := new(fakeRequestClient)
	key := "id:$sourceDescriptions.pets.getPet"
	exec := &executor{
		client: client, sources: []*az.ResolvedSource{other, source},
		params: parameterLocations{key: {"petId": "path", "tags": "query", "X-Trace": "header"}},
	}
	var tags yaml.Node
	if err := yaml.Unmarshal([]byte("- cat dog\n- dog\n"), &tags); err != nil {
		t.Fatal(err)
	}
	response, err := exec.Execute(context.Background(), &az.ExecutionRequest{
		Source: other, OperationID: "$sourceDescriptions.pets.getPet",
		Parameters: map[string]any{"petId": "a/b", "tags": &tags, "X-Trace": "trace-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.request.Method != "GET" || client.request.URI != "pets/pets/a%2Fb?tags=cat+dog&tags=dog" {
		t.Fatalf("request = %#v", client.request)
	}
	if client.request.Headers["X-Trace"] != "trace-1" {
		t.Fatalf("headers = %#v", client.request.Headers)
	}
	if response.Method != "GET" || response.StatusCode != 200 {
		t.Fatalf("response = %#v", response)
	}
	for _, value := range []string{"a/../secret", "/a", "a//b"} {
		_, err = exec.Execute(context.Background(), &az.ExecutionRequest{OperationID: "$sourceDescriptions.pets.getPet", Parameters: map[string]any{"petId": value}})
		if err == nil {
			t.Fatalf("unsafe path value %q accepted", value)
		}
	}
}

func TestParameterValuesPreserveNull(t *testing.T) {
	values, err := parameterValues([]any{nil, "value"})
	if err != nil || len(values) != 2 || values[0] != "" || values[1] != "value" {
		t.Fatalf("parameterValues = %#v, %v", values, err)
	}
}

func TestOperationPathRejectsUnknownSource(t *testing.T) {
	source := testOpenAPISource(t, "pets", "openapi: 3.1.0\ninfo: {title: Pets, version: 1}\npaths: {}\n")
	request := &az.ExecutionRequest{Source: source, OperationPath: "{$sourceDescriptions.typo.url}#/paths/~1pets/get"}
	if _, err := resolveOperation(request, []*az.ResolvedSource{source}); err == nil {
		t.Fatal("unknown operationPath source accepted")
	}
}

func testOpenAPISource(t *testing.T, name, raw string) *az.ResolvedSource {
	t.Helper()
	doc, err := libopenapi.NewDocument([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatal(err)
	}
	return &az.ResolvedSource{Name: name, Type: "openapi", OpenAPIDocument: &model.Model}
}
