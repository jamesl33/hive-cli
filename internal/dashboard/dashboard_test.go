package dashboard

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var receiver = hive.Receiver{Name: "hive", Model: "SLR1c"}

func update(t *testing.T, device string, at time.Time, js string) z2m.Update {
	t.Helper()

	payload := make(map[string]any)
	require.NoError(t, json.Unmarshal([]byte(js), &payload))

	return z2m.Update{Device: device, At: at, Payload: payload}
}

func TestSeriesAppendDropsOld(t *testing.T) {
	start := time.Unix(0, 0)

	var s Series

	s = s.Append(Point{At: start, Value: 1})
	s = s.Append(Point{At: start.Add(time.Hour), Value: 2})
	s = s.Append(Point{At: start.Add(maxAge + 2*time.Hour), Value: 3})

	// The newest point which is too old is kept, as it is the value held from the start of the period
	require.Len(t, s, 2)
	assert.Equal(t, 2.0, s[0].Value)
	assert.Equal(t, 3.0, s[1].Value)
}

func TestSeriesAppendPanicsOverTheLimit(t *testing.T) {
	var s Series

	for i := range maxPoints {
		s = s.Append(Point{At: time.Unix(int64(i), 0), Value: float64(i)})
	}

	assert.Len(t, s, maxPoints)
	assert.Panics(t, func() { s.Append(Point{At: time.Unix(maxPoints, 0)}) })
}

func TestExtractSensor(t *testing.T) {
	u := update(t, "hall", time.Unix(1, 0), `{"temperature":19.5,"humidity":48,"battery":87,"linkquality":120}`)

	got := make(map[history.Metric]float64)
	for _, s := range Extract(receiver, u, u.Payload) {
		got[s.Metric] = s.Value
	}

	assert.Equal(t, map[history.Metric]float64{history.Temperature: 19.5, history.Humidity: 48, history.Battery: 87}, got)
}

func TestExtractReceiverBoiler(t *testing.T) {
	tests := map[string]float64{
		`{"system_mode":"heat","running_state":"heat"}`: history.BoilerOn,
		`{"system_mode":"heat","running_state":"idle"}`: history.BoilerIdle,
		`{"system_mode":"off","running_state":"idle"}`:  history.BoilerOff,
	}

	for js, want := range tests {
		u := update(t, "hive", time.Unix(1, 0), js)

		samples := Extract(receiver, u, u.Payload)
		require.Len(t, samples, 1, js)
		assert.Equal(t, history.Boiler, samples[0].Metric, js)
		assert.Equal(t, want, samples[0].Value, js)
	}
}

func TestStoreMergesReceiverState(t *testing.T) {
	s := NewStore(receiver)
	at := time.Unix(1, 0)

	s.Apply(update(t, "hive", at, `{"system_mode":"heat"}`))
	s.Apply(update(t, "hive", at.Add(time.Second), `{"running_state":"heat","local_temperature":18.5}`))

	boiler := s.Series[history.Boiler]["hive"]
	require.Len(t, boiler, 2)
	assert.Equal(t, history.BoilerIdle, boiler[0].Value)
	assert.Equal(t, history.BoilerOn, boiler[1].Value)

	temp, ok := s.Series[history.Temperature]["hive"].Last()
	require.True(t, ok)
	assert.Equal(t, 18.5, temp.Value)
}

func TestStoreOrdersDevicesByName(t *testing.T) {
	s := NewStore(receiver)

	for _, name := range []string{"office", "bedroom", "office", "hall"} {
		s.Apply(update(t, name, time.Unix(1, 0), `{"temperature":20}`))
	}

	assert.Equal(t, []string{"bedroom", "hall", "office"}, s.Order)
}

func TestStoreIgnoresUnrelatedReport(t *testing.T) {
	s := NewStore(receiver)
	s.Apply(update(t, "hive", time.Unix(1, 0), `{"linkquality":90}`))

	assert.Empty(t, s.Order)
	assert.Empty(t, s.Series)
}

func TestLegendMarksStaleDevices(t *testing.T) {
	s := NewStore(receiver)
	now := time.Unix(100000, 0)

	s.Apply(update(t, "hall", now.Add(-3*time.Hour), `{"temperature":18.2}`))
	s.Apply(update(t, "office", now.Add(-5*time.Minute), `{"temperature":20.1}`))
	s.Apply(update(t, "office", now.Add(-4*time.Hour), `{"battery":90}`))

	legend := s.legend(now, nil, groupNone)

	assert.Contains(t, legend, "hall 18.2°C (3h ago)")
	assert.Contains(t, legend, "office 20.1°C")
	assert.NotContains(t, legend, "office 20.1°C (")
}

