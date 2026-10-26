package dashboard

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/canvas"
	"github.com/NimbleMarkets/ntcharts/linechart"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/lipgloss"
	"github.com/jamesl33/hive-cli/internal/history"
)

const (
	// batteryBarWidth is the number of cells in a battery bar.
	batteryBarWidth = 10

	// lowBattery and mediumBattery are the percentages below which a battery is shown red and amber.
	lowBattery    = 20
	mediumBattery = 40

	// groupKeys are the keys which pick a group of devices, in the order of the groups. A letter then
	// hides a device in the group, so no letter is taken by another key.
	groupKeys = "1234567890"

	// staleAfter is how long a device can go without reporting before the legend greys it out.
	staleAfter = 2 * time.Hour

	// staleColor is the colour of a stale device in the legend.
	staleColor = lipgloss.Color("240")

	// targetColor is the colour of the target line on the temperature chart.
	targetColor = lipgloss.Color("231")

	// usageStep is the time covered by each reading of energy use.
	usageStep = 30 * time.Minute

	// minUsageHeight is the shortest usage chart which is drawn.
	minUsageHeight = 3
)

// usageColors are the metrics of energy used, which the usage chart draws, with their colours.
var usageColors = map[history.Metric]lipgloss.Color{history.Gas: "214", history.Electricity: "39"}

// group is a kind of device, which the legend lists apart from the others.
type group int

const (
	groupReceiver group = iota
	groupSensors
	groupValves
)

// groupNone is no group.
const groupNone group = -1

// maxGroupDevices is the number of devices which can be hidden in a group, which is the letters a to z.
const maxGroupDevices = 26

// groupNames are the names of the groups, in order.
var groupNames = []string{"Hive", "Sensors", "TRVs"}

// palette is the colours of the devices, in the order they are listed.
var palette = []lipgloss.Color{
	"39", "208", "42", "205", "220", "141", "51", "203", "118", "99",
}

// Boiler styles, by state.
var (
	boilerOn      = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Render("█")
	boilerIdle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("▃")
	boilerOff     = lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render("·")
	boilerUnknown = " "
)

// color returns the colour of the device at the position in the order.
func color(position int) lipgloss.Color {
	return palette[position%len(palette)]
}

// Strip returns the boiler state in each of the cells between from and to; a cell has the state of the
// last sample at or before its end, or -1 when nothing was known yet.
func Strip(s Series, from, to time.Time, cells int) []float64 {
	strip := make([]float64, cells)
	step := to.Sub(from) / time.Duration(cells)
	i := 0
	state := -1.0

	for c := range strip {
		end := from.Add(step * time.Duration(c+1))

		for i < len(s) && !s[i].At.After(end) {
			state = s[i].Value
			i++
		}

		strip[c] = state
	}

	return strip
}

// renderStrip draws the boiler state, one rune per cell.
func renderStrip(strip []float64) string {
	var b strings.Builder

	for _, v := range strip {
		switch v {
		case history.BoilerOn:
			b.WriteString(boilerOn)
		case history.BoilerIdle:
			b.WriteString(boilerIdle)
		case history.BoilerOff:
			b.WriteString(boilerOff)
		default:
			b.WriteString(boilerUnknown)
		}
	}

	return b.String()
}

// BatteryColor returns the colour of a battery bar for the percentage.
func BatteryColor(percent float64) lipgloss.Color {
	switch {
	case percent < lowBattery:
		return "196"
	case percent < mediumBattery:
		return "214"
	default:
		return "42"
	}
}

// batteryBar draws a bar for the percentage.
func batteryBar(percent float64) string {
	percent = math.Max(0, math.Min(100, percent))
	filled := int(math.Round(percent / 100 * batteryBarWidth))

	bar := strings.Repeat("█", filled) + strings.Repeat("░", batteryBarWidth-filled)

	return lipgloss.NewStyle().Foreground(BatteryColor(percent)).Render(bar)
}

