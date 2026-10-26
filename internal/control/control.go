// Package control closes radiator valves as the room they heat warms up, so a room which is
// already warm stops taking heat which a colder room could use.
package control

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jamesl33/hive-cli/internal/history"
)

// Config is how the controller behaves. The opening of a valve is 100% while the room is Lo below
// its target or more, and falls by Kp percent for every degree the room warms past that, down to Min.
type Config struct {
	// Kp is the percent the valve closes by for each °C the room warms.
	Kp float64

	// Lo is how many °C below the target the room is when the valve starts to close.
	Lo float64

	// Min is the smallest opening in percent, so the boiler never runs against closed valves.
	Min float64

	// Lead is how far ahead to look, using the rate the room is warming at, so the valve closes before
	// the room reaches its target and not after.
	Lead time.Duration

	// Window is how much history the rate is worked out from.
	Window time.Duration

	// Step is how far apart the readings the rate is worked out from are, taking the latest reading at
	// each step.
	Step time.Duration

	// MinPoints is the fewest readings in the window to trust the rate; with fewer there is no lead.
	MinPoints int

	// Stale is how long a sensor can go without reporting before its valve is opened fully.
	Stale time.Duration

	// WriteEvery is the shortest time between writes to a valve, as they run on batteries.
	WriteEvery time.Duration

	// WriteMin is the smallest change in opening in percent which is worth a write.
	WriteMin float64

	// Round is what openings are rounded to, in percent.
	Round float64
}

// DefaultConfig returns a slow configuration, which is meant to be tuned from the logs.
func DefaultConfig() Config {
	c := Config{
		Kp:         50,
		Lo:         0.5,
		Min:        10,
		Lead:       20 * time.Minute,
		Window:     60 * time.Minute,
		Step:       5 * time.Minute,
		MinPoints:  6,
		Stale:      30 * time.Minute,
		WriteEvery: 30 * time.Minute,
		WriteMin:   15,
		Round:      5,
	}

	return c
}

// Room is a room to control: one temperature sensor and the radiator valve which heats it.
type Room struct {
	// Name is the name of the room, used in logs.
	Name string

	// Sensor is the friendly name of the temperature sensor.
	Sensor string

	// Valve is the friendly name of the radiator valve.
	Valve string

	// Offset is how many °C above the target of the thermostat the room should be held at.
	Offset float64

	// Observe works out what the valve would be set to without ever writing it.
	Observe bool
}

// Valves sets the opening of radiator valves.
type Valves interface {
	// SetOpening tells the valve to open to the percent.
	SetOpening(ctx context.Context, valve string, percent float64) error
}

// Decision is what the controller worked out for a room on a tick.
type Decision struct {
	// Room is the name of the room.
	Room string

	// Valve is the friendly name of the valve.
	Valve string

	// Temperature is the latest reading of the room in °C.
	Temperature float64

	// Rate is how fast the room is warming in °C per hour.
	Rate float64

	// Predicted is the temperature the room is heading to after the lead in °C.
	Predicted float64

	// Target is the temperature the room is held at in °C.
	Target float64

	// Opening is the opening the controller wants in percent.
	Opening float64

	// Current is the opening the valve last reported in percent.
	Current float64

	// Stale is set if the sensor stopped reporting, so the valve is opened fully.
	Stale bool

	// Written is set if the valve was written to.
	Written bool

	// Reason says why the valve was not written to, empty if it was.
	Reason string
}

// Controller works out the opening of the valve of each room from the recorded history.
type Controller struct {
	cfg    Config
	valves Valves
	rooms  []*roomState

	// temps is every reading of each sensor, oldest first.
	temps map[string][]reading

	// openings is the latest opening of each valve and when it changed.
	openings map[string]reading

	// target is the target of the thermostat, nil until it has been seen.
	target *float64
}

// roomState is what the controller works out about a room on a tick.
type roomState struct {
	Room

	// points are the readings of the room across the window, one per step.
	points []point
}

// reading is a value reported at a time.
type reading struct {
	value float64
	at    time.Time
}

// point is a temperature at a time.
type point struct {
	at    time.Time
	value float64
}

// New creates a controller.
func New(cfg Config, rooms []Room, valves Valves) *Controller {
	c := &Controller{
		cfg:      cfg,
		valves:   valves,
		temps:    make(map[string][]reading),
		openings: make(map[string]reading),
	}

	for _, r := range rooms {
		c.rooms = append(c.rooms, &roomState{Room: r})
	}

	return c
}

// Add takes in a recorded measurement, which must be newer than the ones before it: the temperature of
// a sensor, the opening of a valve or the target of the thermostat. Anything else is ignored.
func (c *Controller) Add(s history.Sample) {
	switch s.Metric {
	case history.Temperature:
		c.temps[s.Device] = append(c.temps[s.Device], reading{value: s.Value, at: s.At})
	case history.Valve:
		c.openings[s.Device] = reading{value: s.Value, at: s.At}
	case history.Target:
		c.target = &s.Value
	}
}