func TestLegendGroupsDevices(t *testing.T) {
	s := NewStore(receiver)
	now := time.Unix(100000, 0)

	s.Apply(update(t, "trv_bedroom", now, `{"local_temperature":21.5,"occupied_heating_setpoint":30,"heating_valve_position":100,"idle_valve_position":100,"running_state":"idle"}`))
	s.Apply(update(t, "bedroom", now, `{"temperature":17.2}`))
	s.Apply(update(t, "aaa", now, `{"temperature":18}`))

	assert.Equal(t, []string{"aaa", "bedroom", "trv_bedroom"}, s.Order)

	lines := strings.Split(s.legend(now, nil, groupNone), "\n")

	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "Sensors")
	assert.Contains(t, lines[1], "TRVs")
	assert.Contains(t, lines[1], "a ● bedroom 21.5°C 30.0°C 100%")
}

func TestLegendListsThermostatWithReceiver(t *testing.T) {
	s := NewStore(receiver)
	s.Thermostat = "thermostat"
	now := time.Unix(100000, 0)

	s.Apply(update(t, "thermostat", now, `{"battery":85}`))
	s.Apply(update(t, "hive", now, `{"local_temperature":16.1}`))
	s.Apply(update(t, "lounge", now, `{"temperature":17}`))

	lines := strings.Split(s.legend(now, nil, groupNone), "\n")

	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "hive")
	assert.Contains(t, lines[0], "thermostat")
	assert.Contains(t, lines[1], "lounge")
}

func TestPickingDeviceByGroupThenLetter(t *testing.T) {
	m := New(t.Context(), feed{}, NewStore(receiver))
	m.store.Thermostat = "thermostat"
	now := time.Unix(1, 0)

	for i := range 16 {
		m.store.Apply(update(t, fmt.Sprintf("dev%02d", i), now, `{"temperature":20}`))
	}

	m.store.Apply(update(t, "hive", now, `{"local_temperature":16}`))

	press := func(m Model, k string) Model {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})

		return updated.(Model)
	}

	// Group 2 is the sensors; "o", which is the outdoor sensor key, is the 15th letter there
	m = press(press(m, "2"), "l")
	assert.True(t, m.hidden["dev11"])
	assert.False(t, m.picking)

	m = press(press(m, "2"), "o")
	assert.True(t, m.hidden["dev14"])

	// A key after a number is a letter even when it does something else
	m = press(press(m, "2"), "q")
	assert.False(t, m.picking)

	// Escape cancels picking without quitting
	m = press(m, "2")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, updated.(Model).picking)
	assert.Nil(t, cmd)

	// A group with no devices is not picked
	assert.False(t, press(m, "3").picking)
}

func TestStrip(t *testing.T) {
	from := time.Unix(0, 0)
	s := Series{
		{At: from.Add(15 * time.Second), Value: history.BoilerOn},
		{At: from.Add(35 * time.Second), Value: history.BoilerIdle},
	}

	assert.Equal(t, []float64{-1, history.BoilerOn, history.BoilerOn, history.BoilerIdle}, Strip(s, from, from.Add(40*time.Second), 4))
}

func TestBatteryColor(t *testing.T) {
	assert.Equal(t, "196", string(BatteryColor(5)))
	assert.Equal(t, "214", string(BatteryColor(30)))
	assert.Equal(t, "42", string(BatteryColor(80)))
}

// feed is a Source which has nothing to report.
type feed struct{}

func (feed) Next(ctx context.Context) (z2m.Update, error) {
	<-ctx.Done()
	return z2m.Update{}, ctx.Err()
}

func (feed) Connected() bool { return true }

func TestViewShowsEverything(t *testing.T) {
	store := NewStore(receiver)
	now := time.Now()

	for i := range 30 {
		at := now.Add(time.Duration(i-30) * time.Minute)
		store.Apply(update(t, "hive", at, `{"system_mode":"heat","running_state":"heat","local_temperature":19.5}`))
		store.Apply(update(t, "hall", at, `{"temperature":18.2,"humidity":55,"battery":15}`))
	}

	m := New(t.Context(), feed{}, store)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	view := next.View()
	t.Log("\n" + view)

	assert.LessOrEqual(t, strings.Count(view, "\n")+1, 40)

	for _, want := range []string{"hive", "hall", "Temperature", "Humidity", "Battery", "15%", "19.5°C", "55%"} {
		assert.Contains(t, view, want)
	}
}

