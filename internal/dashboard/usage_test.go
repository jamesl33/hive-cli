package dashboard

import (
	"strings"
	"testing"
	"time"

	"github.com/NimbleMarkets/ntcharts/canvas"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreKeepsUsageOutOfDevices(t *testing.T) {
	s := NewStore(receiver)
	s.add(history.Sample{Device: "gas", Metric: history.Gas, At: time.Now(), Value: 0.5})

	assert.Empty(t, s.Order)
	assert.Len(t, s.Series[history.Gas]["gas"], 1)
	assert.True(t, s.hasUsage())
}

func TestUsageEndAndCorner(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

	s := NewStore(receiver)
	assert.True(t, s.usageEnd().IsZero())

	s.add(history.Sample{Device: "gas", Metric: history.Gas, At: now.Add(-25 * time.Hour), Value: 0.5})

	assert.Equal(t, now.Add(-25*time.Hour+usageStep), s.usageEnd())
	assert.Contains(t, s.usageCorner(now), "24h behind")
}

func TestFuelsSumsTheWindow(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

	s := NewStore(receiver)
	s.add(history.Sample{Device: "gas", Metric: history.Gas, At: now.Add(-3 * time.Hour), Value: 9})
	s.add(history.Sample{Device: "gas", Metric: history.Gas, At: now.Add(-2 * usageStep), Value: 0.5})
	s.add(history.Sample{Device: "gas", Metric: history.Gas, At: now.Add(-usageStep), Value: 0.5})

	assert.Contains(t, s.fuels(now.Add(-90*time.Minute), nil), "gas 1.0 kWh")
	assert.Contains(t, s.fuels(now.Add(-4*time.Hour), nil), "gas 10.0 kWh")
	// A reading which started before the window but ends inside it is drawn, so it is counted.
	assert.Contains(t, s.fuels(now.Add(-50*time.Minute), nil), "gas 1.0 kWh")
}

func TestUsageOutsideWindowIsBlank(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

	s := NewStore(receiver)
	s.add(history.Sample{Device: "gas", Metric: history.Gas, At: now.Add(-25 * time.Hour), Value: 0.5})

	empty := NewStore(receiver)
	empty.add(history.Sample{Device: "gas", Metric: history.Gas, At: now.Add(-25 * time.Hour), Value: 0})

	from := now.Add(-time.Hour)

	assert.Equal(t, empty.renderUsage(98, 6, from, now, nil), s.renderUsage(98, 6, from, now, nil))
	assert.Empty(t, current(s.Series[history.Gas]["gas"], from))
}

func TestFuelKeysHideFuels(t *testing.T) {
	store := NewStore(receiver)
	now := time.Now()

	store.add(history.Sample{Device: "hall", Metric: history.Temperature, At: now.Add(-time.Hour), Value: 18})
	store.add(history.Sample{Device: "gas", Metric: history.Gas, At: now.Add(-30 * time.Hour), Value: 0.5})
	store.add(history.Sample{Device: "electricity", Metric: history.Electricity, At: now.Add(-30 * time.Hour), Value: 0.2})

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

	store.add(history.Sample{Device: "hall", Metric: history.Temperature, At: now.Add(-time.Hour), Value: 18})

	for i := range 48 {
		at := now.Add(-48*time.Hour + time.Duration(i)*usageStep)
		store.add(history.Sample{Device: "gas", Metric: history.Gas, At: at, Value: float64(i%6) / 4})
	}

	m := New(t.Context(), feed{}, store)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyShiftTab})

	view := next.View()
	t.Log("\n" + view)

	assert.LessOrEqual(t, strings.Count(view, "\n")+1, 40)

	for _, want := range []string{"Usage kWh/30m", "behind"} {
		assert.Contains(t, view, want)
	}

	assert.NotContains(t, view, "Temperature")
	assert.NotContains(t, view, "hall")
}

func TestTabCyclesPages(t *testing.T) {
	m := New(t.Context(), feed{}, NewStore(receiver))

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, pageValves, next.(Model).page)

	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, pageFuels, next.(Model).page)

	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, pageClimate, next.(Model).page)

	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, pageFuels, next.(Model).page)
}

