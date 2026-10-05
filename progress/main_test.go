package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/rest-sh/restish/v2/plugin"
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
		map[string]any{
			"event": "job",
			"data": map[string]any{
				"job/stream-status": "job/pending",
				"job/progress": map[string]any{
					"progress/steps": []any{
						map[string]any{
							"progress/id":     8,
							"progress/group":  "rescue",
							"progress/label":  "sleep 2",
							"progress/state":  "dispatched",
							"progress/target": "host-a",
						},
					},
				},
			},
		},
		map[string]any{
			"event": "job",
			"data": map[string]any{
				"job/stream-status": "job/success",
				"job/progress": map[string]any{
					"progress/steps": []any{
						map[string]any{
							"progress/id":     8,
							"progress/group":  "rescue",
							"progress/label":  "sleep 2",
							"progress/state":  "success",
							"progress/target": "host-a",
						},
					},
				},
			},
		},
		map[string]any{"event": "eof", "data": map[string]any{"status": "job/success"}},
	} {
		if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
			t.Fatal(err)
		}
	}
	want := "0% ░░░░░░░░░░░░░░░░░░░░░░░░  Progress  0/1 step  running: rescue: sleep 2\n" +
		"rescue: sleep 2  dispatched: host-a\n" +
		"100% ████████████████████████  Progress  1/1 step  success\n" +
		"rescue: sleep 2  success: host-a\n"
	if got := out.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestFormatterShowsOnlySnapshotSummaryOnTTY(t *testing.T) {
	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	if err := f.Handle(formatterRequest{Event: "start", Color: true}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"event": "job",
		"data": map[string]any{
			"stream-status": "job/pending",
			"progress": map[string]any{
				"steps": []any{map[string]any{
					"id": 8, "group": "rescue", "label": "sleep 2", "state": "dispatched", "target": "host-a",
				}},
			},
		},
	}
	if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "Progress") || !strings.Contains(got, "Group: rescue\n") || strings.Contains(got, "dispatched: host-a") {
		t.Fatalf("output = %q", got)
	}
}

func TestFormatterLimitsActiveGroupsOnTTY(t *testing.T) {
	var out bytes.Buffer
	f := &formatter{w: &out, style: defaultBarStyle()}
	if err := f.Handle(formatterRequest{Event: "start", Color: true}); err != nil {
		t.Fatal(err)
	}
	steps := make([]any, 5)
	for i := range steps {
		steps[i] = map[string]any{"id": i, "group": fmt.Sprintf("parallel %d", i+1), "label": "work", "state": "running"}
	}
	body := map[string]any{"event": "job", "data": map[string]any{
		"stream-status": "job/pending", "progress": map[string]any{"steps": steps},
	}}
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
	body := map[string]any{"event": "job", "data": map[string]any{
		"stream-status": "job/pending",
		"progress": map[string]any{"steps": []any{
			map[string]any{"id": 1, "group-path": groupPathValues("deploy", "a"), "state": "running"},
			map[string]any{"id": 2, "group-path": groupPathValues("deploy", "b"), "state": "running"},
		}},
	}}
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
	if err := f.Handle(formatterRequest{Event: "start", Color: true, PluginConfig: json.RawMessage(`{"keep_groups":true}`)}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"event": "job", "data": map[string]any{
		"stream-status": "job/pending",
		"progress": map[string]any{"steps": []any{
			map[string]any{"id": 1, "group-path": groupPathValues("deploy", "parallel a"), "label": "done", "state": "success"},
			map[string]any{"id": 2, "group-path": groupPathValues("deploy", "parallel b"), "label": "work", "state": "running"},
		}},
	}}
	if err := f.Handle(formatterRequest{Event: "item", Response: plugin.FormatterResponse{Body: body}}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"Group: deploy\n", "  ├─ parallel a ✅\n", "  └─ parallel b\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
}

func TestDecodeProgressOrdersInterleavedGroupsAsTree(t *testing.T) {
	body := map[string]any{"event": "job", "data": map[string]any{
		"stream-status": "job/pending",
		"progress": map[string]any{"steps": []any{
			map[string]any{"id": 1, "group-path": groupPathValues("a", "a1"), "state": "running"},
			map[string]any{"id": 2, "group-path": groupPathValues("b", "b1"), "state": "running"},
			map[string]any{"id": 3, "group-path": groupPathValues("a", "a2"), "state": "running"},
		}},
	}}
	progresses, _, err := decodeProgresses(body)
	if err != nil {
		t.Fatal(err)
	}
	groups := make([]string, 0)
	for _, p := range progresses {
		if p.Group != "" {
			groups = append(groups, p.Group)
		}
	}
	if got, want := strings.Join(groups, ","), "a,a1,a2,b,b1"; got != want {
		t.Fatalf("groups = %q, want %q", got, want)
	}
}

func TestDecodeProgressMarksIncompleteGroupFailed(t *testing.T) {
	body := map[string]any{"event": "job", "data": map[string]any{
		"stream-status": "job/failure",
		"progress": map[string]any{"steps": []any{
			map[string]any{"id": 1, "group": "deploy", "state": "running"},
			map[string]any{"id": 2, "group": "deploy", "state": "not-started"},
		}},
	}}
	progresses, _, err := decodeProgresses(body)
	if err != nil {
		t.Fatal(err)
	}
	if got := progresses[1].State; got != "failure" {
		t.Fatalf("group state = %q, want failure", got)
	}
}

func TestDecodeProgressDoesNotReserveInternalFieldsForGenericSnapshots(t *testing.T) {
	body := []any{
		map[string]any{"id": "same", "_group": []any{"internal"}},
		map[string]any{"id": "same", "_summary": "internal"},
	}
	if _, _, err := decodeProgresses(body); err == nil {
		t.Fatal("generic duplicate IDs were accepted")
	}
	progresses, _, err := decodeProgresses(map[string]any{"id": "work", "label": "Work", "_group_path": "internal"})
	if err != nil {
		t.Fatal(err)
	}
	if progresses[0].Group != "" || progresses[0].Label != "Work" {
		t.Fatalf("progress = %#v", progresses[0])
	}
}

func TestDecodeProgressAllowsInternalGroupIDCollision(t *testing.T) {
	body := map[string]any{"event": "job", "data": map[string]any{
		"stream-status": "job/pending",
		"progress": map[string]any{"steps": []any{
			map[string]any{"id": "group:1:g", "group": "g", "state": "running"},
		}},
	}}
	if _, _, err := decodeProgresses(body); err != nil {
		t.Fatal(err)
	}
}

func groupPathValues(groups ...string) []any {
	result := make([]any, len(groups))
	for i, group := range groups {
		result[i] = map[string]any{"id": fmt.Sprintf("%d:%s", i, group), "label": group}
	}
	return result
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
	t.Setenv("RSH_PROGRESS_COLOR", "magenta")
	t.Setenv("RSH_PROGRESS_HEAD", ">")
	style, err := barStyleFromConfig(json.RawMessage(`{
		"width": 4,
		"max_groups": 2,
		"keep_groups": false,
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