func TestViewIsCached(t *testing.T) {
	store := NewStore(receiver)
	store.Apply(update(t, "hall", time.Now(), `{"temperature":18.2}`))

	m := New(t.Context(), feed{}, store)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = next.(Model)

	first := m.View()
	*m.cache = viewCache{key: m.viewKey(time.Now()), view: "cached", ok: true}

	assert.Equal(t, "cached", m.View())

	store.Apply(update(t, "hall", time.Now(), `{"temperature":19.2}`))
	assert.NotEqual(t, "cached", m.View())
	assert.NotEqual(t, first, m.View())
}

func TestViewWaitsForData(t *testing.T) {
	m := New(t.Context(), feed{}, NewStore(receiver))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	assert.Contains(t, next.View(), "Waiting for devices")
}

func TestSpanned(t *testing.T) {
	to := time.Unix(100, 0)
	s := Series{{At: time.Unix(20, 0), Value: 1}, {At: time.Unix(60, 0), Value: 2}}

	got := spanned(s, to)
	require.Len(t, got, 3)
	assert.Equal(t, Point{At: to, Value: 2}, got[2])
	assert.Nil(t, spanned(nil, to))
}

func TestStepped(t *testing.T) {
	s := Series{{At: time.Unix(10, 0), Value: 1}, {At: time.Unix(60, 0), Value: 2}, {At: time.Unix(90, 0), Value: 2}}

	want := Series{
		{At: time.Unix(10, 0), Value: 1},
		{At: time.Unix(60, 0), Value: 1},
		{At: time.Unix(60, 0), Value: 2},
		{At: time.Unix(90, 0), Value: 2},
	}

	assert.Equal(t, want, stepped(s))
	assert.Nil(t, stepped(nil))
}

func TestWindowCycles(t *testing.T) {
	m := New(t.Context(), feed{}, NewStore(receiver))
	start := m.window

	for range windows {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
		m = next.(Model)
	}

	assert.Equal(t, start, m.window)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'W'}})
	assert.Equal(t, start-1, next.(Model).window)

	m.window = 0
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'W'}})
	assert.Equal(t, len(windows)-1, next.(Model).window)

	assert.Equal(t, "15m", formatWindow(15*time.Minute))
	assert.Equal(t, "3h", formatWindow(3*time.Hour))
	assert.Equal(t, "24h", formatWindow(24*time.Hour))
	assert.Equal(t, "30d", formatWindow(30*24*time.Hour))
	assert.Equal(t, 48*time.Hour, tickStep(7*24*time.Hour))
	assert.Equal(t, 7*24*time.Hour, tickStep(30*24*time.Hour))
	assert.Equal(t, "6mon", formatWindow(6*30*24*time.Hour))
	assert.Equal(t, "12mon", formatWindow(12*30*24*time.Hour))
	assert.Equal(t, 30*24*time.Hour, tickStep(6*30*24*time.Hour))
	assert.Equal(t, 90*24*time.Hour, tickStep(12*30*24*time.Hour))
}

func TestViewWideWindow(t *testing.T) {
	store := NewStore(receiver)
	now := time.Now()

	for i := range 10 {
		store.Apply(update(t, "hall", now.Add(time.Duration(i-10)*time.Minute), `{"temperature":18.2}`))
	}

	m := New(t.Context(), feed{}, store)
	m.window = len(windows) - 1
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	t.Log("\n" + next.View())
}

func TestStoreRecordsTarget(t *testing.T) {
	s := NewStore(receiver)
	s.Apply(update(t, "hive", time.Unix(1, 0), `{"system_mode":"heat","occupied_heating_setpoint":20.5}`))

	target, ok := s.Series[history.Target]["hive"].Last()
	require.True(t, ok)
	assert.Equal(t, 20.5, target.Value)
	assert.Contains(t, s.legend(time.Unix(2, 0), nil, groupNone), "(20.5°C")
}

func TestBoxClipsAndPads(t *testing.T) {
	got := box("T", "", "short\n"+strings.Repeat("x", 30), 12)

	for line := range strings.SplitSeq(got, "\n") {
		assert.Equal(t, 12, lipgloss.Width(line), line)
	}
}

