package dashboard

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/lipgloss"
)

const (
	// batteryBarWidth is the number of cells in a battery bar.
	batteryBarWidth = 10

	// lowBattery and mediumBattery are the percentages below which a battery is shown red and amber.
	lowBattery    = 20
	mediumBattery = 40

	// staleAfter is how long a device can go without reporting before the legend greys it out.
	staleAfter = 2 * time.Hour

	// staleColor is the colour of a stale device in the legend.
	staleColor = lipgloss.Color("240")
)

// palette is the colours of the devices, in the order of their names.
var palette = []lipgloss.Color{"39", "208", "42", "205", "220", "141", "51", "203"}

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
		case BoilerOn:
			b.WriteString(boilerOn)
		case BoilerIdle:
			b.WriteString(boilerIdle)
		case BoilerOff:
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

// valueRange returns the range of a metric across all devices since from, padded so lines do not
// touch the edges.
func valueRange(byDevice map[string]Series, from time.Time) (float64, float64) {
	lo, hi := math.Inf(1), math.Inf(-1)

	for _, series := range byDevice {
		for _, p := range series {
			if p.At.Before(from) {
				continue
			}

			lo, hi = math.Min(lo, p.Value), math.Max(hi, p.Value)
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

// renderChart draws a line for each device which has samples of the metric.
func (s *Store) renderChart(metric Metric, w, h int, from, to time.Time) chart {
	byDevice := s.Series[metric]
	lo, hi := valueRange(byDevice, from)

	c := timeserieslinechart.New(w, h,
		timeserieslinechart.WithTimeRange(from, to),
		timeserieslinechart.WithYRange(lo, hi),
		timeserieslinechart.WithXYSteps(6, 4),
		timeserieslinechart.WithXLabelFormatter(timeserieslinechart.HourTimeLabelFormatter()),
		timeserieslinechart.WithYLabelFormatter(func(_ int, v float64) string { return fmt.Sprintf("%.1f", v) }),
	)

	for i, device := range s.Order {
		points := spanned(visible(byDevice[device], from), to)
		for _, p := range points {
			c.PushDataSet(device, timeserieslinechart.TimePoint{Time: p.At, Value: p.Value})
		}

		if len(points) > 0 {
			c.SetDataSetStyle(device, lipgloss.NewStyle().Foreground(color(i)))
		}
	}

	c.DrawBrailleAll()

	return chart{view: c.View(), left: c.Origin().X + 1, width: c.GraphWidth()}
}

// visible returns the points which can be seen from the time on: those after it, and the one before
// which holds into the window. Only these are drawn, however much history has been loaded.
func visible(s Series, from time.Time) Series {
	i := sort.Search(len(s), func(i int) bool { return s[i].At.After(from) })

	return s[max(i-1, 0):]
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

// legend lists each device with its colour and latest readings; a device which has not reported for
// staleAfter is greyed out and shows how long ago it did.
func (s *Store) legend(now time.Time) string {
	parts := make([]string, 0, len(s.Order))

	for i, device := range s.Order {
		text := device

		if p, ok := s.Series[Temperature][device].Last(); ok {
			text += fmt.Sprintf(" %.1f°C", p.Value)
		}

		if p, ok := s.Series[Humidity][device].Last(); ok {
			text += fmt.Sprintf(" %.0f%%", p.Value)
		}

		style := lipgloss.NewStyle().Foreground(color(i))

		if age := now.Sub(s.lastSeen(device)); age >= staleAfter {
			text += fmt.Sprintf(" (%s ago)", formatWindow(age))
			style = lipgloss.NewStyle().Foreground(staleColor)
		}

		parts = append(parts, style.Render("● "+text))
	}

	return strings.Join(parts, "  ")
}

// batteries lists the battery level of each device which reports one.
func (s *Store) batteries() []string {
	var lines []string

	width := 0
	for device := range s.Series[Battery] {
		width = max(width, lipgloss.Width(device))
	}

	for i, device := range s.Order {
		p, ok := s.Series[Battery][device].Last()
		if !ok {
			continue
		}

		name := lipgloss.NewStyle().Foreground(color(i)).Width(width).Render(device)
		lines = append(lines, fmt.Sprintf("%s %s %3.0f%%", name, batteryBar(p.Value), p.Value))
	}

	return lines
}

// boilerRows draws the boiler state aligned under a chart, with a key.
func (s *Store) boilerRows(c chart, from, to time.Time) []string {
	strip := Strip(s.Series[Boiler][s.Receiver.Name], from, to, c.width)

	key := fmt.Sprintf("Boiler  %s on  %s idle  %s off", boilerOn, boilerIdle, boilerOff)

	return []string{key, strings.Repeat(" ", c.left) + renderStrip(strip)}
}
