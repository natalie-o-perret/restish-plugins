package main

import (
	"fmt"
	"strings"
	"unicode"
)

func parseChoices(body any, cfg *selection) ([]string, []byte, error) {
	if cfg.Items != "" {
		object, ok := body.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("select response is not an object")
		}
		body = object[cfg.Items]
	}
	items, ok := body.([]any)
	if !ok || len(items) == 0 {
		return nil, nil, fmt.Errorf("select response contains no choices")
	}
	values, output := make([]string, len(items)), new(strings.Builder)
	for i, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("select choice %d is not an object", i+1)
		}
		label, ok := row[cfg.Label].(string)
		if !ok {
			return nil, nil, fmt.Errorf("select choice %d field %q is not a string", i+1, cfg.Label)
		}
		value, ok := row[cfg.Value].(string)
		if !ok {
			return nil, nil, fmt.Errorf("select choice %d field %q is not a string", i+1, cfg.Value)
		}
		label = strings.Map(func(r rune) rune {
			if unicode.IsGraphic(r) {
				return r
			}
			return ' '
		}, label)
		values[i] = value
		fmt.Fprintf(output, "%d) %s\n", i+1, label)
	}
	return values, []byte(output.String()), nil
}