// Tick works out every room and writes the valves which need it.
func (c *Controller) Tick(ctx context.Context, now time.Time) ([]Decision, error) {
	var (
		decisions []Decision
		errs      []error
	)

	for _, rs := range c.rooms {
		d := c.decide(rs, now)

		if d.Written {
			err := c.valves.SetOpening(ctx, rs.Valve, d.Opening)
			if err != nil {
				d.Written = false
				d.Reason = "write failed"

				errs = append(errs, fmt.Errorf("failed to set %s: %w", rs.Valve, err))
			}
		}

		decisions = append(decisions, d)
	}

	return decisions, errors.Join(errs...)
}

// Restore opens every valve fully.
func (c *Controller) Restore(ctx context.Context) error {
	var errs []error

	for _, rs := range c.rooms {
		err := c.valves.SetOpening(ctx, rs.Valve, 100)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to open %s: %w", rs.Valve, err))
		}
	}

	return errors.Join(errs...)
}

// decide works out the opening of the valve of a room, and whether to write it.
func (c *Controller) decide(rs *roomState, now time.Time) Decision {
	d := Decision{Room: rs.Name, Valve: rs.Valve, Current: 100}

	current, ok := c.openings[rs.Valve]
	if ok {
		d.Current = current.value
	}

	if c.target == nil {
		d.Opening = d.Current
		d.Reason = "no target"

		return d
	}

	d.Target = *c.target + rs.Offset

	readings := c.temps[rs.Sensor]
	if len(readings) == 0 || now.Sub(readings[len(readings)-1].at) > c.cfg.Stale {
		d.Stale = true
		d.Opening = 100
		d.Reason = c.blocked(rs, now, d, true)
		d.Written = d.Reason == ""

		return d
	}

	rs.points = c.points(readings, now)

	d.Temperature = readings[len(readings)-1].value
	d.Rate = rs.rate(c.cfg.MinPoints) * 60
	d.Predicted = d.Temperature + d.Rate*c.cfg.Lead.Hours()
	d.Opening = c.opening(d.Target - d.Predicted)
	d.Reason = c.blocked(rs, now, d, false)
	d.Written = d.Reason == ""

	return d
}

// opening returns the opening for a room which is err °C below its target, rounded.
func (c *Controller) opening(err float64) float64 {
	u := 100 + c.cfg.Kp*(err-c.cfg.Lo)
	u = math.Min(100, math.Max(c.cfg.Min, u))

	return math.Round(u/c.cfg.Round) * c.cfg.Round
}

// blocked says why the valve should not be written to, or returns empty if it should. An urgent
// write is one which makes the house safe, which is not held back by the limits on writes.
func (c *Controller) blocked(rs *roomState, now time.Time, d Decision, urgent bool) string {
	step := c.cfg.WriteMin
	if urgent {
		step = 1
	}

	switch {
	case rs.Observe:
		return "observe"
	case math.Abs(d.Opening-d.Current) < step:
		return "within write step"
	case urgent:
		return ""
	case c.openedWithin(rs.Valve, now, c.cfg.WriteEvery):
		return "write limit"
	}

	return ""
}

// openedWithin returns whether the opening of the valve changed within the last d. Valves only
// report a changed opening, so this is when the valve was last written to.
func (c *Controller) openedWithin(valve string, now time.Time, d time.Duration) bool {
	o, ok := c.openings[valve]

	return ok && now.Sub(o.at) < d
}

// points returns the readings across the window up to now, taking the latest reading at each step.
// A step before the first reading has no point.
func (c *Controller) points(readings []reading, now time.Time) []point {
	var points []point

	for t := now.Add(-c.cfg.Window); !t.After(now); t = t.Add(c.cfg.Step) {
		v, ok := valueAt(readings, t)
		if ok {
			points = append(points, point{at: t, value: v})
		}
	}

	return points
}

// valueAt returns the latest reading at or before the time.
func valueAt(readings []reading, t time.Time) (float64, bool) {
	i := sort.Search(len(readings), func(i int) bool { return readings[i].at.After(t) })
	if i == 0 {
		return 0, false
	}

	return readings[i-1].value, true
}

// rate returns the slope of the readings in °C per minute, or 0 if there are fewer than points.
func (rs *roomState) rate(points int) float64 {
	n := float64(len(rs.points))
	if len(rs.points) < points {
		return 0
	}

	var sx, sy, sxy, sxx float64

	for _, p := range rs.points {
		x := p.at.Sub(rs.points[0].at).Minutes()

		sx += x
		sy += p.value
		sxy += x * p.value
		sxx += x * x
	}

	den := n*sxx - sx*sx
	if den == 0 {
		return 0
	}

	return (n*sxy - sx*sy) / den
}
