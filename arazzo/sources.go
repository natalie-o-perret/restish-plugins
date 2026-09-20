package main

import (
	"fmt"

	"github.com/pb33f/libopenapi"
	az "github.com/pb33f/libopenapi/arazzo"
	high "github.com/pb33f/libopenapi/datamodel/high/arazzo"
	"github.com/rest-sh/restish/v2/plugin"
)

type sourceClient interface {
	FetchAPISpec(string) (*plugin.APISpecResponseMsg, error)
}

func loadSources(client sourceClient, doc *high.Arazzo) ([]*az.ResolvedSource, error) {
	resolved := make([]*az.ResolvedSource, 0, len(doc.SourceDescriptions))
	for i, source := range doc.SourceDescriptions {
		if source == nil {
			return nil, fmt.Errorf("source %d is empty", i)
		}
		if source.Type != "" && source.Type != "openapi" {
			return nil, fmt.Errorf("source %q is not OpenAPI", source.Name)
		}
		api, err := client.FetchAPISpec(source.Name)
		if err != nil {
			return nil, fmt.Errorf("load Restish API %q: %w", source.Name, err)
		}
		if api == nil || api.Error != "" {
			message := "empty response"
			if api != nil {
				message = api.Error
			}
			return nil, fmt.Errorf("load Restish API %q: %s", source.Name, message)
		}
		openapi, err := libopenapi.NewDocument(api.Raw)
		if err != nil {
			return nil, fmt.Errorf("parse Restish API %q: %w", source.Name, err)
		}
		model, err := openapi.BuildV3Model()
		if err != nil {
			return nil, fmt.Errorf("build Restish API %q: %w", source.Name, err)
		}
		modelDoc := &model.Model
		doc.AddOpenAPISourceDocument(modelDoc)
		resolved = append(resolved, &az.ResolvedSource{Name: source.Name, URL: source.URL, Type: "openapi", OpenAPIDocument: modelDoc})
	}
	return resolved, nil
}