// valueRange returns the range of the metrics across all devices since from, padded so lines do not
// touch the edges.
func valueRange(from time.Time, byDevice ...map[string]Series) (float64, float64) {
	lo, hi := math.Inf(1), math.Inf(-1)

	for _, m := range byDevice {
		for _, series := range m {
			for _, p := range visible(series, from) {
				lo, hi = math.Min(lo, p.Value), math.Max(hi, p.Value)
			}
		}
	}

	if math.IsInf(lo, 1) {
		return 0, 1
	}

	pad := math.Max((hi-lo)*0.1, 0.5)

	return lo - pad, hi + pad
}

// chart is a rendered chart together with where its plot area lies, so other rows can line up with it.
type chart struct {
	view  string
	left  int
	width int
}

// renderChart draws a line for each device which has samples of the metric, except the devices which
// are hidden.
func (s *Store) renderChart(metric history.Metric, hide map[string]bool, w, h int, from, to time.Time) chart {
	byDevice := maps.Clone(s.Series[metric])
	targets := map[string]Series{}

	if metric == history.Temperature {
		targets = maps.Clone(s.Series[history.Target])
	}

	for device, hidden := range hide {
		if hidden {
			delete(byDevice, device)
			delete(targets, device)
		}
	}

	lo, hi := valueRange(from, byDevice, targets)

	fresh := func() timeserieslinechart.Model {
		c := timeserieslinechart.New(w, h,
			timeserieslinechart.WithTimeRange(from, to),
			timeserieslinechart.WithYRange(lo, hi),
			timeserieslinechart.WithXYSteps(1, 4),
			timeserieslinechart.WithYLabelFormatter(func(_ int, v float64) string { return fmt.Sprintf("%.1f", v) }),
		)

		c.XLabelFormatter = tickLabels(tickStep(to.Sub(from)), to.Sub(from)/time.Duration(c.GraphWidth()))

		return c
	}

	var plots []plot

	for i, device := range s.Order {
		points := downsample(clamp(spanned(visible(byDevice[device], from), to), from), from, to, w*2)
		plots = append(plots, plot{style: lipgloss.NewStyle().Foreground(color(i)), points: points})
	}

	for _, device := range slices.Sorted(maps.Keys(targets)) {
		points := downsample(clamp(spanned(stepped(visible(targets[device], from)), to), from), from, to, w*2)
		plots = append(plots, plot{style: lipgloss.NewStyle().Foreground(targetColor), points: points})
	}

	c := drawPlots(fresh, plots)

	return chart{view: c.View(), left: c.Origin().X + 1, width: c.GraphWidth()}
}

// plot is a line of a chart.
type plot struct {
	style  lipgloss.Style
	points Series
}

// drawPlots returns the chart made by fresh with the lines drawn. Each line is drawn on a chart of its
// own and the cells are combined, as a chart gives a cell to the line drawn last. A cell which more
// than one line passes through holds the dots of all of them, and takes the colours of the lines in
// turn along the row, so no line is hidden by another.
func drawPlots(fresh func() timeserieslinechart.Model, plots []plot) timeserieslinechart.Model {
	c := fresh()
	layers := drawLayers(fresh, plots)

	if len(layers) == 0 {
		return c
	}

	c.Clear()
	c.DrawXYAxisAndLabel()

	for y := range c.GraphHeight() {
		mergeRow(&c, layers, y)
	}

	return c
}

// drawLayers returns a chart made by fresh for each plot which has points, with the line drawn on it.
func drawLayers(fresh func() timeserieslinechart.Model, plots []plot) []timeserieslinechart.Model {
	var layers []timeserieslinechart.Model

	for _, p := range plots {
		if len(p.points) == 0 {
			continue
		}

		l := fresh()
		for _, pt := range p.points {
			l.Push(timeserieslinechart.TimePoint{Time: pt.At, Value: pt.Value})
		}

		l.SetStyle(p.style)
		l.DrawBraille()

		layers = append(layers, l)
	}

	return layers
}

// mergeRow sets the cells of the row of the chart to those of the layers combined.
func mergeRow(c *timeserieslinechart.Model, layers []timeserieslinechart.Model, y int) {
	left := c.Origin().X + 1

	for x := range c.GraphWidth() {
		at := canvas.Point{X: left + x, Y: y}

		cell, ok := mergeCell(layers, at, x)
		if ok {
			c.Canvas.SetCell(at, cell)
		}
	}
}

