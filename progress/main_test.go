package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/rest-sh/restish/v2/plugin"
	"github.com/rivo/uniseg"
)

func TestFormatterStreamsProgress(t *testing.T) {
	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	for _, body := range []any{
		map[string]any{"id": "deploy", "current": 1, "total": 2, "message": "first"},
		map[string]any{"id": "deploy", "current": 1, "total": 2, "message": "first"},
		map[string]any{"id": "deploy", "state": "success", "current": 2, "total": 2},
	} {
		if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
			t.Fatal(err)
		}
	}
	want := "50% ████████████░░░░░░░░░░░░  deploy  1/2 steps  running: first\n" +
		"100% ████████████████████████  deploy  2/2 steps  success\n"
	if got := out.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRenderUsesSingularStep(t *testing.T) {
	current, total := int64(1), int64(1)
	want := "100% ████████████████████████  workflow  1/1 step  success"
	got, err := render(progress{Label: "workflow", State: "success", Current: &current, Total: &total}, defaultBarStyle(), false)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("render() = %q, want %q", got, want)
	}
}

func TestRenderUsesCustomUnit(t *testing.T) {
	current, total := int64(512), int64(1024)
	want := "50% ████████████░░░░░░░░░░░░  Download  512/1024 bytes  running"
	got, err := render(progress{Label: "Download", State: "running", Current: &current, Total: &total, Unit: "bytes"}, defaultBarStyle(), false)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("render() = %q, want %q", got, want)
	}
}

func TestRenderKeepsProgressOnOneTerminalLine(t *testing.T) {
	got, err := render(progress{Label: "line one\nline two", Message: "more\nwork"}, defaultBarStyle(), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("render() = %q", got)
	}
}

func TestFormatterRedrawsTTYLine(t *testing.T) {
	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	if err := f.Handle(formatterRequest{Event: "start", Color: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: map[string]any{"label": "workflow"}}}); err != nil {
		t.Fatal(err)
	}
	if err := f.Handle(formatterRequest{Event: "end"}); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "\r\x1b[2Kworkflow  running\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestFormatterStreamsChangedSnapshotRecords(t *testing.T) {
	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	for _, body := range []any{
		[]any{
			map[string]any{"id": "prepare", "state": "running"},
			map[string]any{"id": "finish", "state": "pending"},
		},
		[]any{
			map[string]any{"id": "prepare", "state": "running"},
			map[string]any{"id": "finish", "state": "pending"},
		},
		[]any{
			map[string]any{"id": "prepare", "state": "success"},
			map[string]any{"id": "finish", "state": "pending"},
		},
		[]any{
			map[string]any{"id": "finish", "state": "success"},
		},
	} {
		if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
			t.Fatal(err)
		}
	}
	want := "prepare  running\nfinish  pending\nprepare  success\nfinish  success\n"
	if got := out.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestFormatterNormalizesSSEProgressSnapshots(t *testing.T) {
	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	for _, body := range []any{
		recordSnapshot(
			map[string]any{"id": "status", "label": "Progress", "state": "running", "current": 0, "total": 1},
			map[string]any{"id": "rescue", "parent": "status", "label": "rescue", "state": "running", "current": 0, "total": 1},
			map[string]any{"id": "step-8", "parent": "rescue", "label": "sleep 2", "state": "dispatched", "message": "host-a"}),
		recordSnapshot(
			map[string]any{"id": "status", "label": "Progress", "state": "success", "current": 1, "total": 1},
			map[string]any{"id": "rescue", "parent": "status", "label": "rescue", "state": "success", "current": 1, "total": 1},
			map[string]any{"id": "step-8", "parent": "rescue", "label": "sleep 2", "state": "success", "message": "host-a"}),
		map[string]any{"event": "heartbeat"},
	} {
		if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
			t.Fatal(err)
		}
	}
	want := "0% ░░░░░░░░░░░░░░░░░░░░░░░░  Progress  0/1 step  running\n" +
		"0% ░░░░░░░░░░░░░░░░░░░░░░░░  rescue  0/1 step  running\n" +
		"sleep 2  dispatched: host-a\n" +
		"100% ████████████████████████  Progress  1/1 step  success\n" +
		"100% ████████████████████████  rescue  1/1 step  success\n" +
		"sleep 2  success: host-a\n"
	if got := out.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestFormatterInfersProgressEventSequence(t *testing.T) {
	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	for _, body := range []any{
		map[string]any{"event": "started", "data": map[string]any{"total": 2}},
		map[string]any{"event": "upstream", "data": map[string]any{"duration_ms": 100, "status": "ok", "upstream": "kms/ch-gva-2"}},
		map[string]any{"event": "upstream", "data": map[string]any{"duration_ms": 200, "status": "ok", "upstream": "root-api/ch-gva-2"}},
		map[string]any{"event": "complete", "data": map[string]any{"failed": 0, "succeeded": 2, "total": 2}},
	} {
		if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
			t.Fatal(err)
		}
	}
	got := out.String()
	for _, want := range []string{
		"Progress  0/2 steps  running",
		"Progress  1/2 steps  running: kms/ch-gva-2",
		"Progress  2/2 steps  running: root-api/ch-gva-2",
		"Progress  2/2 steps  success",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
}

