package main

import (
	"testing"

	az "github.com/pb33f/libopenapi/arazzo"
	"github.com/rest-sh/restish/v2/plugin"
)

type fakeSelectionClient struct {
	request *plugin.HTTPRequestMsg
	output  string
}

func (c *fakeSelectionClient) Do(request *plugin.HTTPRequestMsg) (*plugin.HTTPResponseMsg, error) {
	c.request = request
	return &plugin.HTTPResponseMsg{Status: 200, Body: map[string]any{"pets": []any{
		map[string]any{"id": "42", "name": "Mochi"},
		map[string]any{"id": "7", "name": "Pixel\nFake"},
	}}}, nil
}

func (c *fakeSelectionClient) Prompt(string, bool) (*plugin.PromptResponseMsg, error) {
	return &plugin.PromptResponseMsg{Value: "2"}, nil
}

func (c *fakeSelectionClient) WriteStderr(data []byte) error {
	c.output += string(data)
	return nil
}

func TestSelectInput(t *testing.T) {
	client := new(fakeSelectionClient)
	value, err := selectInput(client, &selection{
		Input: "petId", Source: "pets", Path: "/pets", Items: "pets", Label: "name", Value: "id",
	}, []*az.ResolvedSource{{Name: "pets"}})
	if err != nil {
		t.Fatal(err)
	}
	if value != "7" {
		t.Fatalf("value = %v, want 7", value)
	}
	if client.request.Method != "GET" || client.request.URI != "pets/pets" {
		t.Fatalf("request = %#v", client.request)
	}
	if want := "1) Mochi\n2) Pixel Fake\n"; client.output != want {
		t.Fatalf("output = %q, want %q", client.output, want)
	}
	if _, _, err := parseChoices([]any{map[string]any{"id": 7, "name": "Pixel"}}, &selection{Label: "name", Value: "id"}); err == nil {
		t.Fatal("non-string value accepted")
	}
	_, err = selectInput(client, &selection{
		Input: "petId", Source: "owners", Path: "/pets", Label: "name", Value: "id",
	}, []*az.ResolvedSource{{Name: "pets"}})
	if err == nil {
		t.Fatal("unknown source accepted")
	}
}
