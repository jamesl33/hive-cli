package dashboard

import (
	"strings"
	"testing"
	"time"

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
	next, _ := m.Update(tea.WindowSizeMsg{Width: minWidth, Height: minHeight})

	view := next.View()
	require.NotEmpty(t, view)

	assert.LessOrEqual(t, strings.Count(view, "\n")+1, minHeight)
	assert.Contains(t, view, "Usage")
}
