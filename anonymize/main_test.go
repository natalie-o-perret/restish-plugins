package main

import (
	"bytes"
	"errors"
	"regexp"
	"testing"

	"github.com/rest-sh/restish/v2/plugin"
)

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestRunAnonymizesBodyAndHeadersConsistently(t *testing.T) {
	const sharedID = "11111111-2222-4333-8444-555555555555"
	input := plugin.ResponseMiddlewareInput{
		Type: "response-middleware",
		Response: plugin.HookResponse{
			Headers: map[string][]string{"Date": {"today"}, "X-Request-Id": {sharedID}},
			Body: map[string]any{
				"id": sharedID, "name": "example-volume",
				"owner": map[string]any{"id": sharedID},
			},
		},
	}
	var request, response bytes.Buffer
	if err := plugin.WriteMessage(&request, input); err != nil {
		t.Fatal(err)
	}
	if err := run(&request, &response); err != nil {
		t.Fatal(err)
	}
	var output plugin.ResponseMiddlewareOutput
	if err := plugin.ReadMessage(&response, &output); err != nil {
		t.Fatal(err)
	}
	body := output.Response.Body.(map[string]any)
	anonymousID := body["id"].(string)
	if anonymousID == sharedID || !uuidPattern.MatchString(anonymousID) {
		t.Fatalf("anonymized id = %q", anonymousID)
	}
	if got := body["owner"].(map[string]any)["id"]; got != anonymousID {
		t.Fatalf("repeated id = %q, want %q", got, anonymousID)
	}
	if got := output.Response.Headers["X-Request-Id"].([]any)[0]; got != anonymousID {
		t.Fatalf("header id = %q, want %q", got, anonymousID)
	}
	if body["name"] != "example-volume" || output.Response.Headers["Date"].([]any)[0] != "today" {
		t.Fatal("non-identifier data changed")
	}
}

func TestRunRejectsUnexpectedMessages(t *testing.T) {
	var request bytes.Buffer
	if err := plugin.WriteMessage(&request, plugin.ResponseMiddlewareInput{Type: "formatter"}); err != nil {
		t.Fatal(err)
	}
	if err := run(&request, &bytes.Buffer{}); err == nil {
		t.Fatal("unexpected message type was accepted")
	}
}

func TestRunReportsProtocolErrors(t *testing.T) {
	if err := run(&bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("empty request was accepted")
	}
	var request bytes.Buffer
	if err := plugin.WriteMessage(&request, plugin.ResponseMiddlewareInput{Type: "response-middleware"}); err != nil {
		t.Fatal(err)
	}
	if err := run(&request, errorWriter{}); err == nil {
		t.Fatal("output error was ignored")
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	if _, err := anonymizeResponse(plugin.ResponseMiddlewareInput{
		Response: plugin.HookResponse{Body: cycle},
	}); err == nil {
		t.Fatal("recursive body was accepted")
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
