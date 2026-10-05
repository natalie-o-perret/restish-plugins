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
	Parent  string          `json:"parent"`
	Label   string          `json:"label"`
	State   string          `json:"state"`
	Current *int64          `json:"current"`
	Total   *int64          `json:"total"`
	Unit    string          `json:"unit"`
	Message string          `json:"message"`
	Path    []progressGroup `json:"-"`
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
		if err == nil && p.Parent != "" {
			err = fmt.Errorf("progress record %q requires a snapshot containing parent %q", p.ID, p.Parent)
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
		if _, exists := ids[p.ID]; exists {
			return nil, true, fmt.Errorf("progress snapshot contains duplicate id %q", p.ID)
		}
		ids[p.ID] = struct{}{}
		progresses[i] = p
	}
	progresses, err = orderProgressTree(progresses)
	if err != nil {
		return nil, true, err
	}
	return progresses, true, nil
}

func normalizeProgressInput(value any) (any, bool, bool) {
	root, ok := value.(map[string]any)
	if !ok {
		return value, false, false
	}
	if data, ok := root["data"]; ok {
		if records, ok := data.([]any); ok {
			return records, true, false
		}
		root, ok = data.(map[string]any)
		if !ok {
			return nil, false, true
		}
		if records, ok := root["records"].([]any); ok {
			return records, true, false
		}
		return nil, false, true
	}
	if records, ok := root["records"].([]any); ok {
		return records, true, false
	}
	if _, ok := root["event"]; ok {
		return nil, false, true
	}
	return value, false, false
}

func orderProgressTree(progresses []progress) ([]progress, error) {
	byID := make(map[string]progress, len(progresses))
	children := make(map[string][]string)
	for _, p := range progresses {
		byID[p.ID] = p
		children[p.Parent] = append(children[p.Parent], p.ID)
	}
	for _, p := range progresses {
		if p.Parent != "" {
			if _, ok := byID[p.Parent]; !ok {
				return nil, fmt.Errorf("progress record %q references missing parent %q", p.ID, p.Parent)
			}
		}
	}
	ordered := make([]progress, 0, len(progresses))
	visiting := make(map[string]bool)
	var walk func(string, []progressGroup) error
	walk = func(id string, path []progressGroup) error {
		if visiting[id] {
			return fmt.Errorf("progress tree contains a cycle at %q", id)
		}
		visiting[id] = true
		p := byID[id]
		if p.Parent != "" {
			path = append(slices.Clone(path), progressGroup{ID: p.ID, Label: p.Label})
			p.Path = path
		}
		ordered = append(ordered, p)
		for _, child := range children[id] {
			if err := walk(child, path); err != nil {
				return err
			}
		}
		visiting[id] = false
		return nil
	}
	for _, root := range children[""] {
		if err := walk(root, nil); err != nil {
			return nil, err
		}
	}
	if len(ordered) != len(progresses) {
		return nil, fmt.Errorf("progress tree contains a cycle")
	}
	return ordered, nil
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
	if f.tty && slices.ContainsFunc(progresses, func(p progress) bool { return p.Parent != "" }) {
		compact := make([]progress, 0, len(progresses))
		activeLeaves := make([][]progressGroup, 0)
		for _, p := range progresses {
			if !treeBar(p) || terminalState(p.State) || hasActiveDescendant(p, progresses) {
				continue
			}
			activeLeaves = append(activeLeaves, p.Path)
		}
		visibleLeaves := activeLeaves[:min(len(activeLeaves), f.style.MaxGroups)]
		for _, p := range progresses {
			if p.Parent == "" {
				compact = append(compact, p)
				continue
			}
			if !treeBar(p) {
				continue
			}
			if terminalState(p.State) {
				if f.style.KeepGroups {
					compact = append(compact, p)
				}
				continue
			}
			if slices.ContainsFunc(visibleLeaves, func(leaf []progressGroup) bool {
				return groupPathPrefix(p.Path, leaf)
			}) {
				compact = append(compact, p)
			}
		}
		if len(activeLeaves) > f.style.MaxGroups {
			compact = append(compact, progress{ID: "group-overflow", Label: fmt.Sprintf("+%d other active groups", len(activeLeaves)-f.style.MaxGroups), State: "running"})
		}
		progresses = compact
	}
	rendered := make([]string, len(progresses))
	terminal := len(progresses) > 0
	for i, p := range progresses {
		if f.tty && treeBar(p) {
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
		if !treeBar(p) {
			lines = append(lines, strings.Split(rendered[i], "\n")...)
			previousPath = nil
			continue
		}
		path := p.Path
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

func treeBar(p progress) bool {
	return p.Parent != "" && p.Total != nil
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
	path := group.Path
	return slices.ContainsFunc(progresses, func(candidate progress) bool {
		return treeBar(candidate) && !terminalState(candidate.State) && len(candidate.Path) > len(path) && groupPathPrefix(path, candidate.Path)
	})
}

func hasLaterGroupSibling(progresses []progress, index int, path []progressGroup, depth int) bool {
	for _, candidate := range progresses[index+1:] {
		if !treeBar(candidate) || len(candidate.Path) <= depth {
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

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
