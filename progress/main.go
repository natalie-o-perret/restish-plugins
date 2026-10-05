package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/rest-sh/restish/v2/plugin"
	"github.com/schollz/progressbar/v3"
)

type barStyle struct {
	Width      int
	MaxGroups  int
	KeepGroups bool
	ColorStart rgb
	ColorEnd   rgb
	Fill       string
	Head       string
	Empty      string
	Start      string
	End        string
}

func defaultBarStyle() barStyle {
	return barStyle{
		Width:      24,
		MaxGroups:  4,
		ColorStart: rgb{R: 255, G: 59, B: 48},
		ColorEnd:   rgb{R: 255, G: 45, B: 149},
		Fill:       "█",
		Head:       "█",
		Empty:      "░",
	}
}

type rgb struct{ R, G, B uint8 }

const (
	fillMarker  = "\ue000"
	headMarker  = "\ue001"
	emptyMarker = "\ue002"
)

type progress struct {
	ID      string          `json:"id"`
	Label   string          `json:"label"`
	State   string          `json:"state"`
	Current *int64          `json:"current"`
	Total   *int64          `json:"total"`
	Unit    string          `json:"unit"`
	Message string          `json:"message"`
	Summary bool            `json:"_summary"`
	Group   string          `json:"_group"`
	Path    []progressGroup `json:"_group_path"`
}

type progressGroup struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type formatterRequest struct {
	Type         string                   `cbor:"type"`
	Format       string                   `cbor:"format"`
	Color        bool                     `cbor:"color,omitempty"`
	Event        string                   `cbor:"event"`
	PluginConfig json.RawMessage          `cbor:"plugin_config,omitempty"`
	Response     plugin.FormatterResponse `cbor:"response"`
}

type barStyleConfig struct {
	Width      *int    `json:"width"`
	MaxGroups  *int    `json:"max_groups"`
	KeepGroups *bool   `json:"keep_groups"`
	Color      *string `json:"color"`
	ColorStart *string `json:"color_start"`
	ColorEnd   *string `json:"color_end"`
	Fill       *string `json:"fill"`
	Head       *string `json:"head"`
	Empty      *string `json:"empty"`
	Start      *string `json:"start"`
	End        *string `json:"end"`
}

type formatter struct {
	w           io.Writer
	tty         bool
	activeLines int
	lastLines   []string
	lastByID    map[string]string
	style       barStyle
}

func main() {
	manifest := plugin.Manifest{
		Name:              "progress",
		Version:           "0.1.0",
		Description:       "Render streamed progress records as a terminal progress bar",
		RestishAPIVersion: 2,
		Hooks:             []string{"formatter"},
		FormatterNames:    []string{"progress"},
	}
	if plugin.HandleStartupFlags(os.Stdout, manifest, nil) {
		return
	}

	f := &formatter{w: os.Stdout, style: defaultBarStyle()}
	dec := plugin.NewDecoder(os.Stdin)
	for {
		var req formatterRequest
		if err := dec.ReadMessage(&req); err != nil {
			fail(fmt.Errorf("read formatter request: %w", err))
		}
		if req.Type != "formatter" || req.Format != "progress" {
			fail(fmt.Errorf("unexpected formatter request %q/%q", req.Type, req.Format))
		}
		if err := f.Handle(req); err != nil {
			fail(err)
		}
		if req.Event == "end" {
			return
		}
	}
}

