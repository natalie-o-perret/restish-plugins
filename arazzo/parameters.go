package main

import (
	"fmt"
	"net/url"
	"strings"

	az "github.com/pb33f/libopenapi/arazzo"
)

func buildRequest(operation *resolvedOperation, request *az.ExecutionRequest) (string, map[string]string, error) {
	path := operation.path
	query := url.Values{}
	headers := map[string]string{}
	for name, value := range request.Parameters {
		parameter := operation.parameters[name]
		if parameter == nil {
			return "", nil, fmt.Errorf("parameter %q is not uniquely defined by the operation", name)
		}
		if parameter.Content != nil && parameter.Content.Len() > 0 {
			return "", nil, fmt.Errorf("parameter %q uses unsupported content serialization", name)
		}
		values, err := parameterValues(value)
		if err != nil {
			return "", nil, fmt.Errorf("parameter %q: %w", name, err)
		}
		switch parameter.In {
		case "path":
			if parameter.Style != "" && parameter.Style != "simple" {
				return "", nil, fmt.Errorf("path parameter %q uses unsupported style %q", name, parameter.Style)
			}
			placeholder := "{" + name + "}"
			if !strings.Contains(path, placeholder) {
				return "", nil, fmt.Errorf("path parameter %q has no placeholder", name)
			}
			for i := range values {
				normalized := strings.ReplaceAll(values[i], "\\", "/")
				if strings.HasPrefix(normalized, "/") || strings.HasSuffix(normalized, "/") || strings.Contains(normalized, "//") {
					return "", nil, fmt.Errorf("path parameter %q contains an empty segment", name)
				}
				for _, segment := range strings.Split(normalized, "/") {
					if segment == "." || segment == ".." {
						return "", nil, fmt.Errorf("path parameter %q contains a dot segment", name)
					}
				}
				values[i] = url.PathEscape(values[i])
			}
			path = strings.ReplaceAll(path, placeholder, strings.Join(values, ","))
		case "query":
			if parameter.Style != "" && parameter.Style != "form" {
				return "", nil, fmt.Errorf("query parameter %q uses unsupported style %q", name, parameter.Style)
			}
			if parameter.AllowReserved {
				return "", nil, fmt.Errorf("query parameter %q uses unsupported allowReserved", name)
			}
			if parameter.Explode != nil && !*parameter.Explode {
				query.Set(name, strings.Join(values, ","))
			} else {
				for _, item := range values {
					query.Add(name, item)
				}
			}
		case "header":
			if parameter.Style != "" && parameter.Style != "simple" {
				return "", nil, fmt.Errorf("header parameter %q uses unsupported style %q", name, parameter.Style)
			}
			header := strings.Join(values, ",")
			if header == "" {
				return "", nil, fmt.Errorf("header parameter %q is empty", name)
			}
			headers[name] = header
		default:
			return "", nil, fmt.Errorf("parameter %q uses unsupported location %q", name, parameter.In)
		}
	}
	if strings.Contains(path, "{") {
		return "", nil, fmt.Errorf("operation path %q has unresolved parameters", path)
	}
	uri := operation.source.Name + path
	if encoded := query.Encode(); encoded != "" {
		uri += "?" + encoded
	}
	return uri, headers, nil
}
