// Package control closes radiator valves as the room they heat warms up, so a room which is
// already warm stops taking heat which a colder room could use.
package control

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jamesl33/hive-cli/internal/history"
)

// Config is how the controller behaves. The opening of a valve is Min when the room is at its target
// and rises by Kp percent for every degree the room is below it, up to 100.
type Config struct {
	// Kp is the percent the valve closes by for each °C the room warms.
	Kp float64

	// Min is the smallest opening in percent, so the boiler never runs against closed valves.
	Min float64

	// Lead is how far ahead to look, using the rate the room is warming at, so the valve closes before
	// the room reaches its target and not after.
	Lead time.Duration

	// Window is how much history the rate is worked out from.
	Window time.Duration

	// MinPoints is the fewest readings in the window to trust the rate; with fewer there is no lead.
	MinPoints int

	// Stale is how long a sensor can go without reporting before its valve is opened fully.
	Stale time.Duration

	// WriteEvery is the shortest time between writes which open a valve, as they run on batteries.
	WriteEvery time.Duration

	// WriteMin is the smallest change in opening in percent which is worth a write.
	WriteMin float64

	// Round is what openings are rounded to, in percent.
	Round float64
}

const (
	// defaultKp is 50% of closing for each °C the room warms.
	defaultKp = 50

	// defaultMin is the smallest opening in percent, so the boiler never runs against closed valves.
	defaultMin = 10

	// defaultLead is 20 minutes of looking ahead at the rate the room is warming at.
	defaultLead = 20 * time.Minute

	// defaultWindow is the 30 minutes of readings which the rate is worked out from.
	defaultWindow = 30 * time.Minute

	// defaultMinPoints is the fewest readings in the window to trust the rate.
	defaultMinPoints = 6

	// defaultStale is the 30 minutes of silence after which the valve of a sensor is opened fully.
	defaultStale = 30 * time.Minute

	// defaultWriteEvery is the shortest time between writes which open one valve, as they run on batteries.
	defaultWriteEvery = 5 * time.Minute

	// defaultWriteMin is the smallest change in opening in percent which is worth a write.
	defaultWriteMin = 15

	// defaultRound is what openings are rounded to in percent.
	defaultRound = 5
)

// DefaultConfig returns a slow configuration, which is meant to be tuned from the logs.
func DefaultConfig() Config {
	c := Config{
		Kp:         defaultKp,
		Min:        defaultMin,
		Lead:       defaultLead,
		Window:     defaultWindow,
		MinPoints:  defaultMinPoints,
		Stale:      defaultStale,
		WriteEvery: defaultWriteEvery,
		WriteMin:   defaultWriteMin,
		Round:      defaultRound,
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

// Controller works out the opening of the valve of each room.
type Controller struct {
	cfg    Config
	valves Valves
	rooms  []*roomState

	// temps is the latest reading of each sensor.
	temps map[string]reading

	// openings is the opening each valve last reported.
	openings map[string]float64

	// target is the target of the thermostat, nil until it has been seen.
	target *float64
}

// roomState is what the controller remembers about a room.
type roomState struct {
	Room

	// points are the readings of the room on recent ticks, one per tick.
	points []point

	// lastWrite is when the valve was last written to.
	lastWrite time.Time
}

// reading is a temperature report.
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
		temps:    make(map[string]reading),
		openings: make(map[string]float64),
	}

	for _, r := range rooms {
		c.rooms = append(c.rooms, &roomState{Room: r})
	}

	return c
}

// Add takes in a measurement: the temperature of a sensor, or the target of the thermostat. Anything
// else is ignored.
func (c *Controller) Add(s history.Sample) {
	switch s.Metric {
	case history.Temperature:
		c.temps[s.Device] = reading{value: s.Value, at: s.At}
	case history.Target:
		c.target = &s.Value
	}
}

// SetOpening takes in the opening a valve reported in percent.
func (c *Controller) SetOpening(valve string, percent float64) {
	c.openings[valve] = percent
}

// Ready returns whether the controller has the target of the thermostat and a reading from the sensor
// of every room.
func (c *Controller) Ready() bool {
	if c.target == nil {
		return false
	}

	for _, rs := range c.rooms {
		_, ok := c.temps[rs.Sensor]
		if !ok {
			return false
		}
	}

	return true
}

// Tick works out every room and writes the valves which need it.
func (c *Controller) Tick(ctx context.Context, now time.Time) ([]Decision, error) {
	var (
		decisions []Decision
		errs      []error
	)

	for _, rs := range c.rooms {
		d, err := c.tickRoom(ctx, rs, now)
		if err != nil {
			errs = append(errs, err)
		}

		decisions = append(decisions, d)
	}

	return decisions, errors.Join(errs...)
}

// tickRoom works out a room and writes its valve if it needs it.
func (c *Controller) tickRoom(ctx context.Context, rs *roomState, now time.Time) (Decision, error) {
	d := c.decide(rs, now)
	if !d.Written {
		return d, nil
	}

	err := c.valves.SetOpening(ctx, rs.Valve, d.Opening)
	if err != nil {
		d.Written = false
		d.Reason = "write failed"

		return d, fmt.Errorf("failed to set %s: %w", rs.Valve, err)
	}

	rs.lastWrite = now
	c.openings[rs.Valve] = d.Opening

	return d, nil
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
		d.Current = current
	}

	if c.target == nil {
		d.Opening = d.Current
		d.Reason = "no target"

		return d
	}

	d.Target = *c.target + rs.Offset

	t, ok := c.temps[rs.Sensor]
	if !ok || now.Sub(t.at) > c.cfg.Stale {
		rs.points = nil

		d.Stale = true
		d.Opening = 100
		d.Reason = c.blocked(rs, now, d, true)
		d.Written = d.Reason == ""

		return d
	}

	rs.add(now, t.value, c.cfg.Window)

	d.Temperature = t.value
	d.Rate = rs.rate(c.cfg.MinPoints) * 60
	d.Predicted = t.value + d.Rate*c.cfg.Lead.Hours()
	d.Opening = c.opening(d.Target - d.Predicted)

	d.Reason = c.blocked(rs, now, d, false)
	d.Written = d.Reason == ""

	return d
}

// opening returns the opening for a room which is err °C below its target, rounded.
func (c *Controller) opening(err float64) float64 {
	u := c.cfg.Min + c.cfg.Kp*err
	u = math.Min(100, math.Max(c.cfg.Min, u))

	return math.Round(u/c.cfg.Round) * c.cfg.Round
}

// blocked says why the valve should not be written to, or returns empty if it should. An urgent
// write is one which makes the house safe, which is not held back by the limits on writes. A close is
// only held back by the write step, except to the minimum, as every minute of delay is overshoot.
func (c *Controller) blocked(rs *roomState, now time.Time, d Decision, urgent bool) string {
	closing := d.Opening < d.Current
	step := c.cfg.WriteMin
	if urgent {
		step = 1
	}

	switch {
	case rs.Observe:
		return "observe"
	case closing && d.Opening == c.cfg.Min:
		return ""
	case math.Abs(d.Opening-d.Current) < step:
		return "within write step"
	case urgent, closing:
		return ""
	case !rs.lastWrite.IsZero() && now.Sub(rs.lastWrite) < c.cfg.WriteEvery:
		return "write limit"
	}

	return ""
}

// add records a reading and forgets anything older than the window.
func (rs *roomState) add(now time.Time, value float64, window time.Duration) {
	rs.points = append(rs.points, point{at: now, value: value})

	for len(rs.points) > 0 && now.Sub(rs.points[0].at) > window {
		rs.points = rs.points[1:]
	}
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