func (f *formatter) Handle(req formatterRequest) error {
	switch req.Event {
	case "start":
		style, err := barStyleFromConfig(req.PluginConfig)
		if err != nil {
			return err
		}
		f.style = style
		fallthrough
	case "item":
		if req.Color {
			f.tty = true
		}
		if req.Response.Body == nil {
			return nil
		}
		progresses, snapshot, err := decodeProgresses(req.Response.Body)
		if err != nil {
			return err
		}
		if progresses == nil && !snapshot {
			return nil
		}
		return f.write(progresses, snapshot)
	case "end":
		if f.tty && f.activeLines > 0 {
			_, err := fmt.Fprintln(f.w)
			f.activeLines = 0
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported formatter event %q", req.Event)
	}
}

func decodeProgresses(value any) ([]progress, bool, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false, fmt.Errorf("encode progress record: %w", err)
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, false, fmt.Errorf("decode progress record: %w", err)
	}
	normalizedInput := false
	if normalized, ok, skip := normalizeProgressInput(decoded); skip {
		return nil, false, nil
	} else if ok {
		normalizedInput = true
		raw, err = json.Marshal(normalized)
		if err != nil {
			return nil, false, fmt.Errorf("encode normalized progress record: %w", err)
		}
	}
	if !normalizedInput {
		stripProgressInternalFields(decoded)
		raw, err = json.Marshal(decoded)
		if err != nil {
			return nil, false, fmt.Errorf("encode progress record: %w", err)
		}
	}
	if len(raw) == 0 || raw[0] != '[' {
		p, err := decodeProgress(json.RawMessage(raw))
		if !normalizedInput {
			p.Summary, p.Group, p.Path = false, "", nil
		}
		return []progress{p}, false, err
	}

	var records []json.RawMessage
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, true, fmt.Errorf("decode progress snapshot: %w", err)
	}
	progresses := make([]progress, len(records))
	ids := make(map[string]struct{}, len(records))
	for i, record := range records {
		p, err := decodeProgress(record)
		if err != nil {
			return nil, true, fmt.Errorf("progress snapshot item %d: %w", i, err)
		}
		if p.ID == "" {
			return nil, true, fmt.Errorf("progress snapshot item %d requires id", i)
		}
		if !normalizedInput {
			p.Summary, p.Group, p.Path = false, "", nil
		}
		if !p.Summary && p.Group == "" {
			if _, exists := ids[p.ID]; exists {
				return nil, true, fmt.Errorf("progress snapshot contains duplicate id %q", p.ID)
			}
			ids[p.ID] = struct{}{}
		}
		progresses[i] = p
	}
	return progresses, true, nil
}

func stripProgressInternalFields(value any) {
	strip := func(record map[string]any) {
		delete(record, "_summary")
		delete(record, "_group")
		delete(record, "_group_path")
	}
	switch value := value.(type) {
	case map[string]any:
		strip(value)
	case []any:
		for _, item := range value {
			if record, ok := item.(map[string]any); ok {
				strip(record)
			}
		}
	}
}

