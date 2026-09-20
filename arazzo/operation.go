package main

import (
	"strings"

	az "github.com/pb33f/libopenapi/arazzo"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

type resolvedOperation struct {
	source     *az.ResolvedSource
	path       string
	method     string
	parameters map[string]*v3.Parameter
}

func resolveOperation(request *az.ExecutionRequest, sources []*az.ResolvedSource) (*resolvedOperation, error) {
	if request.OperationID != "" {
		return resolveOperationID(request.OperationID, sources)
	}
	return resolveOperationPath(request.OperationPath, request.Source, sources)
}

func makeOperation(source *az.ResolvedSource, path, method string, item *v3.PathItem, op *v3.Operation) *resolvedOperation {
	parameters := map[string]*v3.Parameter{}
	for _, parameter := range append(append([]*v3.Parameter{}, item.Parameters...), op.Parameters...) {
		if parameter != nil && parameter.Name != "" {
			if previous, exists := parameters[parameter.Name]; exists && (previous == nil || previous.In != parameter.In) {
				parameters[parameter.Name] = nil
				continue
			}
			parameters[parameter.Name] = parameter
		}
	}
	return &resolvedOperation{source: source, path: path, method: strings.ToUpper(method), parameters: parameters}
}