// mergeCell returns the cell at the point which holds the dots of every layer there, in the colour of
// the layer which column x falls to; it returns false if no layer has dots there.
func mergeCell(layers []timeserieslinechart.Model, at canvas.Point, x int) (canvas.Cell, bool) {
	var dots rune

	var styles []lipgloss.Style

	for i := range layers {
		cell := layers[i].Canvas.Cell(at)
		if cell.Rune <= brailleBlank || cell.Rune > brailleFull {
			continue
		}

		dots |= cell.Rune - brailleBlank

		styles = append(styles, cell.Style)
	}

	if len(styles) == 0 {
		return canvas.Cell{}, false
	}

	return canvas.Cell{Rune: brailleBlank + dots, Style: styles[x%len(styles)]}, true
}

// The braille runes with no dots and with all of them.
const (
	brailleBlank = '⠀'
	brailleFull  = '⣿'
)

// hasUsage returns whether any energy use has been recorded.
func (s *Store) hasUsage() bool {
	return len(s.usage()) > 0
}

// usageEnd returns when the newest reading of energy use ends, or the zero time if there is none.
func (s *Store) usageEnd() time.Time {
	var end time.Time

	for _, u := range s.usage() {
		if p, ok := u.series.Last(); ok && p.At.Add(usageStep).After(end) {
			end = p.At.Add(usageStep)
		}
	}

	return end
}

// usageCorner says how far behind the usage chart is, in the colour of a warning, as readings come
// from the smart meter about a day late and the line stops short of the right edge.
func (s *Store) usageCorner(now time.Time) string {
	end := s.usageEnd()
	behind := max(now.Sub(end), 0)

	return lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(formatWindow(behind) + " behind")
}

// fuelKeys are the keys which show or hide the fuels.
var fuelKeys = map[history.Metric]string{history.Gas: "g", history.Electricity: "e"}

// fuels lists each fuel with its key, colour and the energy used after the time, in kWh; one hidden from
// the usage chart is struck through.
func (s *Store) fuels(from time.Time, hide map[string]bool) string {
	var parts []string

	for _, u := range s.usage() {
		style := lipgloss.NewStyle().Foreground(usageColors[u.metric]).Strikethrough(hide[u.device])
		parts = append(parts, style.Render(fmt.Sprintf("%s ● %s %.1f kWh", fuelKeys[u.metric], u.device, u.series.sum(from))))
	}

	return strings.Join(parts, "  ")
}

// meter is the readings of energy used from one device.
type meter struct {
	metric history.Metric
	device string
	series Series
}

// usage returns the readings of each device which has any, in the order of their metric and name.
func (s *Store) usage() []meter {
	var meters []meter

	for _, metric := range slices.Sorted(maps.Keys(usageColors)) {
		meters = append(meters, s.meters(metric)...)
	}

	return meters
}

// meters returns the readings of each device of the metric, in the order of their name.
func (s *Store) meters(metric history.Metric) []meter {
	var meters []meter

	for _, device := range slices.Sorted(maps.Keys(s.Series[metric])) {
		meters = append(meters, meter{metric: metric, device: device, series: s.Series[metric][device]})
	}

	return meters
}

// sum returns the energy used in the readings which the chart draws, the ones ending after the time.
func (s Series) sum(from time.Time) float64 {
	total := 0.0

	for _, p := range current(s, from) {
		total += p.Value
	}

	return total
}

// usagePlot returns the line of the readings from the time to now, held for their half hour so it
// stops at the end of the newest, with at most 2*w points. The series must not be empty.
func usagePlot(series Series, from, now time.Time, w int, style lipgloss.Style) plot {
	last := series[len(series)-1]
	points := append(stepped(series), Point{At: last.At.Add(usageStep), Value: last.Value})

	return plot{style: style, points: downsample(clamp(points, from), from, now, w*2)}
}

// highest returns the largest value of the plots.
func highest(plots []plot) float64 {
	hi := 0.0

	for _, p := range plots {
		hi = math.Max(hi, p.points.peak())
	}

	return hi
}

// peak returns the largest value in the series, or 0 if it is empty.
func (s Series) peak() float64 {
	hi := 0.0

	for _, p := range s {
		hi = math.Max(hi, p.Value)
	}

	return hi
}

