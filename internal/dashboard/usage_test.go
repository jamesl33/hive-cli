package dashboard

import (
	"strings"
	"testing"
	"time"

	"github.com/NimbleMarkets/ntcharts/canvas"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreKeepsUsageOutOfDevices(t *testing.T) {
	s := NewStore(receiver)
	s.add(Sample{Device: "gas", Metric: Gas, At: time.Now(), Value: 0.5})

	assert.Empty(t, s.Order)
	assert.Len(t, s.Series[Gas]["gas"], 1)
	assert.True(t, s.hasUsage())
}

func TestUsageEndAndCorner(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

	s := NewStore(receiver)
	assert.True(t, s.usageEnd().IsZero())

	s.add(Sample{Device: "gas", Metric: Gas, At: now.Add(-25 * time.Hour), Value: 0.5})

	assert.Equal(t, now.Add(-25*time.Hour+usageStep), s.usageEnd())
	assert.Contains(t, s.usageCorner(now), "24h behind")
}

func TestFuelsSumsTheWindow(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

	s := NewStore(receiver)
	s.add(Sample{Device: "gas", Metric: Gas, At: now.Add(-3 * time.Hour), Value: 9})
	s.add(Sample{Device: "gas", Metric: Gas, At: now.Add(-2 * usageStep), Value: 0.5})
	s.add(Sample{Device: "gas", Metric: Gas, At: now.Add(-usageStep), Value: 0.5})

	assert.Contains(t, s.fuels(now.Add(-90*time.Minute), nil), "gas 1.0 kWh")
	assert.Contains(t, s.fuels(now.Add(-4*time.Hour), nil), "gas 10.0 kWh")
	// A reading which started before the window but ends inside it is drawn, so it is counted.
	assert.Contains(t, s.fuels(now.Add(-50*time.Minute), nil), "gas 1.0 kWh")
}

func TestUsageOutsideWindowIsBlank(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

	s := NewStore(receiver)
	s.add(Sample{Device: "gas", Metric: Gas, At: now.Add(-25 * time.Hour), Value: 0.5})

	empty := NewStore(receiver)
	empty.add(Sample{Device: "gas", Metric: Gas, At: now.Add(-25 * time.Hour), Value: 0})

	from := now.Add(-time.Hour)

	assert.Equal(t, empty.renderUsage(98, 6, from, now, nil), s.renderUsage(98, 6, from, now, nil))
	assert.Empty(t, current(s.Series[Gas]["gas"], from))
}

func TestFuelKeysHideFuels(t *testing.T) {
	store := NewStore(receiver)
	now := time.Now()

	store.add(Sample{Device: "hall", Metric: Temperature, At: now.Add(-time.Hour), Value: 18})
	store.add(Sample{Device: "gas", Metric: Gas, At: now.Add(-30 * time.Hour), Value: 0.5})
	store.add(Sample{Device: "electricity", Metric: Electricity, At: now.Add(-30 * time.Hour), Value: 0.2})

	m := New(t.Context(), feed{}, store)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})

	hidden := next.(Model).hidden
	assert.True(t, hidden["gas"])
	assert.False(t, hidden["electricity"])
	// The chart scales to the fuel which is left: with gas hidden it peaks at electricity's 0.2.
	assert.Contains(t, m.store.renderUsage(98, 6, now.Add(-48*time.Hour), now, hidden), "0.2")

	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	assert.False(t, next.(Model).hidden["gas"])
}

func TestViewShowsUsage(t *testing.T) {
	store := NewStore(receiver)
	now := time.Now()

	store.add(Sample{Device: "hall", Metric: Temperature, At: now.Add(-time.Hour), Value: 18})

	for i := range 48 {
		at := now.Add(-48*time.Hour + time.Duration(i)*usageStep)
		store.add(Sample{Device: "gas", Metric: Gas, At: at, Value: float64(i%6) / 4})
	}

	m := New(t.Context(), feed{}, store)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	view := next.View()
	t.Log("\n" + view)

	assert.LessOrEqual(t, strings.Count(view, "\n")+1, 40)

	for _, want := range []string{"Usage kWh/30m", "behind", "hall", "Temperature"} {
		assert.Contains(t, view, want)
	}
}

func TestViewPutsHumidityBesideTemperature(t *testing.T) {
	store := NewStore(receiver)
	now := time.Now()

	store.add(Sample{Device: "hall", Metric: Temperature, At: now.Add(-time.Hour), Value: 18})
	store.add(Sample{Device: "hall", Metric: Humidity, At: now.Add(-time.Hour), Value: 55})
	store.add(Sample{Device: "gas", Metric: Gas, At: now.Add(-30 * time.Hour), Value: 0.5})

	m := New(t.Context(), feed{}, store)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	view := next.View()
	t.Log("\n" + view)

	assert.LessOrEqual(t, strings.Count(view, "\n")+1, 40)

	var shared string

	for line := range strings.SplitSeq(view, "\n") {
		if strings.Contains(line, "Humidity") {
			shared = line
		}
	}

	assert.Contains(t, shared, "Temperature")
	assert.Equal(t, 100, lipgloss.Width(shared))
	assert.Greater(t, strings.Index(view, "Usage kWh/30m"), strings.Index(view, "Humidity"))
}

func TestViewFitsUsageAtMinHeight(t *testing.T) {
	store := NewStore(receiver)
	now := time.Now()

	store.add(Sample{Device: "hall", Metric: Temperature, At: now, Value: 18})
	store.add(Sample{Device: "hall", Metric: Humidity, At: now, Value: 55})
	store.add(Sample{Device: "gas", Metric: Gas, At: now.Add(-30 * time.Hour), Value: 0.5})

	m := New(t.Context(), feed{}, store)
	// The fuels box takes three rows, so the usage chart needs that many more than the minimum
	height := minHeight + 3
	next, _ := m.Update(tea.WindowSizeMsg{Width: minWidth, Height: height})

	view := next.View()
	require.NotEmpty(t, view)

	assert.LessOrEqual(t, strings.Count(view, "\n")+1, height)
	assert.Contains(t, view, "Fuels")
	assert.Contains(t, view, "Usage")
}

func TestDrawPlotsAlternatesOverlappingLines(t *testing.T) {
	from := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)

	fresh := func() timeserieslinechart.Model {
		return timeserieslinechart.New(40, 8,
			timeserieslinechart.WithTimeRange(from, to),
			timeserieslinechart.WithYRange(0, 10),
		)
	}

	points := Series{{At: from, Value: 5}, {At: to, Value: 5}}
	red, blue := lipgloss.Color("1"), lipgloss.Color("4")

	c := drawPlots(fresh, []plot{
		{style: lipgloss.NewStyle().Foreground(red), points: points},
		{style: lipgloss.NewStyle().Foreground(blue), points: points},
	})

	colors := map[lipgloss.TerminalColor]bool{}

	for y := range c.GraphHeight() {
		for x := range c.GraphWidth() {
			cell := c.Canvas.Cell(canvas.Point{X: c.Origin().X + 1 + x, Y: y})
			if cell.Rune > brailleBlank && cell.Rune <= brailleFull {
				colors[cell.Style.GetForeground()] = true
			}
		}
	}

	assert.Len(t, colors, 2)
}