func TestFormatterFallsBackForNonProgressBody(t *testing.T) {
	for _, body := range []any{
		map[string]any{"message": "cannot match command"},
		map[string]any{"event": "error", "data": map[string]any{"message": "failed"}},
		[]any{"one", "two"},
	} {
		var out bytes.Buffer
		f := &formatter{w: &out, style: defaultBarStyle()}
		if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
			t.Fatal(err)
		}
		if out.Len() != 0 || !f.fallback {
			t.Fatalf("body = %#v, output = %q, fallback = %v", body, out.String(), f.fallback)
		}
	}
}

func TestFormatterIgnoresTransportEvents(t *testing.T) {
	for _, event := range []string{"heartbeat", "eof"} {
		var out bytes.Buffer
		f := &formatter{w: &out, style: defaultBarStyle()}
		body := map[string]any{"event": event, "data": map[string]any{"status": "job/success"}}
		if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
			t.Fatal(err)
		}
		if out.Len() != 0 || f.fallback {
			t.Fatalf("event = %q, output = %q, fallback = %v", event, out.String(), f.fallback)
		}
	}

	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	started := map[string]any{"event": "started", "data": map[string]any{"total": 2}}
	if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: started}}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	eof := map[string]any{"event": "eof", "data": map[string]any{"status": "job/success"}}
	if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: eof}}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 || f.current != 0 || f.fallback {
		t.Fatalf("active sequence after eof: output = %q, current = %d, fallback = %v", out.String(), f.current, f.fallback)
	}
}

