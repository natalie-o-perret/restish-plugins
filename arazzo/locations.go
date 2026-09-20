package main

import (
	"fmt"
	"strings"

	high "github.com/pb33f/libopenapi/datamodel/high/arazzo"
)

type parameterLocations map[string]map[string]string

func indexParameterLocations(doc *high.Arazzo, workflow *high.Workflow) parameterLocations {
	locations := parameterLocations{}
	for _, step := range workflow.Steps {
		if step == nil || step.WorkflowId != "" {
			continue
		}
		key := operationKey(step.OperationId, step.OperationPath)
		if locations[key] == nil {
			locations[key] = map[string]string{}
		}
		for _, parameter := range step.Parameters {
			parameter = resolveArazzoParameter(doc, parameter)
			if parameter == nil || parameter.Name == "" {
				continue
			}
			if previous, exists := locations[key][parameter.Name]; exists && previous != parameter.In {
				locations[key][parameter.Name] = ""
				continue
			}
			locations[key][parameter.Name] = parameter.In
		}
	}
	return locations
}

func resolveArazzoParameter(doc *high.Arazzo, parameter *high.Parameter) *high.Parameter {
	if parameter == nil || !parameter.IsReusable() {
		return parameter
	}
	const prefix = "$components.parameters."
	if doc.Components == nil || doc.Components.Parameters == nil || !strings.HasPrefix(parameter.Reference, prefix) {
		return nil
	}
	return doc.Components.Parameters.GetOrZero(strings.TrimPrefix(parameter.Reference, prefix))
}

func operationKey(operationID, operationPath string) string {
	if operationID != "" {
		return "id:" + operationID
	}
	return "path:" + operationPath
}

func validateParameterLocations(requestKey string, request map[string]any, operation *resolvedOperation, locations parameterLocations) error {
	for name := range request {
		location, ok := locations[requestKey][name]
		parameter := operation.parameters[name]
		if !ok || location == "" {
			return fmt.Errorf("parameter %q location is not uniquely defined by the workflow", name)
		}
		if parameter == nil || parameter.In != location {
			return fmt.Errorf("parameter %q location %q does not match the operation", name, location)
		}
	}
	return nil
}
