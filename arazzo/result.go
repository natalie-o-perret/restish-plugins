package main

import (
	"fmt"

	az "github.com/pb33f/libopenapi/arazzo"
)

func workflowOutput(result *az.WorkflowResult, fallback any) (any, error) {
	if len(result.Outputs) == 0 {
		return fallback, nil
	}
	outputs := make(map[string]any, len(result.Outputs))
	for name, value := range result.Outputs {
		decoded, err := decodeValue(value)
		if err != nil {
			return nil, fmt.Errorf("decode workflow output %q: %w", name, err)
		}
		outputs[name] = decoded
	}
	return outputs, nil
}