func TestRenderTruncatesToTerminalWidth(t *testing.T) {
	current, total := int64(1), int64(16)
	got, err := renderWidth(progress{Label: "Progress", State: "running", Current: &current, Total: &total, Message: "compute-hypervisor-status/ch-gva-2"}, defaultBarStyle(), false, 72)
	if err != nil {
		t.Fatal(err)
	}
	if width := uniseg.StringWidth(got); width != 72 {
		t.Fatalf("width = %d, want 72: %q", width, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("output = %q, want ellipsis", got)
	}
}

func TestRunFramesOutputAndNativeFallback(t *testing.T) {
	var in, out bytes.Buffer
	for _, req := range []formatterRequest{
		{Type: "formatter", RequestID: 1, Format: "progress", Event: "start", Color: true},
		{Type: "formatter", RequestID: 2, Format: "progress", Event: "item", Response: plugin.FormatterResponse{Body: map[string]any{"id": "work", "current": 1, "total": 2}}},
		{Type: "formatter", RequestID: 3, Format: "progress", Event: "item", Response: plugin.FormatterResponse{Body: map[string]any{"message": "cannot match command"}}},
		{Type: "formatter", RequestID: 4, Format: "progress", Event: "end"},
	} {
		if err := plugin.WriteMessage(&in, req); err != nil {
			t.Fatal(err)
		}
	}
	if err := run(&in, &out); err != nil {
		t.Fatal(err)
	}

	dec := plugin.NewDecoder(&out)
	var outputs []formatterOutput
	for {
		var msg formatterOutput
		if err := dec.ReadMessage(&msg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, msg)
	}
	dataFrames := 0
	for _, msg := range outputs {
		if msg.RequestID == 2 && len(msg.Data) > 0 {
			dataFrames++
		}
	}
	if dataFrames != 1 {
		t.Fatalf("outputs = %#v, want one progress frame", outputs)
	}
	if !slices.ContainsFunc(outputs, func(msg formatterOutput) bool {
		return msg.RequestID == 3 && msg.Fallback && msg.Complete
	}) {
		t.Fatalf("outputs = %#v, want native fallback", outputs)
	}
}

func TestFormatterShowsOnlySnapshotSummaryOnTTY(t *testing.T) {
	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	if err := f.Handle(formatterRequest{Event: "start", Color: true}); err != nil {
		t.Fatal(err)
	}
	body := recordSnapshot(
		map[string]any{"id": "status", "label": "Progress", "state": "running", "current": 0, "total": 1},
		map[string]any{"id": "rescue", "parent": "status", "label": "rescue", "state": "running", "current": 0, "total": 1},
		map[string]any{"id": "step-8", "parent": "rescue", "label": "sleep 2", "state": "dispatched", "message": "host-a"})
	if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "Progress") || !strings.Contains(got, "Group: rescue\n") || strings.Contains(got, "sleep 2") {
		t.Fatalf("output = %q", got)
	}
}

func TestFormatterLimitsActiveGroupsOnTTY(t *testing.T) {
	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	if err := f.Handle(formatterRequest{Event: "start", Color: true}); err != nil {
		t.Fatal(err)
	}
	records := []any{map[string]any{"id": "status", "label": "Progress", "state": "running", "current": 0, "total": 5}}
	for i := 1; i <= 5; i++ {
		records = append(records, map[string]any{"id": fmt.Sprint(i), "parent": "status", "label": fmt.Sprintf("parallel %d", i), "state": "running", "current": 0, "total": 1})
	}
	body := map[string]any{"event": "progress", "data": map[string]any{"records": records}}
	if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"Group: parallel 1\n", "Group: parallel 4\n", "+1 other active groups"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "Group: parallel 5\n") {
		t.Fatalf("output = %q", got)
	}
}

func TestFormatterGroupLimitCountsTreeLeaves(t *testing.T) {
	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	if err := f.Handle(formatterRequest{Event: "start", Color: true, PluginConfig: json.RawMessage(`{"max_groups":1}`)}); err != nil {
		t.Fatal(err)
	}
	body := recordSnapshot(
		map[string]any{"id": "status", "label": "Progress", "state": "running", "current": 0, "total": 2},
		map[string]any{"id": "deploy", "parent": "status", "label": "deploy", "state": "running", "current": 0, "total": 2},
		map[string]any{"id": "a", "parent": "deploy", "label": "a", "state": "running", "current": 0, "total": 1},
		map[string]any{"id": "b", "parent": "deploy", "label": "b", "state": "running", "current": 0, "total": 1})
	if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"Group: deploy\n", "  └─ a\n", "+1 other active groups"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "  └─ b\n") {
		t.Fatalf("output = %q", got)
	}
}

func TestFormatterRendersGroupTreeAndKeepsCompletedGroups(t *testing.T) {
	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	if err := f.Handle(formatterRequest{Event: "start", Color: true, PluginConfig: json.RawMessage(`{
		"keep_groups": true,
		"group_prefix": "Phase:",
		"success_icon": "done"
	}`)}); err != nil {
		t.Fatal(err)
	}
	body := recordSnapshot(
		map[string]any{"id": "status", "label": "Progress", "state": "running", "current": 1, "total": 2},
		map[string]any{"id": "deploy", "parent": "status", "label": "deploy", "state": "running", "current": 1, "total": 2},
		map[string]any{"id": "a", "parent": "deploy", "label": "parallel a", "state": "success", "current": 1, "total": 1},
		map[string]any{"id": "b", "parent": "deploy", "label": "parallel b", "state": "running", "current": 0, "total": 1})
	if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"Phase: deploy\n", "  ├─ parallel a done\n", "  └─ parallel b\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
}

