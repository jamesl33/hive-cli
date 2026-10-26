package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var start = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

// recorder is Valves which remembers what it was told.
type recorder struct {
	set map[string]float64
	err error
}

func (r *recorder) SetOpening(_ context.Context, valve string, percent float64) error {
	if r.err != nil {
		return r.err
	}

	if r.set == nil {
		r.set = make(map[string]float64)
	}

	r.set[valve] = percent

	return nil
}

var office = Room{Name: "office", Sensor: "tah_office", Valve: "trv_office"}

// rig creates a controller for the office with the thermostat at 16.5 °C.
func rig(valves Valves, rooms ...Room) *Controller {
	if len(rooms) == 0 {
		rooms = []Room{office}
	}

	c := New(DefaultConfig(), rooms, valves)
	c.Add(history.Sample{Device: "hive_receiver", Metric: history.Target, At: start, Value: 16.5})

	return c
}

// run adds a reading of the office sensor every five minutes, as temp(minute), then ticks at the end.
func run(t *testing.T, c *Controller, minutes int, temp func(minute int) float64) []Decision {
	t.Helper()

	for m := 0; m <= minutes; m += 5 {
		c.Add(history.Sample{Device: "tah_office", Metric: history.Temperature, At: at(m), Value: temp(m)})
	}

	d, err := c.Tick(t.Context(), at(minutes))
	require.NoError(t, err)

	return d
}

// at returns the time which is the minutes after the start.
func at(minutes int) time.Time { return start.Add(time.Duration(minutes) * time.Minute) }

// opened records that the office valve changed to the percent at the time.
func opened(c *Controller, percent float64, when time.Time) {
	c.Add(history.Sample{Device: "trv_office", Metric: history.Valve, At: when, Value: percent})
}

func steady(v float64) func(int) float64 { return func(int) float64 { return v } }

func TestOpening(t *testing.T) {
	for _, tc := range []struct {
		name string
		temp float64
		want float64
	}{
		{"cold room stays fully open", 15.0, 100},
		{"open until it is lo below target", 16.0, 100},
		{"closing at target", 16.5, 75},
		{"half a degree over", 17.0, 50},
		{"a degree over", 17.5, 25},
		{"far over stays at the minimum", 20.0, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := run(t, rig(&recorder{}), 60, steady(tc.temp))

			assert.Equal(t, tc.want, d[0].Opening)
		})
	}
}

func TestOffset(t *testing.T) {
	r := office
	r.Offset = -1

	d := run(t, rig(&recorder{}, r), 60, steady(16.5))

	assert.InDelta(t, 15.5, d[0].Target, 1e-9)
	assert.Equal(t, 25.0, d[0].Opening)
}

func TestLeadClosesBeforeTarget(t *testing.T) {
	rising := func(m int) float64 { return 15.5 + float64(m)*0.02 }

	d := run(t, rig(&recorder{}), 60, rising)[0]

	assert.InDelta(t, 1.2, d.Rate, 0.01)
	assert.Greater(t, d.Predicted, d.Temperature)
	assert.Less(t, d.Opening, run(t, rig(&recorder{}), 60, steady(d.Temperature))[0].Opening)
}

func TestNoLeadWithoutHistory(t *testing.T) {
	d := run(t, rig(&recorder{}), 10, func(m int) float64 { return 15.5 + float64(m)*0.1 })[0]

	assert.Zero(t, d.Rate)
	assert.Equal(t, d.Temperature, d.Predicted)
}

func TestWrites(t *testing.T) {
	v := &recorder{}
	c := rig(v)

	run(t, c, 30, steady(17.5))

	assert.Equal(t, 25.0, v.set["trv_office"])
}

