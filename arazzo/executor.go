package main

import (
	"context"
	"fmt"

	az "github.com/pb33f/libopenapi/arazzo"
	"github.com/rest-sh/restish/v2/plugin"
)

type executor struct {
	client  requestClient
	sources []*az.ResolvedSource
	params  parameterLocations
	output  any
}

type requestClient interface {
	Do(*plugin.HTTPRequestMsg) (*plugin.HTTPResponseMsg, error)
}

func (e *executor) Execute(_ context.Context, request *az.ExecutionRequest) (*az.ExecutionResponse, error) {
	operation, err := resolveOperation(request, e.sources)
	if err != nil {
		return nil, err
	}
	if err := validateParameterLocations(operationKey(request.OperationID, request.OperationPath), request.Parameters, operation, e.params); err != nil {
		return nil, err
	}
	uri, headers, err := buildRequest(operation, request)
	if err != nil {
		return nil, err
	}
	body, err := decodeValue(request.RequestBody)
	if err != nil {
		return nil, err
	}
	response, err := e.client.Do(&plugin.HTTPRequestMsg{Method: operation.method, URI: uri, Headers: headers, Body: body, ContentType: request.ContentType})
	if err != nil {
		return nil, err
	} else if response.Error != "" {
		return nil, fmt.Errorf("request: %s", response.Error)
	}
	e.output = response.Body
	return &az.ExecutionResponse{StatusCode: response.Status, Headers: response.Headers, Body: response.Body, URL: response.URL, Method: operation.method}, nil
}