// renderUsage draws a line for each fuel of the energy used in each half hour from the time up to now,
// except the fuels which are hidden. Each reading is held for its half hour and the line stops at the
// end of the newest, so a reading which is late leaves a gap before now.
func (s *Store) renderUsage(w, h int, from, now time.Time, hide map[string]bool) string {
	var plots []plot

	for _, u := range s.usage() {
		series := current(u.series, from)
		if len(series) == 0 || hide[u.device] {
			continue
		}

		plots = append(plots, usagePlot(series, from, now, w, lipgloss.NewStyle().Foreground(usageColors[u.metric])))
	}

	hi := highest(plots)

	fresh := func() timeserieslinechart.Model {
		c := timeserieslinechart.New(w, h,
			timeserieslinechart.WithTimeRange(from, now),
			timeserieslinechart.WithYRange(0, math.Max(hi*1.1, 0.1)),
			timeserieslinechart.WithXYSteps(1, 4),
			timeserieslinechart.WithYLabelFormatter(func(_ int, v float64) string { return fmt.Sprintf("%.1f", v) }),
		)

		c.XLabelFormatter = tickLabels(tickStep(now.Sub(from)), now.Sub(from)/time.Duration(c.GraphWidth()))

		return c
	}

	c := drawPlots(fresh, plots)

	return c.View()
}

// tickSteps are the spacings of the labels on a time axis, which fall on multiples of the spacing.
var tickSteps = []time.Duration{
	10 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 10 * time.Minute,
	15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 4 * time.Hour, 6 * time.Hour,
	24 * time.Hour, 48 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour, 90 * 24 * time.Hour,
}

// maxTicks is the most labels on a time axis.
const maxTicks = 6

// tickStep returns the smallest spacing which gives the span no more than maxTicks labels.
func tickStep(span time.Duration) time.Duration {
	for _, step := range tickSteps {
		if span/step <= maxTicks {
			return step
		}
	}

	return tickSteps[len(tickSteps)-1]
}

// tickLabels returns a formatter which labels a column of the axis with the time of the multiple of
// step which falls in it, in local time, and leaves every other column blank. col is the time a
// column covers.
func tickLabels(step, col time.Duration) linechart.LabelFormatter {
	layout := "15:04"

	switch {
	case step >= 7*24*time.Hour:
		layout = "02 Jan"
	case step >= 24*time.Hour:
		layout = "Mon 02"
	case step < time.Minute:
		layout = "15:04:05"
	}

	return func(_ int, v float64) string {
		secs, frac := math.Modf(v)
		t := time.Unix(int64(secs), int64(frac*1e9))

		// Truncate counts from the zero time, so shift the time to local wall clock first.
		_, offset := t.Zone()
		wall := t.Add(time.Duration(offset) * time.Second)

		tick := wall.Add(col).Truncate(step)
		if !tick.After(wall) {
			return ""
		}

		return tick.UTC().Format(layout)
	}
}

// boilerOnTime returns how long the boiler fired between from and to; a state is held until the next
// sample, and the one before from holds into the span.
func boilerOnTime(s Series, from, to time.Time) time.Duration {
	var on time.Duration

	s = visible(s, from)

	for i, p := range s {
		if p.Value != history.BoilerOn {
			continue
		}

		start, end := p.At, to
		if start.Before(from) {
			start = from
		}

		if i+1 < len(s) && s[i+1].At.Before(to) {
			end = s[i+1].At
		}

		on += end.Sub(start)
	}

	return max(on, 0)
}

// formatOn formats the time the boiler fired with its share of the span, e.g. 3h12m on (13%).
func formatOn(on, span time.Duration) string {
	pct := int(on * 100 / span)

	if on < time.Hour {
		return fmt.Sprintf("%dm on (%d%%)", int(on.Minutes()), pct)
	}

	return fmt.Sprintf("%dh%dm on (%d%%)", int(on.Hours()), int(on.Minutes())%60, pct)
}

// visible returns the points which can be seen from the time on: those after it, and the one before
// which holds into the window. Only these are drawn, however much history has been loaded.
func visible(s Series, from time.Time) Series {
	i := sort.Search(len(s), func(i int) bool { return s[i].At.After(from) })

	return s[max(i-1, 0):]
}