func normalizeProgressInput(value any) (any, bool, bool) {
	root, ok := value.(map[string]any)
	if !ok {
		return value, false, false
	}
	eventValue, event := objectField(root, "event")
	if event && fmt.Sprint(eventValue) == "eof" {
		return nil, false, true
	}
	if data, ok := objectField(root, "data"); ok {
		if object, ok := data.(map[string]any); ok {
			root = object
		}
	}
	if progressValue, ok := objectField(root, "progress"); ok {
		if progressObject, ok := progressValue.(map[string]any); ok {
			if stepsValue, ok := objectField(progressObject, "steps"); ok {
				if steps, ok := stepsValue.([]any); ok {
					records := make([]any, 0, len(steps))
					groupOrder := make([]string, 0)
					groupSteps := make(map[string][]map[string]any)
					groupPaths := make(map[string][]progressGroup)
					for _, stepValue := range steps {
						step, ok := stepValue.(map[string]any)
						if !ok {
							return value, false, false
						}
						record := make(map[string]any)
						for _, name := range []string{"id", "label", "state", "current", "total", "unit", "message"} {
							if field, ok := objectField(step, name); ok {
								record[name] = field
							}
						}
						if id, ok := record["id"]; ok {
							record["id"] = fmt.Sprint(id)
						}
						path := progressGroupPath(step)
						if len(path) > 0 {
							for i := range path {
								prefix := path[:i+1]
								key := progressGroupKey(prefix)
								if _, exists := groupSteps[key]; !exists {
									groupOrder = append(groupOrder, key)
									groupPaths[key] = slices.Clone(prefix)
								}
								groupSteps[key] = append(groupSteps[key], record)
							}
							if label, ok := record["label"]; ok && fmt.Sprint(label) != "" {
								record["label"] = fmt.Sprintf("%s: %v", path[len(path)-1].Label, label)
							}
						}
						if _, ok := record["message"]; !ok {
							if reason, ok := objectField(step, "reason"); ok {
								record["message"] = reason
							} else if target, ok := objectField(step, "target"); ok {
								record["message"] = target
							}
						}
						records = append(records, record)
					}
					if len(records) > 0 {
						groupOrder = treeGroupOrder(groupOrder, groupPaths)
						state := "running"
						if status, ok := objectField(root, "stream-status"); ok {
							state = fmt.Sprint(status)
							if _, suffix, found := strings.Cut(state, "/"); found {
								state = suffix
							}
							if state == "pending" {
								state = "running"
							}
						}
						current := 0
						message := ""
						for _, recordValue := range records {
							record := recordValue.(map[string]any)
							recordState := fmt.Sprint(record["state"])
							if terminalState(recordState) {
								current++
							}
							if recordState == "dispatched" || recordState == "running" {
								message = fmt.Sprint(record["label"])
							}
						}
						if terminalState(state) {
							current = len(records)
						}
						summaries := []any{map[string]any{
							"id":       "status",
							"label":    "Progress",
							"state":    state,
							"current":  current,
							"total":    len(records),
							"message":  message,
							"_summary": true,
						}}
						for _, groupKey := range groupOrder {
							path := groupPaths[groupKey]
							groupName := path[len(path)-1].Label
							steps := groupSteps[groupKey]
							groupCurrent := 0
							activeLabels := make([]string, 0)
							failed := false
							cancelled := false
							for _, step := range steps {
								stepState := fmt.Sprint(step["state"])
								if terminalState(stepState) {
									groupCurrent++
								}
								if stepState == "dispatched" || stepState == "running" {
									activeLabels = append(activeLabels, strings.TrimPrefix(fmt.Sprint(step["label"]), groupName+": "))
								} else if failureState(stepState) {
									failed = true
								} else if cancelledState(stepState) {
									cancelled = true
								}
							}
							if terminalState(state) {
								activeLabels = nil
								if state == "success" && !failed && !cancelled {
									groupCurrent = len(steps)
								}
							}
							if groupCurrent == 0 && len(activeLabels) == 0 && !terminalState(state) {
								continue
							}
							groupState := "running"
							if len(activeLabels) == 0 && (groupCurrent == len(steps) || terminalState(state) || failed || cancelled) {
								switch {
								case failed || failureState(state) && groupCurrent < len(steps):
									groupState = "failure"
								case cancelled || cancelledState(state) && groupCurrent < len(steps):
									groupState = "cancelled"
								default:
									groupState = "success"
								}
							}
							groupMessage := ""
							if len(activeLabels) == 1 {
								groupMessage = activeLabels[0]
							} else if len(activeLabels) > 1 {
								groupMessage = fmt.Sprintf("%d running", len(activeLabels))
							}
							summaries = append(summaries, map[string]any{
								"id": "group:" + groupKey, "label": groupName, "_group": groupName, "_group_path": path,
								"state": groupState, "current": groupCurrent, "total": len(steps), "message": groupMessage,
							})
						}
						records = append(summaries, records...)
					}
					return records, true, false
				}
			}
		}
	}
	if event {
		state := fmt.Sprint(eventValue)
		message := ""
		if status, ok := objectField(root, "status"); ok {
			state = fmt.Sprint(status)
			if _, suffix, found := strings.Cut(state, "/"); found {
				state = suffix
			}
		} else if state == "timeout" {
			state, message = "error", "timeout"
		}
		return []any{map[string]any{
			"id": "status", "label": "Progress", "state": state,
			"current": 1, "total": 1, "message": message,
		}}, true, false
	}
	return value, false, false
}

func objectField(object map[string]any, name string) (any, bool) {
	if value, ok := object[name]; ok {
		return value, true
	}
	for key, value := range object {
		if strings.HasSuffix(key, "/"+name) {
			return value, true
		}
	}
	return nil, false
}

func progressGroupPath(step map[string]any) []progressGroup {
	if value, ok := objectField(step, "group-path"); ok {
		if items, ok := value.([]any); ok {
			path := make([]progressGroup, 0, len(items))
			valid := true
			for _, item := range items {
				object, ok := item.(map[string]any)
				if !ok {
					valid = false
					break
				}
				id, idOK := objectField(object, "id")
				label, labelOK := objectField(object, "label")
				if !idOK || !labelOK || fmt.Sprint(id) == "" || fmt.Sprint(label) == "" {
					valid = false
					break
				}
				path = append(path, progressGroup{ID: fmt.Sprint(id), Label: fmt.Sprint(label)})
			}
			if valid && len(path) > 0 {
				return path
			}
		}
	}
	if group, ok := objectField(step, "group"); ok && fmt.Sprint(group) != "" {
		name := fmt.Sprint(group)
		return []progressGroup{{ID: name, Label: name}}
	}
	return nil
}

