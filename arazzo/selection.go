package main

import (
	"fmt"
	"strconv"
	"strings"

	az "github.com/pb33f/libopenapi/arazzo"
	"github.com/rest-sh/restish/v2/plugin"
)

type workflowMeta struct {
	Format  string     `yaml:"format"`
	Columns []string   `yaml:"columns"`
	Select  *selection `yaml:"select"`
}

type selection struct {
	Input  string `yaml:"input"`
	Source string `yaml:"source"`
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
	Items  string `yaml:"items"`
	Label  string `yaml:"label"`
	Value  string `yaml:"value"`
}

type selectionClient interface {
	Do(*plugin.HTTPRequestMsg) (*plugin.HTTPResponseMsg, error)
	Prompt(string, bool) (*plugin.PromptResponseMsg, error)
	WriteStderr([]byte) error
}

func selectInput(client selectionClient, cfg *selection, sources []*az.ResolvedSource) (any, error) {
	if cfg.Input == "" || cfg.Source == "" || cfg.Path == "" || cfg.Label == "" || cfg.Value == "" {
		return nil, fmt.Errorf("select requires input, source, path, label, and value")
	}
	if !strings.HasPrefix(cfg.Path, "/") {
		return nil, fmt.Errorf("select path must start with /")
	}
	found := false
	for _, source := range sources {
		found = found || source != nil && source.Name == cfg.Source
	}
	if !found {
		return nil, fmt.Errorf("select source %q is not declared", cfg.Source)
	}
	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = "GET"
	}
	response, err := client.Do(&plugin.HTTPRequestMsg{Method: method, URI: cfg.Source + cfg.Path})
	if err != nil {
		return nil, fmt.Errorf("select request: %w", err)
	}
	if response.Error != "" {
		return nil, fmt.Errorf("select request: %s", response.Error)
	}
	if response.Status < 200 || response.Status >= 300 {
		return nil, fmt.Errorf("select request: HTTP %d", response.Status)
	}
	values, output, err := parseChoices(response.Body, cfg)
	if err != nil {
		return nil, err
	}
	for len(output) > 0 {
		size := min(len(output), 64<<10)
		if err := client.WriteStderr(output[:size]); err != nil {
			return nil, err
		}
		output = output[size:]
	}
	answer, err := client.Prompt(fmt.Sprintf("Select %s [1-%d]: ", cfg.Input, len(values)), false)
	if err != nil {
		return nil, err
	}
	if answer.Error != "" {
		return nil, fmt.Errorf("select prompt: %s", answer.Error)
	}
	index, err := strconv.Atoi(strings.TrimSpace(answer.Value))
	if err != nil || index < 1 || index > len(values) {
		return nil, fmt.Errorf("select %q: enter a number from 1 to %d", cfg.Input, len(values))
	}
	return values[index-1], nil
}
