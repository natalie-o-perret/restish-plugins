package main

import "fmt"

func decodeValue(value any) (any, error) {
	if decoder, ok := value.(interface{ Decode(any) error }); ok {
		var decoded any
		if err := decoder.Decode(&decoded); err != nil {
			return nil, fmt.Errorf("decode value: %w", err)
		}
		return decodeValue(decoded)
	}
	switch value := value.(type) {
	case map[string]any:
		decoded := make(map[string]any, len(value))
		for key, item := range value {
			var err error
			decoded[key], err = decodeValue(item)
			if err != nil {
				return nil, err
			}
		}
		return decoded, nil
	case []any:
		decoded := make([]any, len(value))
		for i, item := range value {
			var err error
			decoded[i], err = decodeValue(item)
			if err != nil {
				return nil, err
			}
		}
		return decoded, nil
	default:
		return value, nil
	}
}