func TestDownsample(t *testing.T) {
	from, to := time.Unix(0, 0), time.Unix(100, 0)

	series := func(points ...[2]float64) Series {
		var s Series
		for _, p := range points {
			s = append(s, Point{At: time.Unix(int64(p[0]), 0), Value: p[1]})
		}

		return s
	}

	tests := []struct {
		name    string
		in      Series
		from    time.Time
		to      time.Time
		buckets int
		want    Series
	}{
		{
			name:    "empty",
			buckets: 2,
		},
		{
			name:    "fits in the buckets",
			in:      series([2]float64{10, 1}, [2]float64{20, 2}, [2]float64{60, 3}, [2]float64{70, 4}),
			buckets: 2,
			want:    series([2]float64{10, 1}, [2]float64{20, 2}, [2]float64{60, 3}, [2]float64{70, 4}),
		},
		{
			name:    "no span",
			in:      series([2]float64{10, 1}, [2]float64{20, 2}, [2]float64{30, 3}, [2]float64{40, 4}, [2]float64{50, 5}),
			from:    from,
			to:      from,
			buckets: 1,
			want:    series([2]float64{10, 1}, [2]float64{20, 2}, [2]float64{30, 3}, [2]float64{40, 4}, [2]float64{50, 5}),
		},
		{
			name: "keeps the extremes of each bucket in time order, and the ends",
			in: series(
				[2]float64{-10, 5}, // Held into the window
				[2]float64{10, 3}, [2]float64{20, 9}, [2]float64{30, 1}, [2]float64{40, 4},
				[2]float64{60, 7}, [2]float64{70, 2}, [2]float64{80, 8}, [2]float64{90, 6},
			),
			buckets: 2,
			want: series(
				[2]float64{-10, 5},
				[2]float64{20, 9}, [2]float64{30, 1},
				[2]float64{70, 2}, [2]float64{80, 8},
				[2]float64{90, 6},
			),
		},
		{
			name: "does not repeat the last point when it is an extreme",
			in: series(
				[2]float64{10, 3}, [2]float64{20, 4}, [2]float64{30, 5},
				[2]float64{60, 7}, [2]float64{70, 2}, [2]float64{90, 9},
			),
			buckets: 2,
			want: series(
				[2]float64{10, 3},
				[2]float64{20, 4}, [2]float64{30, 5},
				[2]float64{70, 2}, [2]float64{90, 9},
			),
		},
		{
			name: "equal values give one point per bucket, and the last is kept",
			in: series(
				[2]float64{10, 3}, [2]float64{20, 3}, [2]float64{30, 3},
				[2]float64{60, 7}, [2]float64{70, 7},
			),
			buckets: 2,
			want: series(
				[2]float64{10, 3},
				[2]float64{20, 3},
				[2]float64{60, 7},
				[2]float64{70, 7},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := tt.from, tt.to
			if end.IsZero() {
				start, end = from, to
			}

			assert.Equal(t, tt.want, downsample(tt.in, start, end, tt.buckets))
		})
	}
}

func TestClampMovesLeadingPointOntoStart(t *testing.T) {
	from := time.Unix(10, 0)
	s := Series{{At: time.Unix(0, 0), Value: 0}, {At: time.Unix(20, 0), Value: 20}, {At: time.Unix(30, 0), Value: 30}}

	got := clamp(s, from)

	assert.Equal(t, Point{At: from, Value: 10}, got[0])
	assert.Equal(t, time.Unix(0, 0), s[0].At)
	assert.Equal(t, s[1:], got[1:])
}

func TestClampLoneOldPointOnceSpanned(t *testing.T) {
	from, to := time.Unix(100, 0), time.Unix(200, 0)
	s := Series{{At: time.Unix(10, 0), Value: 5}}

	got := clamp(spanned(s, to), from)

	assert.Equal(t, Series{{At: from, Value: 5}, {At: to, Value: 5}}, got)
}

func TestBoilerOnTime(t *testing.T) {
	from, to := time.Unix(100, 0), time.Unix(200, 0)
	s := Series{
		{At: time.Unix(80, 0), Value: history.BoilerOn},
		{At: time.Unix(120, 0), Value: history.BoilerOff},
		{At: time.Unix(150, 0), Value: history.BoilerOn},
	}

	assert.Equal(t, 20*time.Second+50*time.Second, boilerOnTime(s, from, to))
	assert.Zero(t, boilerOnTime(nil, from, to))
}

func TestFormatOn(t *testing.T) {
	assert.Equal(t, "3h12m on (13%)", formatOn(3*time.Hour+12*time.Minute+30*time.Second, 24*time.Hour))
}

func TestPauseFreezesView(t *testing.T) {
	store := NewStore(receiver)
	store.Apply(update(t, "hall", time.Now(), `{"temperature":18.2}`))

	m := New(t.Context(), feed{}, store)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(Model)

	paused := m.View()
	assert.Contains(t, paused, "paused")

	store.Apply(update(t, "hall", time.Now(), `{"temperature":19.2}`))
	assert.Equal(t, paused, m.View())

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	assert.NotContains(t, next.View(), "paused")
}

