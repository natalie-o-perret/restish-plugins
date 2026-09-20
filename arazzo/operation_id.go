package main

import (
	"fmt"
	"strings"

	az "github.com/pb33f/libopenapi/arazzo"
)

func resolveOperationID(value string, sources []*az.ResolvedSource) (*resolvedOperation, error) {
	sourceName, operationID, qualified, err := parseOperationID(value)
	if err != nil {
		return nil, err
	}
	if len(sources) > 1 && !qualified {
		return nil, fmt.Errorf("operationId %q must identify a source when multiple sources are declared", value)
	}
	var found *resolvedOperation
	for _, source := range sources {
		if source == nil || qualified && source.Name != sourceName || source.OpenAPIDocument == nil || source.OpenAPIDocument.Paths == nil {
			continue
		}
		for path, item := range source.OpenAPIDocument.Paths.PathItems.FromOldest() {
			if item == nil {
				continue
			}
			for method, operation := range item.GetOperations().FromOldest() {
				if operation == nil || operation.OperationId != operationID {
					continue
				}
				if found != nil {
					return nil, fmt.Errorf("operationId %q is ambiguous", value)
				}
				found = makeOperation(source, path, method, item, operation)
			}
		}
	}
	if found == nil {
		return nil, fmt.Errorf("operationId %q not found", value)
	}
	return found, nil
}

func parseOperationID(value string) (string, string, bool, error) {
	const prefix = "$sourceDescriptions."
	if !strings.HasPrefix(value, prefix) {
		return "", value, false, nil
	}
	parts := strings.SplitN(strings.TrimPrefix(value, prefix), ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false, fmt.Errorf("invalid operationId %q", value)
	}
	return parts[0], parts[1], true, nil
}
