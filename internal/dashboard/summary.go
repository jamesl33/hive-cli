package dashboard

import (
	"cmp"
	"math"
	"slices"
	"time"
)

// DeviceSummary is the temperature of a device over a period.
type DeviceSummary struct {
	// Device is the friendly name of the device.
	Device string

	// Mean is the temperature averaged over the time it was known.
	Mean float64

	// Min is the lowest temperature in the period.
	Min float64

	// Max is the highest temperature in the period.
	Max float64

	// sum is the temperature multiplied by the seconds it was held, added up; Mean is sum over span.
	sum float64

	// span is the seconds the temperature was known for.
	span float64
}

// Summary is a period of recorded history boiled down.
type Summary struct {
	// Rooms is the temperature of every device but the outdoor one, by name.
	Rooms []DeviceSummary

	// Outdoors is the outdoor device, if it has any history in the period.
	Outdoors *DeviceSummary

	// SpreadMean is the difference between the warmest and coolest room averaged over the time at
	// least two rooms had a temperature.
	SpreadMean float64

	// SpreadMax is the largest difference between the warmest and coolest room.
	SpreadMax float64

	// BoilerOn is how long the boiler was firing.
	BoilerOn time.Duration

	// BoilerRuns is how many times the boiler started.
	BoilerRuns int
}

// Summarize works out the temperature of each device, the spread between the rooms and the boiler use
// from the samples between from and to, oldest first. A series is held at its last value, so samples
// from before from set the starting values.
func Summarize(samples []Sample, outdoors string, from, to time.Time) Summary {
	t := newTally(outdoors, from)

	for _, s := range samples {
		if s.At.After(to) {
			break
		}

		t.apply(s)
	}

	t.account(to)

	return t.summary()
}

// tally adds up a summary while the samples of a period are replayed in order.
type tally struct {
	// outdoors is the device which is left out of the spread.
	outdoors string

	// from is when the period starts, which samples from before it can't count runs before.
	from time.Time

	// prev is the time which has been added up to.
	prev time.Time

	// stats is the temperature of each device so far.
	stats map[string]*DeviceSummary

	// current is the latest temperature of each device.
	current map[string]float64

	// boiler is the latest state of the boiler.
	boiler float64

	// spreadSum is the spread multiplied by the seconds it lasted, added up.
	spreadSum float64

	// spreadSpan is the seconds there was a spread to measure.
	spreadSpan float64

	// spreadMax is the largest spread so far.
	spreadMax float64

	// boilerOn is how long the boiler has been firing.
	boilerOn time.Duration

	// boilerRuns is how many times the boiler has started.
	boilerRuns int
}

// newTally returns a tally of the period which starts at from.
func newTally(outdoors string, from time.Time) *tally {
	t := tally{
		outdoors: outdoors,
		from:     from,
		prev:     from,
		stats:    make(map[string]*DeviceSummary),
		current:  make(map[string]float64),
		boiler:   BoilerOff,
	}

	return &t
}

// apply adds up the time before the sample, then takes in what the sample says.
func (t *tally) apply(s Sample) {
	t.account(s.At)

	switch s.Metric {
	case Temperature:
		t.current[s.Device] = s.Value
	case Boiler:
		t.setBoiler(s)
	}
}

// setBoiler takes in the state of the boiler, counting a run if it started in the period.
func (t *tally) setBoiler(s Sample) {
	started := s.Value == BoilerOn && t.boiler != BoilerOn
	if started && !s.At.Before(t.from) {
		t.boilerRuns++
	}

	t.boiler = s.Value
}

// account adds up the time from prev until the time, during which nothing changed.
func (t *tally) account(until time.Time) {
	elapsed := until.Sub(t.prev)
	if elapsed <= 0 {
		return
	}

	t.prev = until
	secs := elapsed.Seconds()
	lo, hi, rooms := math.Inf(1), math.Inf(-1), 0

	for device, v := range t.current {
		t.record(device, v, secs)

		if device == t.outdoors {
			continue
		}

		lo, hi, rooms = math.Min(lo, v), math.Max(hi, v), rooms+1
	}

	if t.boiler == BoilerOn {
		t.boilerOn += elapsed
	}

	if rooms < 2 {
		return
	}

	t.spreadSum += (hi - lo) * secs
	t.spreadSpan += secs
	t.spreadMax = math.Max(t.spreadMax, hi-lo)
}

// record adds the seconds a device held its temperature to its stats.
func (t *tally) record(device string, v, secs float64) {
	d := t.stats[device]
	if d == nil {
		d = &DeviceSummary{Device: device, Min: v, Max: v}
		t.stats[device] = d
	}

	d.sum += v * secs
	d.span += secs
	d.Min = math.Min(d.Min, v)
	d.Max = math.Max(d.Max, v)
}

// summary returns what has been added up.
func (t *tally) summary() Summary {
	s := Summary{SpreadMax: t.spreadMax, BoilerOn: t.boilerOn, BoilerRuns: t.boilerRuns}

	if t.spreadSpan > 0 {
		s.SpreadMean = t.spreadSum / t.spreadSpan
	}

	for _, d := range t.stats {
		t.place(&s, d)
	}

	slices.SortFunc(s.Rooms, func(a, b DeviceSummary) int { return cmp.Compare(a.Device, b.Device) })

	return s
}

// place works out the mean of a device and puts it in the summary, as the outdoor device or a room.
func (t *tally) place(s *Summary, d *DeviceSummary) {
	d.Mean = d.sum / d.span

	if d.Device == t.outdoors {
		s.Outdoors = d

		return
	}

	s.Rooms = append(s.Rooms, *d)
}
