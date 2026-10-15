package main

import (
	"fmt"
	"io"
	"os"

	"github.com/natalie-o-perret/go-data/anonymize"
	"github.com/rest-sh/restish/v2/plugin"
)

func main() {
	manifest := plugin.Manifest{
		Name:              "anonymize",
		Version:           "0.1.0",
		Description:       "Anonymize identifiers in decoded response data",
		RestishAPIVersion: 2,
		Hooks:             []string{"response-middleware"},
	}
	if plugin.HandleStartupFlags(os.Stdout, manifest, nil) {
		return
	}
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(r io.Reader, w io.Writer) error {
	var input plugin.ResponseMiddlewareInput
	if err := plugin.ReadMessage(r, &input); err != nil {
		return fmt.Errorf("read response middleware request: %w", err)
	}
	if input.Type != "response-middleware" {
		return fmt.Errorf("unexpected request type %q", input.Type)
	}
	output, err := anonymizeResponse(input)
	if err != nil {
		return err
	}
	return plugin.WriteMessage(w, output)
}

func anonymizeResponse(input plugin.ResponseMiddlewareInput) (plugin.ResponseMiddlewareOutput, error) {
	headers := make(map[string]any, len(input.Response.Headers))
	for name, values := range input.Response.Headers {
		items := make([]any, len(values))
		for i, value := range values {
			items[i] = value
		}
		headers[name] = items
	}
	result, err := anonymize.Identifiers(map[string]any{
		"body": input.Response.Body, "headers": headers,
	})
	if err != nil {
		return plugin.ResponseMiddlewareOutput{}, err
	}
	response := result.(map[string]any)
	return plugin.ResponseMiddlewareOutput{Response: &plugin.HookResponseUpdate{
		Body: response["body"], Headers: response["headers"].(map[string]any),
	}}, nil
}