func TestWriteLimits(t *testing.T) {
	t.Run("a small change is not worth a write", func(t *testing.T) {
		v := &recorder{}
		c := rig(v)
		opened(c, 75, at(-60))

		d := run(t, c, 30, steady(16.5))[0]

		assert.Equal(t, "within write step", d.Reason)
		assert.Empty(t, v.set)
	})

	t.Run("a valve which changed within the limit is not written", func(t *testing.T) {
		v := &recorder{}
		c := rig(v)
		opened(c, 100, at(25))

		d := run(t, c, 30, steady(17.5))[0]

		assert.Equal(t, "write limit", d.Reason)
		assert.Empty(t, v.set)
	})

	t.Run("a valve which changed before the limit is written", func(t *testing.T) {
		v := &recorder{}
		c := rig(v)
		opened(c, 100, at(-1))

		d := run(t, c, 30, steady(17.5))[0]

		assert.True(t, d.Written)
		assert.Equal(t, 25.0, v.set["trv_office"])
	})

	t.Run("failed write is reported", func(t *testing.T) {
		v := &recorder{err: errors.New("broker down")}
		c := rig(v)

		c.Add(history.Sample{Device: "tah_office", Metric: history.Temperature, At: start, Value: 17.5})

		d, err := c.Tick(t.Context(), start)

		require.Error(t, err)
		assert.Equal(t, "write failed", d[0].Reason)
		assert.False(t, d[0].Written)
	})
}

func TestObserve(t *testing.T) {
	v := &recorder{}
	r := office
	r.Observe = true

	d := run(t, rig(v, r), 30, steady(17.5))[0]

	assert.Equal(t, "observe", d.Reason)
	assert.Empty(t, v.set)
}

func TestStaleSensorOpensFully(t *testing.T) {
	v := &recorder{}
	c := rig(v)
	opened(c, 25, at(-60))
	c.Add(history.Sample{Device: "tah_office", Metric: history.Temperature, At: start, Value: 17.5})

	d, err := c.Tick(t.Context(), at(31))
	require.NoError(t, err)

	assert.True(t, d[0].Stale)
	assert.Equal(t, 100.0, d[0].Opening)
	assert.True(t, d[0].Written)
	assert.Equal(t, 100.0, v.set["trv_office"])
}

func TestStaleSensorIgnoresWriteLimit(t *testing.T) {
	v := &recorder{}
	c := rig(v)
	opened(c, 25, start)

	d, err := c.Tick(t.Context(), at(1))
	require.NoError(t, err)

	assert.True(t, d[0].Stale)
	assert.True(t, d[0].Written)
}

func TestNoTargetNoWrites(t *testing.T) {
	v := &recorder{}
	c := New(DefaultConfig(), []Room{office}, v)
	c.Add(history.Sample{Device: "tah_office", Metric: history.Temperature, At: start, Value: 17.5})

	d, err := c.Tick(t.Context(), start)
	require.NoError(t, err)

	assert.Equal(t, "no target", d[0].Reason)
	assert.Empty(t, v.set)
}

func TestRestore(t *testing.T) {
	v := &recorder{}
	c := rig(v, office, Room{Name: "landing", Sensor: "tah_landing", Valve: "trv_landing"})

	require.NoError(t, c.Restore(t.Context()))
	assert.Equal(t, map[string]float64{"trv_office": 100, "trv_landing": 100}, v.set)
}

func TestRate(t *testing.T) {
	rs := &roomState{}

	for m := range 12 {
		rs.points = append(rs.points, point{at: at(m * 5), value: 15 + float64(m)*0.1})
	}

	assert.InDelta(t, 0.02, rs.rate(6), 1e-9)
	assert.Zero(t, rs.rate(13))
}

func TestPointsForwardFill(t *testing.T) {
	c := New(DefaultConfig(), []Room{office}, &recorder{})
	readings := []reading{{value: 15, at: at(2)}, {value: 16, at: at(32)}}

	got := c.points(readings, at(60))

	require.Len(t, got, 12)
	assert.Equal(t, point{at: at(5), value: 15}, got[0])
	assert.Equal(t, 15.0, got[5].value)
	assert.Equal(t, 16.0, got[6].value)
	assert.Equal(t, point{at: at(60), value: 16}, got[11])
}