func TestHideOutdoors(t *testing.T) {
	store := NewStore(receiver)
	store.Outdoors = "garden"
	store.Apply(update(t, "garden", time.Now(), `{"temperature":2.2}`))
	store.Apply(update(t, "hall", time.Now(), `{"temperature":18.2}`))

	now := time.Now()
	shown := store.renderChart(history.Temperature, nil, 60, 10, now.Add(-time.Hour), now)
	hidden := store.renderChart(history.Temperature, map[string]bool{store.Outdoors: true}, 60, 10, now.Add(-time.Hour), now)

	assert.NotEqual(t, shown.view, hidden.view)
	assert.Len(t, store.Series[history.Temperature], 2)
}

func TestBoxCorner(t *testing.T) {
	top, _, _ := strings.Cut(box("Devices", "24h", "x", 30), "\n")
	assert.Equal(t, "┌─ Devices ──────────── 24h ─┐", top)

	top, _, _ = strings.Cut(box("Devices", "24h", "x", 12), "\n")
	assert.Equal(t, "┌─ Devices ┐", top)
}

func TestTickLabels(t *testing.T) {
	at := func(h, m, sec int) float64 {
		return float64(time.Date(2026, 10, 23, h, m, sec, 0, time.Local).Unix())
	}

	label := tickLabels(time.Hour, time.Minute)

	assert.Equal(t, "11:00", label(0, at(10, 59, 30)))
	assert.Empty(t, label(0, at(10, 30, 0)))
	assert.Equal(t, time.Hour, tickStep(6*time.Hour))
	assert.Equal(t, 4*time.Hour, tickStep(24*time.Hour))
}

func TestBatteriesHiddenUntilLow(t *testing.T) {
	store := NewStore(receiver)
	store.Apply(update(t, "hall", time.Now(), `{"temperature":19.5,"battery":80}`))

	assert.Nil(t, store.batteries())
	assert.Equal(t, ", +1.5", store.gap("hall", 18))
	assert.Empty(t, store.gap("garage", 18))

	store.Apply(update(t, "hall", time.Now(), `{"battery":15}`))
	assert.Len(t, store.batteries(), 1)
}

func TestToggleDevice(t *testing.T) {
	store := NewStore(receiver)
	store.Apply(update(t, "hall", time.Now(), `{"temperature":18.2}`))

	m := New(t.Context(), feed{}, store)

	// Group 2 is the sensors, and "a" is the first device in it
	for _, hidden := range []bool{true, false} {
		for _, k := range []rune{'2', 'a'} {
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
			m = next.(Model)
		}

		assert.Equal(t, hidden, m.hidden["hall"])
	}
}

func TestOutdoorsStartHidden(t *testing.T) {
	store := NewStore(receiver)
	store.Outdoors = "garden"

	assert.True(t, New(t.Context(), feed{}, store).hidden["garden"])
	assert.Empty(t, New(t.Context(), feed{}, NewStore(receiver)).hidden)
}

func TestReportsValve(t *testing.T) {
	r := NewReports(receiver)

	got := func(js string) map[history.Metric]float64 {
		samples := make(map[history.Metric]float64)
		for _, s := range r.Samples(update(t, "office-trv", time.Unix(1, 0), js)) {
			samples[s.Metric] = s.Value
		}

		return samples
	}

	full := `{"local_temperature":21.5,"occupied_heating_setpoint":16,"heating_valve_position":80,"idle_valve_position":10,"running_state":"idle","battery":90}`

	assert.Equal(t, map[history.Metric]float64{
		history.Temperature: 21.5, history.Setpoint: 16, history.Valve: 10, history.Battery: 90,
	}, got(full))

	// The opening follows the running state, using the positions from earlier reports
	assert.Equal(t, map[history.Metric]float64{history.Valve: 80}, got(`{"running_state":"heat"}`))
	assert.Empty(t, got(`{"linkquality":40}`))
}

func TestReportsSensorHasNoValve(t *testing.T) {
	r := NewReports(receiver)

	for _, s := range r.Samples(update(t, "hall", time.Unix(1, 0), `{"temperature":19,"running_state":"idle"}`)) {
		assert.Equal(t, history.Temperature, s.Metric)
	}
}

func TestDisplayName(t *testing.T) {
	assert.Equal(t, "lounge", displayName("tah_lounge"))
	assert.Equal(t, "lounge", displayName("trv_lounge"))
	assert.Equal(t, "receiver", displayName("hive_receiver"))
	assert.Equal(t, "lounge", displayName("lounge"))
}
