package main

import (
	"fmt"
	"strings"

	az "github.com/pb33f/libopenapi/arazzo"
)

func resolveOperationPath(value string, fallback *az.ResolvedSource, sources []*az.ResolvedSource) (*resolvedOperation, error) {
	sourceName, qualified, err := parseOperationPathSource(value)
	if err != nil {
		return nil, err
	}
	if len(sources) > 1 && !qualified {
		return nil, fmt.Errorf("operationPath %q must identify a source when multiple sources are declared", value)
	}
	source := fallback
	if qualified {
		source = nil
		for _, candidate := range sources {
			if candidate != nil && candidate.Name == sourceName {
				source = candidate
				break
			}
		}
	} else if source == nil && len(sources) == 1 {
		source = sources[0]
	}
	if source == nil || source.OpenAPIDocument == nil || source.OpenAPIDocument.Paths == nil {
		return nil, fmt.Errorf("operationPath source %q is unavailable", sourceName)
	}
	path, method, err := parseOperationPathPointer(value)
	if err != nil {
		return nil, err
	}
	item := source.OpenAPIDocument.Paths.PathItems.GetOrZero(path)
	if item == nil || item.GetOperations().GetOrZero(method) == nil {
		return nil, fmt.Errorf("operation %s %s not found", strings.ToUpper(method), path)
	}
	return makeOperation(source, path, method, item, item.GetOperations().GetOrZero(method)), nil
}

func parseOperationPathSource(value string) (string, bool, error) {
	const prefix = "$sourceDescriptions."
	index := strings.Index(value, prefix)
	if index < 0 {
		return "", false, nil
	}
	rest := value[index+len(prefix):]
	end := strings.Index(rest, ".")
	if end <= 0 || !strings.HasPrefix(rest[end:], ".url}") {
		return "", false, fmt.Errorf("invalid operationPath source in %q", value)
	}
	return rest[:end], true, nil
}

func parseOperationPathPointer(value string) (string, string, error) {
	const marker = "#/paths/"
	index := strings.Index(value, marker)
	if index < 0 {
		return "", "", fmt.Errorf("invalid operationPath %q", value)
	}
	parts := strings.Split(value[index+len(marker):], "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid operationPath %q", value)
	}
	path := strings.ReplaceAll(strings.ReplaceAll(parts[0], "~1", "/"), "~0", "~")
	return path, strings.ToLower(parts[1]), nil
}
