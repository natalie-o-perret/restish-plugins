package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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
}

type formatter struct {
	w        io.Writer
	tty      bool
	active   bool
	lastLine string
	style    barStyle
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

	style, err := barStyleFromEnv()
	if err != nil {
		fail(err)
	}
	f := &formatter{w: os.Stdout, style: style}
	dec := plugin.NewDecoder(os.Stdin)
	for {
		var req plugin.FormatterRequest
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

func (f *formatter) Handle(req plugin.FormatterRequest) error {
	switch req.Event {
	case "start", "item":
		if req.Color {
			f.tty = true
		}
		if req.Response.Body == nil {
			return nil
		}
		p, err := decodeProgress(req.Response.Body)
		if err != nil {
			return err
		}
		return f.write(p)
	case "end":
		if f.tty && f.active {
			_, err := fmt.Fprintln(f.w)
			f.active = false
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported formatter event %q", req.Event)
	}
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

func (f *formatter) write(p progress) error {
	line, err := render(p, f.style, f.tty)
	if err != nil {
		return err
	}
	if line == f.lastLine {
		return nil
	}
	f.lastLine = line
	if !f.tty {
		_, err := fmt.Fprintln(f.w, line)
		return err
	}
	if _, err := fmt.Fprintf(f.w, "\r\x1b[2K%s", line); err != nil {
		return err
	}
	f.active = true
	if terminalState(p.State) {
		_, err := fmt.Fprintln(f.w)
		f.active = false
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

func barStyleFromEnv() (barStyle, error) {
	style := defaultBarStyle()
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
		return barStyle{}, fmt.Errorf("RSH_PROGRESS_FILL, RSH_PROGRESS_HEAD, and RSH_PROGRESS_EMPTY cannot be empty")
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