func TestDecodeProgressOrdersInterleavedGroupsAsTree(t *testing.T) {
	body := recordSnapshot(
		map[string]any{"id": "status"},
		map[string]any{"id": "a", "parent": "status"},
		map[string]any{"id": "a1", "parent": "a"},
		map[string]any{"id": "b", "parent": "status"},
		map[string]any{"id": "b1", "parent": "b"},
		map[string]any{"id": "a2", "parent": "a"})
	progresses, _, err := decodeProgresses(body)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0)
	for _, p := range progresses {
		ids = append(ids, p.ID)
	}
	if got, want := strings.Join(ids, ","), "status,a,a1,a2,b,b1"; got != want {
		t.Fatalf("ids = %q, want %q", got, want)
	}
}

func TestDecodeProgressRejectsBrokenTree(t *testing.T) {
	for _, body := range []any{
		map[string]any{"id": "child", "parent": "missing"},
		[]any{map[string]any{"id": "child", "parent": "missing"}},
		[]any{map[string]any{"id": "a", "parent": "b"}, map[string]any{"id": "b", "parent": "a"}},
	} {
		if _, _, err := decodeProgresses(body); err == nil {
			t.Fatalf("tree %#v was accepted", body)
		}
	}
}

func TestFormatterPreservesForestOrder(t *testing.T) {
	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	if err := f.Handle(formatterRequest{Event: "start", Color: true}); err != nil {
		t.Fatal(err)
	}
	body := recordSnapshot(
		map[string]any{"id": "one", "label": "One", "state": "running"},
		map[string]any{"id": "one-child", "parent": "one", "label": "one child", "state": "running", "current": 0, "total": 1},
		map[string]any{"id": "two", "label": "Two", "state": "running"},
		map[string]any{"id": "two-child", "parent": "two", "label": "two child", "state": "running", "current": 0, "total": 1})
	if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	positions := []int{strings.Index(got, "One"), strings.Index(got, "Group: one child"), strings.Index(got, "Two"), strings.Index(got, "Group: two child")}
	if !slices.IsSorted(positions) || positions[0] < 0 {
		t.Fatalf("output = %q", got)
	}
}

func recordSnapshot(records ...any) map[string]any {
	return map[string]any{"event": "progress", "data": map[string]any{"records": records}}
}