func TestValvesPageDrawsTheValveCharts(t *testing.T) {
	store := NewStore(receiver)
	now := time.Now()

	store.Apply(update(t, "hall", now, `{"temperature":18.2}`))
	store.Apply(update(t, "trv_bedroom", now, `{"local_temperature":21.5,"occupied_heating_setpoint":30,"heating_valve_position":100,"idle_valve_position":100,"running_state":"idle"}`))

	m := New(t.Context(), feed{}, store)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyTab})

	view := next.View()
	t.Log("\n" + view)

	assert.LessOrEqual(t, strings.Count(view, "\n")+1, 40)

	for _, want := range []string{"Temperature °C", "Setpoint °C", "Opening %", "bedroom"} {
		assert.Contains(t, view, want)
	}

	assert.NotContains(t, view, "hall")
	assert.Contains(t, view, "100│")
	assert.Contains(t, view, "  0└")
}

func TestValveOpeningLabelsFitAtAnyHeight(t *testing.T) {
	store := NewStore(receiver)
	now := time.Now()

	store.Apply(update(t, "trv_bedroom", now, `{"local_temperature":21.5,"occupied_heating_setpoint":30,"heating_valve_position":100,"idle_valve_position":100,"running_state":"idle"}`))

	for h := 3; h <= 12; h++ {
		chart := store.renderChart(history.Valve, nil, 60, h, now.Add(-time.Hour), now)
		assert.Contains(t, chart.view, "100│", "height %d", h)
	}
}

func TestViewPutsHumidityBelowTemperature(t *testing.T) {
	store := NewStore(receiver)
	now := time.Now()

	store.add(history.Sample{Device: "hall", Metric: history.Temperature, At: now.Add(-time.Hour), Value: 18})
	store.add(history.Sample{Device: "hall", Metric: history.Humidity, At: now.Add(-time.Hour), Value: 55})
	store.add(history.Sample{Device: "gas", Metric: history.Gas, At: now.Add(-30 * time.Hour), Value: 0.5})

	m := New(t.Context(), feed{}, store)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	view := next.View()
	t.Log("\n" + view)

	assert.LessOrEqual(t, strings.Count(view, "\n")+1, 40)

	assert.Greater(t, strings.Index(view, "Humidity"), strings.Index(view, "Temperature"))
	for line := range strings.SplitSeq(view, "\n") {
		assert.Equal(t, 100, lipgloss.Width(line))
		assert.False(t, strings.Contains(line, "Humidity") && strings.Contains(line, "Temperature"))
	}

	assert.Greater(t, strings.Index(view, "Boiler"), strings.Index(view, "Humidity"))
	assert.NotContains(t, view, "Usage")
}

func TestViewFitsUsageAtMinHeight(t *testing.T) {
	store := NewStore(receiver)
	now := time.Now()

	store.add(history.Sample{Device: "hall", Metric: history.Temperature, At: now, Value: 18})
	store.add(history.Sample{Device: "hall", Metric: history.Humidity, At: now, Value: 55})
	store.add(history.Sample{Device: "gas", Metric: history.Gas, At: now.Add(-30 * time.Hour), Value: 0.5})

	m := New(t.Context(), feed{}, store)
	next, _ := m.Update(tea.WindowSizeMsg{Width: minWidth, Height: minHeight})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	height := minHeight

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

func TestClimateFitsMinSizeWithBatteries(t *testing.T) {
	store := NewStore(receiver)
	now := time.Now()

	for _, device := range []string{"a", "b", "c", "d", "e", "f"} {
		store.add(history.Sample{Device: device, Metric: history.Temperature, At: now, Value: 18})
		store.add(history.Sample{Device: device, Metric: history.Humidity, At: now, Value: 55})
		store.add(history.Sample{Device: device, Metric: history.Battery, At: now, Value: 10})
	}

	m := New(t.Context(), feed{}, store)
	m.connected, m.paused = false, true

	next, _ := m.Update(tea.WindowSizeMsg{Width: minWidth, Height: minHeight})

	view := next.View()
	t.Log("\n" + view)

	assert.LessOrEqual(t, strings.Count(view, "\n")+1, minHeight)

	for line := range strings.SplitSeq(view, "\n") {
		assert.Equal(t, minWidth, lipgloss.Width(line), line)
	}
}

func TestFuelsWithoutUsage(t *testing.T) {
	store := NewStore(receiver)
	store.add(history.Sample{Device: "hall", Metric: history.Temperature, At: time.Now(), Value: 18})

	next, _ := New(t.Context(), feed{}, store).Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyShiftTab})

	assert.Contains(t, next.View(), "No energy use recorded.")
}

func TestTabRedrawsCachedView(t *testing.T) {
	store := NewStore(receiver)
	store.add(history.Sample{Device: "hall", Metric: history.Temperature, At: time.Now(), Value: 18})

	next, _ := New(t.Context(), feed{}, store).Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	assert.Contains(t, next.View(), "Temperature")

	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.NotContains(t, next.View(), "Temperature")
}
