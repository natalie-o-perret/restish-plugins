package main

import (
	"fmt"
	"reflect"
)

func parameterValues(value any) ([]string, error) {
	var err error
	value, err = decodeValue(value)
	if err != nil {
		return nil, err
	}
	item := reflect.ValueOf(value)
	if !item.IsValid() {
		return []string{""}, nil
	}
	for item.Kind() == reflect.Pointer || item.Kind() == reflect.Interface {
		if item.IsNil() {
			return []string{""}, nil
		}
		item = item.Elem()
	}
	if item.Kind() != reflect.Slice && item.Kind() != reflect.Array {
		value, err := parameterValue(item)
		return []string{value}, err
	}
	values := make([]string, item.Len())
	for i := range item.Len() {
		entry := item.Index(i)
		null := false
		for entry.Kind() == reflect.Pointer || entry.Kind() == reflect.Interface {
			if entry.IsNil() {
				null = true
				break
			}
			entry = entry.Elem()
		}
		if null {
			continue
		}
		value, err := parameterValue(entry)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	return values, nil
}

func parameterValue(value reflect.Value) (string, error) {
	switch value.Kind() {
	case reflect.Map, reflect.Struct, reflect.Slice, reflect.Array:
		return "", fmt.Errorf("nested values are not supported")
	default:
		return fmt.Sprint(value.Interface()), nil
	}
}
