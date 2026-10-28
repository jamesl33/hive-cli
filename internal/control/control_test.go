package control

import (
	"context"
	"errors"
	"math"
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
		{"cold room stays fully open", 14.0, 100},
		{"open until the whole range below target", 14.7, 100},
		{"closing on the way up", 16.0, 35},
		{"at the minimum at target", 16.5, 10},
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
	r.Offset = 1

	d := run(t, rig(&recorder{}, r), 60, steady(16.5))

	assert.InDelta(t, 17.5, d[0].Target, 1e-9)
	assert.Equal(t, 60.0, d[0].Opening)
}

func TestLeadClosesBeforeTarget(t *testing.T) {
	rising := func(m int) float64 { return 14.8 + float64(m)*0.02 }

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

	assert.Equal(t, 10.0, v.set["trv_office"])
}

func TestWriteLimits(t *testing.T) {
	t.Run("a small change is not worth a write", func(t *testing.T) {
		v := &recorder{}
		c := rig(v)
		c.SetOpening("trv_office", 40)

		d := run(t, c, 30, steady(16.0))[0]

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

		c.Add(history.Sample{Device: "tah_office", Metric: history.Temperature, At: start.Add(4 * time.Minute), Value: 15})

		d, err = c.Tick(t.Context(), start.Add(4*time.Minute))
		require.NoError(t, err)
		assert.Equal(t, "write limit", d[0].Reason)
		assert.Equal(t, 10.0, v.set["trv_office"])
	})

	t.Run("a close to the minimum ignores the write step", func(t *testing.T) {
		v := &recorder{}
		c := rig(v)
		c.SetOpening("trv_office", 20)

		run(t, c, 30, steady(16.5))

		assert.Equal(t, 10.0, v.set["trv_office"])
	})

	t.Run("a close skips the time limit", func(t *testing.T) {
		v := &recorder{}
		c := rig(v)
		c.Add(history.Sample{Device: "tah_office", Metric: history.Temperature, At: start, Value: 15.0})

		d, err := c.Tick(t.Context(), start)
		require.NoError(t, err)
		assert.Equal(t, 85.0, d[0].Opening)
		assert.True(t, d[0].Written)

		now := start.Add(time.Minute)
		c.Add(history.Sample{Device: "tah_office", Metric: history.Temperature, At: now, Value: 16.0})

		d, err = c.Tick(t.Context(), now)
		require.NoError(t, err)
		assert.Equal(t, 35.0, d[0].Opening)
		assert.True(t, d[0].Written)
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

	d, err := c.Tick(t.Context(), start.Add(2*time.Hour+time.Minute))
	require.NoError(t, err)

	assert.True(t, d[0].Stale)
	assert.Equal(t, 100.0, d[0].Opening)
	assert.True(t, d[0].Written)
	assert.Equal(t, 100.0, v.set["trv_office"])
}

func TestQuietSensorIsNotStale(t *testing.T) {
	v := &recorder{}
	c := rig(v)
	c.SetOpening("trv_office", 10)
	c.Add(history.Sample{Device: "tah_office", Metric: history.Temperature, At: start, Value: 17.5})

	d, err := c.Tick(t.Context(), start.Add(90*time.Minute))
	require.NoError(t, err)

	assert.False(t, d[0].Stale)
	assert.Empty(t, v.set)
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
	c := rig(
		v,
		office,
		Room{Name: "landing", Sensor: "tah_landing", Valve: "trv_landing"},
		Room{Name: "lounge", Sensor: "tah_lounge", Valve: "trv_kitchen", Observe: true},
	)

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

func TestClosesFullyWhileObservedValveOpen(t *testing.T) {
	for _, tc := range []struct {
		name     string
		observed float64
		want     float64
	}{
		{"observed open", 100, 0},
		{"observed closed", 0, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lounge := Room{Name: "lounge", Sensor: "tah_lounge", Valve: "trv_kitchen", Observe: true}
			c := rig(&recorder{}, office, lounge)
			c.SetOpening("trv_office", 100)
			c.SetOpening("trv_kitchen", tc.observed)
			c.Add(history.Sample{Device: "tah_lounge", Metric: history.Temperature, At: start, Value: 15})

			d := run(t, c, 30, steady(18))[0]

			assert.Equal(t, tc.want, d.Opening)
		})
	}
}

func TestRaisesToMinWhenObservedValveCloses(t *testing.T) {
	v := &recorder{}
	lounge := Room{Name: "lounge", Sensor: "tah_lounge", Valve: "trv_kitchen", Observe: true}
	c := rig(v, office, lounge)
	c.SetOpening("trv_office", 0)
	c.SetOpening("trv_kitchen", 0)
	c.Add(history.Sample{Device: "tah_lounge", Metric: history.Temperature, At: start, Value: 15})
	c.rooms[0].lastWrite = start

	d := run(t, c, 0, steady(18))[0]

	assert.True(t, d.Written)
	assert.Equal(t, 10.0, v.set["trv_office"])
}

func TestOffClosesValveFully(t *testing.T) {
	v := &recorder{}
	r := office
	r.Off = true
	c := rig(v, r)
	c.SetOpening("trv_office", 100)
	c.rooms[0].lastWrite = start

	d := run(t, c, 0, steady(10))[0]

	assert.True(t, d.Written)
	assert.Equal(t, 0.0, d.Opening)
	assert.Equal(t, 10.0, d.Temperature)
	assert.Equal(t, 0.0, v.set["trv_office"])
}

func TestOffWritesNothingOnceClosed(t *testing.T) {
	v := &recorder{}
	r := office
	r.Off = true
	c := rig(v, r)
	c.SetOpening("trv_office", 0)

	d := run(t, c, 30, steady(10))[0]

	assert.False(t, d.Written)
	assert.Equal(t, "off", d.Reason)
	assert.Empty(t, v.set)
}

func TestOffClosesWithoutTarget(t *testing.T) {
	v := &recorder{}
	r := office
	r.Off = true
	c := New(DefaultConfig(), []Room{r}, v)

	d, err := c.Tick(t.Context(), start)
	require.NoError(t, err)

	assert.True(t, d[0].Written)
	assert.Equal(t, 0.0, v.set["trv_office"])
}

func TestRestoreSkipsOffValves(t *testing.T) {
	v := &recorder{}
	landing := Room{Name: "landing", Sensor: "tah_landing", Valve: "trv_landing", Off: true}
	c := rig(v, office, landing)

	require.NoError(t, c.Restore(t.Context()))
	assert.Equal(t, map[string]float64{"trv_office": 100}, v.set)
}

func TestValidate(t *testing.T) {
	require.NoError(t, DefaultConfig().Validate())

	for name, mutate := range map[string]func(*Config){
		"kp":     func(c *Config) { c.Kp = 0 },
		"min":    func(c *Config) { c.Min = 101 },
		"lead":   func(c *Config) { c.Lead = -1 },
		"window": func(c *Config) { c.Window = 0 },
		"stale":  func(c *Config) { c.Stale = 0 },
		"round":  func(c *Config) { c.Round = 0 },
		"nan":    func(c *Config) { c.Kp = math.NaN() },
	} {
		t.Run(name, func(t *testing.T) {
			c := DefaultConfig()
			mutate(&c)

			assert.Error(t, c.Validate())
		})
	}
}

func TestValidateRooms(t *testing.T) {
	a := Room{Name: "a", Sensor: "s1", Valve: "v1"}

	require.NoError(t, ValidateRooms([]Room{a, {Name: "b", Sensor: "s1", Valve: "v2"}}))
	assert.Error(t, ValidateRooms([]Room{a, {Name: "a", Sensor: "s2", Valve: "v2"}}))
	assert.Error(t, ValidateRooms([]Room{a, {Name: "b", Sensor: "s2", Valve: "v1"}}))
}

func TestOpensFullyPastTheWriteStep(t *testing.T) {
	v := &recorder{}
	c := rig(v)
	c.SetOpening("trv_office", 90)

	run(t, c, 30, steady(14.0))

	assert.Equal(t, 100.0, v.set["trv_office"])
}