func TestFormatterRedrawsTTYSnapshot(t *testing.T) {
	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	if err := f.Handle(formatterRequest{Event: "start", Color: true}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []any{
		[]any{
			map[string]any{"id": "prepare", "state": "running"},
			map[string]any{"id": "finish", "state": "pending"},
		},
		[]any{
			map[string]any{"id": "prepare", "state": "running"},
		},
		[]any{
			map[string]any{"id": "prepare", "state": "success"},
		},
	} {
		if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
			t.Fatal(err)
		}
	}
	want := "\r\x1b[2Kprepare  running\n\r\x1b[2Kfinish  pending" +
		"\r\x1b[1A\x1b[2Kprepare  running\n\r\x1b[2K\x1b[1A" +
		"\r\x1b[2Kprepare  success\n"
	if got := out.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestDecodeProgressRejectsIncompleteCount(t *testing.T) {
	_, err := decodeProgress(map[string]any{"label": "workflow", "current": 1})
	if err == nil || !strings.Contains(err.Error(), "current and total together") {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeProgressSnapshotRequiresUniqueIDs(t *testing.T) {
	for _, body := range []any{
		[]any{map[string]any{"label": "missing id"}},
		[]any{map[string]any{"id": "same"}, map[string]any{"id": "same"}},
	} {
		if _, _, err := decodeProgresses(body); err == nil {
			t.Fatalf("snapshot %#v was accepted", body)
		}
	}
}

func TestBarStyleFromConfigAndEnv(t *testing.T) {
	t.Setenv("RSH_PROGRESS_WIDTH", "5")
	t.Setenv("RSH_PROGRESS_MAX_GROUPS", "3")
	t.Setenv("RSH_PROGRESS_KEEP_GROUPS", "true")
	t.Setenv("RSH_PROGRESS_GROUP_PREFIX", "Phase:")
	t.Setenv("RSH_PROGRESS_SUCCESS_ICON", "ok")
	t.Setenv("RSH_PROGRESS_COLOR", "magenta")
	t.Setenv("RSH_PROGRESS_HEAD", ">")
	style, err := barStyleFromConfig(json.RawMessage(`{
		"width": 4,
		"max_groups": 2,
		"keep_groups": false,
		"group_prefix": "Task:",
		"success_icon": "done",
		"failure_icon": "failed",
		"cancelled_icon": "stopped",
		"color_start": "#7c3aed",
		"color_end": "#22d3ee",
		"fill": "=",
		"head": "+",
		"empty": ".",
		"start": "[",
		"end": "]"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if style.ColorStart != (rgb{R: 255, B: 255}) || style.ColorEnd != (rgb{R: 255, B: 255}) {
		t.Fatalf("solid colour = %#v -> %#v", style.ColorStart, style.ColorEnd)
	}
	if style.MaxGroups != 3 {
		t.Fatalf("max groups = %d, want 3", style.MaxGroups)
	}
	if !style.KeepGroups {
		t.Fatal("keep groups = false, want true")
	}
	if style.GroupPrefix != "Phase:" || style.SuccessIcon != "ok" || style.FailureIcon != "failed" || style.CancelledIcon != "stopped" {
		t.Fatalf("tree style = %#v", style)
	}
	current, total := int64(1), int64(2)
	got, err := render(progress{Label: "work", State: "running", Current: &current, Total: &total}, style, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "50% [=>...]  work  1/2 steps  running"; got != want {
		t.Fatalf("render() = %q, want %q", got, want)
	}
}

func TestBarStyleFromEnvRejectsInvalidWidth(t *testing.T) {
	t.Setenv("RSH_PROGRESS_WIDTH", "wide")
	if _, err := barStyleFromConfig(nil); err == nil {
		t.Fatal("expected invalid width error")
	}
}

func TestBarStyleFromConfigRejectsInvalidValues(t *testing.T) {
	for _, config := range []string{
		`null`,
		`{"width":0}`,
		`{"max_groups":0}`,
		`{"color":"orange"}`,
		`{"fill":""}`,
	} {
		if _, err := barStyleFromConfig(json.RawMessage(config)); err == nil {
			t.Fatalf("config %s was accepted", config)
		}
	}
}

func TestRenderUsesANSIColourWhenEnabled(t *testing.T) {
	current, total := int64(2), int64(2)
	got, err := render(progress{Label: "work", State: "running", Current: &current, Total: &total}, defaultBarStyle(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"\x1b[38;2;255;59;48m", "\x1b[38;2;255;45;149m"} {
		if !strings.Contains(got, code) {
			t.Fatalf("render() = %q, want gradient colour %q", got, code)
		}
	}
}

func TestGradientColor(t *testing.T) {
	start := rgb{R: 255}
	end := rgb{R: 255, B: 200}
	if got, want := gradientColor(start, end, 2, 5), (rgb{R: 255, B: 100}); got != want {
		t.Fatalf("gradientColor() = %#v, want %#v", got, want)
	}
}

func TestParseColor(t *testing.T) {
	got, err := parseColor("#ff2d95")
	if err != nil {
		t.Fatal(err)
	}
	if want := (rgb{R: 255, G: 45, B: 149}); got != want {
		t.Fatalf("parseColor() = %#v, want %#v", got, want)
	}
}