// current returns the readings of energy use whose half hour ends after the time. Unlike visible, it
// keeps no reading from before, as a reading does not hold past its half hour.
func current(s Series, from time.Time) Series {
	i := sort.Search(len(s), func(i int) bool { return s[i].At.Add(usageStep).After(from) })

	return s[i:]
}

// clamp moves a leading point which lies before the time onto it, on the line to the next point, so
// every line starts at the left edge of the chart however far back its last earlier report was.
func clamp(s Series, from time.Time) Series {
	if len(s) < 2 || !s[0].At.Before(from) || s[1].At.Before(from) {
		return s
	}

	a, b := s[0], s[1]
	frac := float64(from.Sub(a.At)) / float64(b.At.Sub(a.At))

	out := slices.Clone(s)
	out[0] = Point{At: from, Value: a.Value + (b.Value-a.Value)*frac}

	return out
}

// downsample reduces the series to the lowest and highest point in each of the buckets which the span
// from the time to the time is divided into, so a chart with that many columns of dots draws the same
// lines from fewer points. The first and last points are kept, as they anchor the ends of the line.
//
// The algorithm is min/max bucketing:
//  1. The span is divided into equal buckets of step = (to - from) / buckets.
//  2. Each point falls in bucket (At - from) / step. The series is time ordered, so each bucket is a
//     contiguous run, found in one pass.
//  3. Per run, the points with the lowest and highest value are kept, in time order, so the line still
//     rises and falls through the same extremes. A run with one point, or equal extremes, keeps one.
//  4. A series of 2*buckets points or fewer is returned as is, as it cannot shrink.
//
// The output has at most 2*buckets+2 points, and nothing is averaged, so spikes are never smoothed away.
func downsample(s Series, from, to time.Time, buckets int) Series {
	step := to.Sub(from) / time.Duration(max(buckets, 1))
	if step <= 0 || len(s) <= 2*buckets {
		return s
	}

	out := make(Series, 0, 2*buckets+2)
	out = append(out, s[0])

	for i := 1; i < len(s); {
		bucket := s[i].At.Sub(from) / step
		lo, hi := i, i
		j := i

		for ; j < len(s) && s[j].At.Sub(from)/step == bucket; j++ {
			if s[j].Value < s[lo].Value {
				lo = j
			}

			if s[j].Value > s[hi].Value {
				hi = j
			}
		}

		lo, hi = min(lo, hi), max(lo, hi)

		out = append(out, s[lo])
		if hi != lo {
			out = append(out, s[hi])
		}

		i = j
	}

	if last := s[len(s)-1]; out[len(out)-1] != last {
		out = append(out, last)
	}

	return out
}

// stepped holds each value until the next point, so the line rises and falls in steps instead of
// sloping across the gap between reports.
func stepped(s Series) Series {
	if len(s) == 0 {
		return nil
	}

	out := make(Series, 0, 2*len(s))
	out = append(out, s[0])

	for _, p := range s[1:] {
		prev := out[len(out)-1]
		if prev.Value != p.Value {
			out = append(out, Point{At: p.At, Value: prev.Value})
		}

		out = append(out, p)
	}

	return out
}

// spanned extends a series up to the present, holding its last value, so the line is not cut short
// between reports.
func spanned(s Series, to time.Time) Series {
	if len(s) == 0 {
		return nil
	}

	out := make(Series, 0, len(s)+2)

	out = append(out, s...)

	if last := s[len(s)-1]; last.At.Before(to) {
		out = append(out, Point{At: to, Value: last.Value})
	}

	return out
}

// lastSeen returns when the device last reported a measurement, or the zero time if it never has.
func (s *Store) lastSeen(device string) time.Time {
	var seen time.Time

	for _, byDevice := range s.Series {
		if p, ok := byDevice[device].Last(); ok && p.At.After(seen) {
			seen = p.At
		}
	}

	return seen
}

