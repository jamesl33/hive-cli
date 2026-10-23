package dashboard

import (
	"math"
	"slices"
	"time"
)

// DeviceSummary is the temperature of a device over a period.
type DeviceSummary struct {
	Device string
	Mean   float64
	Min    float64
	Max    float64

	sum  float64 // degrees × seconds
	span float64 // seconds
}

// Summary is a period of recorded history boiled down.
type Summary struct {
	// Rooms is the temperature of every device but the outdoor one, by name.
	Rooms []DeviceSummary

	// Outdoors is the outdoor device, if it has any history in the period.
	Outdoors *DeviceSummary

	// SpreadMean and SpreadMax are the difference between the warmest and coolest room, while at
	// least two have a temperature.
	SpreadMean float64
	SpreadMax  float64

	// BoilerOn is how long the boiler was firing, and BoilerRuns how many times it started.
	BoilerOn   time.Duration
	BoilerRuns int
}

// Summarize works out the temperature of each device, the spread between the rooms and the boiler use
// from the samples between from and to, oldest first. A series is held at its last value, so samples
// from before from set the starting values.
func Summarize(samples []Sample, outdoors string, from, to time.Time) Summary {
	var (
		sum       Summary
		spreadSum float64
		spreadDur float64
	)

	stats := make(map[string]*DeviceSummary)
	current := make(map[string]float64)
	boiler := BoilerOff
	prev := from

	// account adds the time from prev until the time, during which nothing changed.
	account := func(until time.Time) {
		secs := until.Sub(prev).Seconds()
		if secs <= 0 {
			return
		}

		prev = until
		lo, hi, rooms := math.Inf(1), math.Inf(-1), 0

		for device, v := range current {
			d := stats[device]
			if d == nil {
				d = &DeviceSummary{Device: device, Min: v, Max: v}
				stats[device] = d
			}

			d.sum += v * secs
			d.span += secs
			d.Min = math.Min(d.Min, v)
			d.Max = math.Max(d.Max, v)

			if device == outdoors {
				continue
			}

			lo, hi, rooms = math.Min(lo, v), math.Max(hi, v), rooms+1
		}

		if boiler == BoilerOn {
			sum.BoilerOn += time.Duration(secs * float64(time.Second))
		}

		if rooms < 2 {
			return
		}

		spreadSum += (hi - lo) * secs
		spreadDur += secs
		sum.SpreadMax = math.Max(sum.SpreadMax, hi-lo)
	}

	for _, s := range samples {
		if s.At.After(to) {
			break
		}

		account(s.At)

		switch s.Metric {
		case Temperature:
			current[s.Device] = s.Value
		case Boiler:
			if s.Value == BoilerOn && boiler != BoilerOn && !s.At.Before(from) {
				sum.BoilerRuns++
			}

			boiler = s.Value
		}
	}

	account(to)

	if spreadDur > 0 {
		sum.SpreadMean = spreadSum / spreadDur
	}

	for device, d := range stats {
		d.Mean = d.sum / d.span

		if device == outdoors {
			sum.Outdoors = d

			continue
		}

		sum.Rooms = append(sum.Rooms, *d)
	}

	slices.SortFunc(sum.Rooms, func(a, b DeviceSummary) int { return compareKeys(key{device: a.Device}, key{device: b.Device}) })

	return sum
}
