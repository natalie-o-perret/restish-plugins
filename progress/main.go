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
	ID      string `json:"id"`
	Label   string `json:"label"`
	State   string `json:"state"`
	Current *int64 `json:"current"`
	Total   *int64 `json:"total"`
	Unit    string `json:"unit"`
	Message string `json:"message"`
	Summary bool   `json:"_summary"`
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
	if normalized, ok, skip := normalizeProgressInput(decoded); skip {
		return nil, false, nil
	} else if ok {
		raw, err = json.Marshal(normalized)
		if err != nil {
			return nil, false, fmt.Errorf("encode normalized progress record: %w", err)
		}
	}
	if len(raw) == 0 || raw[0] != '[' {
		p, err := decodeProgress(json.RawMessage(raw))
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
		if _, exists := ids[p.ID]; exists {
			return nil, true, fmt.Errorf("progress snapshot contains duplicate id %q", p.ID)
		}
		ids[p.ID] = struct{}{}
		progresses[i] = p
	}
	return progresses, true, nil
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
						if group, ok := objectField(step, "group"); ok && fmt.Sprint(group) != "" {
							if label, ok := record["label"]; ok && fmt.Sprint(label) != "" {
								record["label"] = fmt.Sprintf("%v: %v", group, label)
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
						records = append([]any{map[string]any{
							"id":       "status",
							"label":    "Progress",
							"state":    state,
							"current":  current,
							"total":    len(records),
							"message":  message,
							"_summary": true,
						}}, records...)
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
		progresses = progresses[:1]
	}
	lines := make([]string, len(progresses))
	terminal := len(progresses) > 0
	for i, p := range progresses {
		line, err := render(p, f.style, f.tty)
		if err != nil {
			return err
		}
		lines[i] = line
		terminal = terminal && terminalState(p.State)
	}

	if !f.tty {
		if !snapshot {
			if slices.Equal(lines, f.lastLines) {
				return nil
			}
			f.lastLines = slices.Clone(lines)
			_, err := fmt.Fprintln(f.w, lines[0])
			return err
		}
		if f.lastByID == nil {
			f.lastByID = make(map[string]string)
		}
		next := make(map[string]string, len(progresses))
		for i, p := range progresses {
			next[p.ID] = lines[i]
			if f.lastByID[p.ID] != lines[i] {
				if _, err := fmt.Fprintln(f.w, lines[i]); err != nil {
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
	parts := []string{p.Label}
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
		description += ": " + p.Message
	}
	return description
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

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