// legend lists the devices of each group on a line, each with its key, colour and latest readings; a
// device which has not reported for staleAfter is greyed out and shows how long ago it did, and one
// hidden from the charts is struck through.
func (s *Store) legend(now time.Time, hide map[string]bool, picking group) string {
	parts := make([][]string, len(groupNames))

	for i, device := range s.Order {
		g := s.group(device)
		parts[g] = append(parts[g], s.legendEntry(i, len(parts[g]), device, now, hide[device]))
	}

	var lines []string

	for g, entries := range parts {
		if len(entries) == 0 {
			continue
		}

		label := lipgloss.NewStyle().Reverse(group(g) == picking).Render(fmt.Sprintf("%-9s", groupKeys[g:g+1]+" "+groupNames[g]))

		lines = append(lines, label+" "+strings.Join(entries, "  "))
	}

	return strings.Join(lines, "\n")
}

// namePrefixes are the prefixes which name the kind of a device, e.g. "tah_lounge". The legend groups
// devices by kind so it leaves them out.
var namePrefixes = []string{"hive_", "tah_", "trv_"}

// DisplayName returns the name of the device without its kind prefix.
func DisplayName(device string) string {
	for _, prefix := range namePrefixes {
		name, ok := strings.CutPrefix(device, prefix)
		if ok {
			return name
		}
	}

	return device
}

// legendEntry returns the legend entry of the device at the position i in the order, and the position
// k in its group, which gives its letter.
func (s *Store) legendEntry(i, k int, device string, now time.Time, hidden bool) string {
	text := DisplayName(device)

	if p, ok := s.Series[history.Temperature][device].Last(); ok {
		text += fmt.Sprintf(" %.1f°C", p.Value)
	}

	style := lipgloss.NewStyle().Foreground(color(i))
	stale := now.Sub(s.lastSeen(device)) >= staleAfter

	if stale {
		style = lipgloss.NewStyle().Foreground(staleColor)
	}

	style = style.Strikethrough(hidden)

	key := ""
	if k < maxGroupDevices {
		key = string(rune('a'+k)) + " "
	}

	part := style.Render(key + "● " + text)

	if p, ok := s.Series[history.Target][device].Last(); ok {
		targetStyle := lipgloss.NewStyle().Foreground(targetColor).Strikethrough(hidden)
		if stale {
			targetStyle = style
		}

		part += targetStyle.Render(fmt.Sprintf(" (%.1f°C%s)", p.Value, s.gap(device, p.Value)))
	}

	text = ""

	if p, ok := s.Series[history.Setpoint][device].Last(); ok {
		text += fmt.Sprintf(" %.1f°C", p.Value)
	}

	if p, ok := s.Series[history.Valve][device].Last(); ok {
		text += fmt.Sprintf(" %.0f%%", p.Value)
	}

	if p, ok := s.Series[history.Humidity][device].Last(); ok {
		text += fmt.Sprintf(" %.0f%%", p.Value)
	}

	if stale {
		text += fmt.Sprintf(" (%s ago)", formatWindow(now.Sub(s.lastSeen(device))))
	}

	return part + style.Render(text)
}

// gap returns the signed difference between the temperature of the device and the target, e.g. ", +0.4",
// or nothing if it has no temperature.
func (s *Store) gap(device string, target float64) string {
	p, ok := s.Series[history.Temperature][device].Last()
	if !ok {
		return ""
	}

	return fmt.Sprintf(", %+.1f", p.Value-target)
}

// batteries lists the battery level of each device which reports one, but only if one of them is
// below mediumBattery; until then there is nothing to act on.
func (s *Store) batteries() []string {
	var lines []string

	low := false

	width := 0
	for device := range s.Series[history.Battery] {
		width = max(width, lipgloss.Width(device))
	}

	for i, device := range s.Order {
		p, ok := s.Series[history.Battery][device].Last()
		if !ok {
			continue
		}

		low = low || p.Value < mediumBattery

		name := lipgloss.NewStyle().Foreground(color(i)).Width(width).Render(device)
		lines = append(lines, fmt.Sprintf("%s %s %3.0f%%", name, batteryBar(p.Value), p.Value))
	}

	if !low {
		return nil
	}

	return lines
}

// boilerRows draws the boiler state across the width, with a key.
func (s *Store) boilerRows(width int, from, to time.Time) []string {
	strip := Strip(s.Series[history.Boiler][s.Receiver.Name], from, to, width)

	key := fmt.Sprintf("Boiler  %s on  %s idle  %s off", boilerOn, boilerIdle, boilerOff)

	return []string{key, renderStrip(strip)}
}
