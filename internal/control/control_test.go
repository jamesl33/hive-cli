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

// run ticks every five minutes, with the sensor reading temp(minute) each time.
func run(t *testing.T, c *Controller, minutes int, temp func(minute int) float64) []Decision {
	t.Helper()

	var last []Decision

	for m := 0; m <= minutes; m += 5 {
		now := start.Add(time.Duration(m) * time.Minute)
		c.Add(history.Sample{Device: "tah_office", Metric: history.Temperature, At: now, Value: temp(m)})

		var err error

		last, err = c.Tick(t.Context(), now)
		require.NoError(t, err)
	}

	return last
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
		c.SetOpening("trv_office", 75)

		d := run(t, c, 30, steady(16.5))[0]

		assert.Equal(t, "within write step", d.Reason)
		assert.Empty(t, v.set)
	})

	t.Run("a write is not repeated within the limit", func(t *testing.T) {
		v := &recorder{}
		c := rig(v)

		c.Add(history.Sample{Device: "tah_office", Metric: history.Temperature, At: start, Value: 17.5})

		d, err := c.Tick(t.Context(), start)
		require.NoError(t, err)
		assert.True(t, d[0].Written)

		c.Add(history.Sample{Device: "tah_office", Metric: history.Temperature, At: start.Add(5 * time.Minute), Value: 15})
		c.SetOpening("trv_office", 25)

		d, err = c.Tick(t.Context(), start.Add(5*time.Minute))
		require.NoError(t, err)
		assert.Equal(t, "write limit", d[0].Reason)
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
	c.SetOpening("trv_office", 25)
	c.Add(history.Sample{Device: "tah_office", Metric: history.Temperature, At: start, Value: 17.5})

	d, err := c.Tick(t.Context(), start.Add(31*time.Minute))
	require.NoError(t, err)

	assert.True(t, d[0].Stale)
	assert.Equal(t, 100.0, d[0].Opening)
	assert.True(t, d[0].Written)
	assert.Equal(t, 100.0, v.set["trv_office"])
}

func TestStaleSensorIgnoresWriteLimit(t *testing.T) {
	v := &recorder{}
	c := rig(v)
	c.SetOpening("trv_office", 25)
	c.rooms[0].lastWrite = start

	d, err := c.Tick(t.Context(), start.Add(time.Minute))
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

func TestReady(t *testing.T) {
	c := New(DefaultConfig(), []Room{office}, &recorder{})
	assert.False(t, c.Ready())

	c.Add(history.Sample{Device: "hive_receiver", Metric: history.Target, At: start, Value: 16.5})
	assert.False(t, c.Ready())

	c.Add(history.Sample{Device: "tah_bedroom", Metric: history.Temperature, At: start, Value: 16})
	assert.False(t, c.Ready())

	c.Add(history.Sample{Device: "tah_office", Metric: history.Temperature, At: start, Value: 16})
	assert.True(t, c.Ready())
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
		rs.add(start.Add(time.Duration(m)*5*time.Minute), 15+float64(m)*0.1, time.Hour)
	}

	assert.InDelta(t, 0.02, rs.rate(6), 1e-9)
	assert.Zero(t, rs.rate(13))
}