func progressGroupKey(path []progressGroup) string {
	var key strings.Builder
	for _, group := range path {
		fmt.Fprintf(&key, "%d:%s", len(group.ID), group.ID)
	}
	return key.String()
}

func treeGroupOrder(order []string, paths map[string][]progressGroup) []string {
	children := make(map[string][]string)
	for _, key := range order {
		path := paths[key]
		parent := progressGroupKey(path[:len(path)-1])
		children[parent] = append(children[parent], key)
	}
	result := make([]string, 0, len(order))
	var appendChildren func(string)
	appendChildren = func(parent string) {
		for _, child := range children[parent] {
			result = append(result, child)
			appendChildren(child)
		}
	}
	appendChildren("")
	return result
}

func decodeProgress(value any) (progress, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return progress{}, fmt.Errorf("encode progress record: %w", err)
	}
	var p progress
	if err := json.Unmarshal(raw, &p); err != nil {
		return progress{}, fmt.Errorf("progress formatter expects an object with integer current and total fields: %w", err)
	}
	if p.Label == "" {
		p.Label = p.ID
	}
	if p.Label == "" {
		return progress{}, fmt.Errorf("progress formatter requires label or id")
	}
	if p.State == "" {
		p.State = "running"
	}
	if p.Current != nil && *p.Current < 0 || p.Total != nil && *p.Total < 0 {
		return progress{}, fmt.Errorf("progress formatter requires non-negative current and total")
	}
	if (p.Current == nil) != (p.Total == nil) {
		return progress{}, fmt.Errorf("progress formatter requires current and total together")
	}
	if p.Total != nil && *p.Total == 0 {
		return progress{}, fmt.Errorf("progress formatter requires total greater than zero")
	}
	if p.Total != nil && *p.Current > *p.Total {
		return progress{}, fmt.Errorf("progress formatter requires current no greater than total")
	}
	return p, nil
}

func (f *formatter) write(progresses []progress, snapshot bool) error {
	if f.tty && len(progresses) > 0 && progresses[0].Summary {
		compact := []progress{progresses[0]}
		activeLeaves := make([][]progressGroup, 0)
		for _, p := range progresses[1:] {
			if p.Group == "" || terminalState(p.State) || hasActiveDescendant(p, progresses[1:]) {
				continue
			}
			activeLeaves = append(activeLeaves, groupPath(p))
		}
		visibleLeaves := activeLeaves[:min(len(activeLeaves), f.style.MaxGroups)]
		for _, p := range progresses[1:] {
			if p.Group == "" {
				continue
			}
			if terminalState(p.State) {
				if f.style.KeepGroups {
					compact = append(compact, p)
				}
				continue
			}
			if slices.ContainsFunc(visibleLeaves, func(leaf []progressGroup) bool {
				return groupPathPrefix(groupPath(p), leaf)
			}) {
				compact = append(compact, p)
			}
		}
		if len(activeLeaves) > f.style.MaxGroups {
			compact = append(compact, progress{ID: "group-overflow", Label: fmt.Sprintf("+%d other active groups", len(activeLeaves)-f.style.MaxGroups), State: "running"})
		}
		progresses = compact
	} else if !f.tty && snapshot {
		progresses = slices.DeleteFunc(progresses, func(p progress) bool { return p.Group != "" })
	}
	rendered := make([]string, len(progresses))
	terminal := len(progresses) > 0
	for i, p := range progresses {
		if p.Group != "" {
			p.Label = ""
		}
		line, err := render(p, f.style, f.tty)
		if err != nil {
			return err
		}
		rendered[i] = line
		terminal = terminal && terminalState(p.State)
	}
	lines := rendered
	if f.tty {
		lines = renderTTYLines(progresses, rendered)
	}

	if !f.tty {
		if !snapshot {
			if slices.Equal(rendered, f.lastLines) {
				return nil
			}
			f.lastLines = slices.Clone(rendered)
			_, err := fmt.Fprintln(f.w, rendered[0])
			return err
		}
		if f.lastByID == nil {
			f.lastByID = make(map[string]string)
		}
		next := make(map[string]string, len(progresses))
		for i, p := range progresses {
			next[p.ID] = rendered[i]
			if f.lastByID[p.ID] != rendered[i] {
				if _, err := fmt.Fprintln(f.w, rendered[i]); err != nil {
					return err
				}
			}
		}
		f.lastByID = next
		return nil
	}

	if slices.Equal(lines, f.lastLines) {
		return nil
	}
	rows := max(f.activeLines, len(lines))
	if _, err := fmt.Fprint(f.w, "\r"); err != nil {
		return err
	}
	if f.activeLines > 1 {
		if _, err := fmt.Fprintf(f.w, "\x1b[%dA", f.activeLines-1); err != nil {
			return err
		}
	}
	for i := range rows {
		if i > 0 {
			if _, err := fmt.Fprint(f.w, "\n\r"); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(f.w, "\x1b[2K"); err != nil {
			return err
		}
		if i < len(lines) {
			if _, err := fmt.Fprint(f.w, lines[i]); err != nil {
				return err
			}
		}
	}
	if rows > len(lines) && len(lines) > 0 {
		if _, err := fmt.Fprintf(f.w, "\x1b[%dA", rows-len(lines)); err != nil {
			return err
		}
	}
	f.lastLines = slices.Clone(lines)
	f.activeLines = len(lines)
	if terminal && f.activeLines > 0 {
		_, err := fmt.Fprintln(f.w)
		f.activeLines = 0
		return err
	}
	return nil
}

func render(p progress, style barStyle, color bool) (string, error) {
	if p.Total == nil {
		return progressDescription(p), nil
	}
	theme := progressbar.Theme{
		Saucer:        fillMarker,
		SaucerHead:    headMarker,
		SaucerPadding: emptyMarker,
		BarStart:      style.Start,
		BarEnd:        style.End,
	}
	bar := progressbar.NewOptions64(*p.Total,
		progressbar.OptionSetWriter(io.Discard),
		progressbar.OptionSetWidth(style.Width),
		progressbar.OptionSetTheme(theme),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionSetElapsedTime(false),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionShowDescriptionAtLineEnd(),
		progressbar.OptionSetDescription(progressDescription(p)),
	)
	if err := bar.Set64(*p.Current); err != nil {
		return "", fmt.Errorf("render progress: %w", err)
	}
	line := strings.TrimSpace(strings.TrimPrefix(bar.String(), "\r"))
	return renderBarCells(line, style, color), nil
}

func renderTTYLines(progresses []progress, rendered []string) []string {
	lines := make([]string, 0, len(rendered))
	var previousPath []progressGroup
	for i, p := range progresses {
		if p.Group == "" {
			lines = append(lines, strings.Split(rendered[i], "\n")...)
			previousPath = nil
			continue
		}
		path := groupPath(p)
		common := 0
		for common < len(path) && common < len(previousPath) && path[common].ID == previousPath[common].ID {
			common++
		}
		for depth := common; depth < len(path); depth++ {
			label := singleLine(path[depth].Label)
			if depth == len(path)-1 {
				label += stateIcon(p.State)
			}
			if depth == 0 {
				lines = append(lines, "Group: "+label)
			} else {
				branch := "└─ "
				if hasLaterGroupSibling(progresses, i, path, depth) {
					branch = "├─ "
				}
				lines = append(lines, strings.Repeat("  ", depth)+branch+label)
			}
		}
		for _, line := range strings.Split(rendered[i], "\n") {
			lines = append(lines, strings.Repeat("  ", len(path))+line)
		}
		previousPath = path
	}
	return lines
}

func groupPath(p progress) []progressGroup {
	if len(p.Path) > 0 {
		return p.Path
	}
	return []progressGroup{{ID: p.Group, Label: p.Group}}
}

func groupPathPrefix(prefix, path []progressGroup) bool {
	if len(prefix) > len(path) {
		return false
	}
	for i := range prefix {
		if prefix[i].ID != path[i].ID {
			return false
		}
	}
	return true
}

func hasActiveDescendant(group progress, progresses []progress) bool {
	path := groupPath(group)
	return slices.ContainsFunc(progresses, func(candidate progress) bool {
		candidatePath := groupPath(candidate)
		return candidate.Group != "" && !terminalState(candidate.State) && len(candidatePath) > len(path) && groupPathPrefix(path, candidatePath)
	})
}

func hasLaterGroupSibling(progresses []progress, index int, path []progressGroup, depth int) bool {
	for _, candidate := range progresses[index+1:] {
		if candidate.Group == "" || len(candidate.Path) <= depth {
			continue
		}
		sameParent := true
		for i := 0; i < depth; i++ {
			if len(candidate.Path) <= i || candidate.Path[i].ID != path[i].ID {
				sameParent = false
				break
			}
		}
		if sameParent && candidate.Path[depth].ID != path[depth].ID {
			return true
		}
	}
	return false
}

func stateIcon(state string) string {
	switch state {
	case "success":
		return " ✅"
	case "failure", "failed", "error":
		return " ❌"
	case "cancelled", "canceled":
		return " 🚫"
	default:
		return ""
	}
}

func renderBarCells(line string, style barStyle, color bool) string {
	var out strings.Builder
	position := 0
	for _, cell := range line {
		switch string(cell) {
		case fillMarker, headMarker:
			glyph := style.Fill
			if string(cell) == headMarker {
				glyph = style.Head
			}
			if color {
				c := gradientColor(style.ColorStart, style.ColorEnd, position, style.Width)
				fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%dm%s\x1b[0m", c.R, c.G, c.B, glyph)
			} else {
				out.WriteString(glyph)
			}
			position++
		case emptyMarker:
			out.WriteString(style.Empty)
			position++
		default:
			out.WriteRune(cell)
		}
	}
	return out.String()
}

func gradientColor(start, end rgb, position, width int) rgb {
	if width <= 1 {
		return start
	}
	position = max(0, min(position, width-1))
	interpolate := func(a, b uint8) uint8 {
		return uint8((int(a)*(width-1-position) + int(b)*position) / (width - 1))
	}
	return rgb{
		R: interpolate(start.R, end.R),
		G: interpolate(start.G, end.G),
		B: interpolate(start.B, end.B),
	}
}

func progressDescription(p progress) string {
	parts := make([]string, 0, 3)
	if p.Label != "" {
		parts = append(parts, singleLine(p.Label))
	}
	if p.Total != nil {
		unit := p.Unit
		if unit == "" {
			unit = "steps"
			if *p.Total == 1 {
				unit = "step"
			}
		}
		parts = append(parts, fmt.Sprintf("%d/%d %s", *p.Current, *p.Total, unit))
	}
	parts = append(parts, p.State)
	description := strings.Join(parts, "  ")
	if p.Message != "" {
		description += ": " + singleLine(p.Message)
	}
	return description
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func barStyleFromConfig(raw json.RawMessage) (barStyle, error) {
	style := defaultBarStyle()
	if len(raw) != 0 {
		var config *barStyleConfig
		if err := json.Unmarshal(raw, &config); err != nil || config == nil {
			if err == nil {
				return barStyle{}, fmt.Errorf("progress config must be a JSON object")
			}
			return barStyle{}, fmt.Errorf("invalid progress config: %w", err)
		}
		if config.Width != nil {
			if *config.Width < 1 || *config.Width > 200 {
				return barStyle{}, fmt.Errorf("progress config width must be an integer from 1 to 200")
			}
			style.Width = *config.Width
		}
		if config.MaxGroups != nil {
			if *config.MaxGroups < 1 || *config.MaxGroups > 20 {
				return barStyle{}, fmt.Errorf("progress config max_groups must be an integer from 1 to 20")
			}
			style.MaxGroups = *config.MaxGroups
		}
		if config.KeepGroups != nil {
			style.KeepGroups = *config.KeepGroups
		}
		if config.Color != nil {
			parsed, err := parseColor(*config.Color)
			if err != nil {
				return barStyle{}, fmt.Errorf("progress config color: %w", err)
			}
			style.ColorStart, style.ColorEnd = parsed, parsed
		}
		for _, value := range []struct {
			name   string
			value  *string
			target *rgb
		}{
			{"color_start", config.ColorStart, &style.ColorStart},
			{"color_end", config.ColorEnd, &style.ColorEnd},
		} {
			if value.value == nil {
				continue
			}
			parsed, err := parseColor(*value.value)
			if err != nil {
				return barStyle{}, fmt.Errorf("progress config %s: %w", value.name, err)
			}
			*value.target = parsed
		}
		for _, value := range []struct {
			value  *string
			target *string
		}{
			{config.Fill, &style.Fill},
			{config.Head, &style.Head},
			{config.Empty, &style.Empty},
			{config.Start, &style.Start},
			{config.End, &style.End},
		} {
			if value.value != nil {
				*value.target = *value.value
			}
		}
	}

	if value := os.Getenv("RSH_PROGRESS_WIDTH"); value != "" {
		width, err := strconv.Atoi(value)
		if err != nil || width < 1 || width > 200 {
			return barStyle{}, fmt.Errorf("RSH_PROGRESS_WIDTH must be an integer from 1 to 200")
		}
		style.Width = width
	}
	if value := os.Getenv("RSH_PROGRESS_MAX_GROUPS"); value != "" {
		maxGroups, err := strconv.Atoi(value)
		if err != nil || maxGroups < 1 || maxGroups > 20 {
			return barStyle{}, fmt.Errorf("RSH_PROGRESS_MAX_GROUPS must be an integer from 1 to 20")
		}
		style.MaxGroups = maxGroups
	}
	if value := os.Getenv("RSH_PROGRESS_KEEP_GROUPS"); value != "" {
		keepGroups, err := strconv.ParseBool(value)
		if err != nil {
			return barStyle{}, fmt.Errorf("RSH_PROGRESS_KEEP_GROUPS must be true or false")
		}
		style.KeepGroups = keepGroups
	}
	for name, target := range map[string]*string{
		"RSH_PROGRESS_FILL":  &style.Fill,
		"RSH_PROGRESS_HEAD":  &style.Head,
		"RSH_PROGRESS_EMPTY": &style.Empty,
		"RSH_PROGRESS_START": &style.Start,
		"RSH_PROGRESS_END":   &style.End,
	} {
		if value, ok := os.LookupEnv(name); ok {
			*target = value
		}
	}
	if color := os.Getenv("RSH_PROGRESS_COLOR"); color != "" {
		parsed, err := parseColor(color)
		if err != nil {
			return barStyle{}, fmt.Errorf("RSH_PROGRESS_COLOR: %w", err)
		}
		style.ColorStart, style.ColorEnd = parsed, parsed
	}
	for name, target := range map[string]*rgb{
		"RSH_PROGRESS_COLOR_START": &style.ColorStart,
		"RSH_PROGRESS_COLOR_END":   &style.ColorEnd,
	} {
		if value := os.Getenv(name); value != "" {
			parsed, err := parseColor(value)
			if err != nil {
				return barStyle{}, fmt.Errorf("%s: %w", name, err)
			}
			*target = parsed
		}
	}
	if style.Fill == "" || style.Head == "" || style.Empty == "" {
		return barStyle{}, fmt.Errorf("progress fill, head, and empty cannot be empty")
	}
	return style, nil
}

func parseColor(value string) (rgb, error) {
	colors := map[string]rgb{
		"black":   {},
		"blue":    {B: 255},
		"cyan":    {G: 255, B: 255},
		"green":   {G: 255},
		"magenta": {R: 255, B: 255},
		"red":     {R: 255},
		"white":   {R: 255, G: 255, B: 255},
		"yellow":  {R: 255, G: 255},
	}
	if color, ok := colors[strings.ToLower(value)]; ok {
		return color, nil
	}
	if len(value) == 7 && value[0] == '#' {
		n, err := strconv.ParseUint(value[1:], 16, 24)
		if err == nil {
			return rgb{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n)}, nil
		}
	}
	return rgb{}, fmt.Errorf("must be a basic colour name or #RRGGBB")
}

func terminalState(state string) bool {
	switch state {
	case "success", "failure", "failed", "error", "cancelled", "canceled":
		return true
	default:
		return false
	}
}

func failureState(state string) bool {
	return state == "failure" || state == "failed" || state == "error"
}

func cancelledState(state string) bool {
	return state == "cancelled" || state == "canceled"
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
