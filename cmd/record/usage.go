package record

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/jamesl33/hive-cli/internal/octopus"
)

const (
	// usageEvery is how often the smart meter readings are fetched. Octopus publishes them about a day late.
	usageEvery = time.Hour

	// usageBackfill is how far back the first fetch goes, within what the history keeps.
	usageBackfill = 28 * 24 * time.Hour

	// gasCalorificValue is the MJ/m³ of the gas, used to convert the volume a SMETS2 meter reports to
	// energy.
	gasCalorificValue = 39.5
)

// meterOptions defines the options for recording the energy used by one meter, which is skipped if none
// are set.
type meterOptions struct {
	fuel   octopus.Fuel
	point  string
	serial string
}

// metric returns the metric the meter's use is recorded as, which is named for the fuel. It is also the
// device.
func metric(fuel octopus.Fuel) history.Metric {
	return history.Metric(fuel)
}

// client returns the Octopus client, or nil if the meter isn't being recorded.
func (o meterOptions) client(key string) (*octopus.Client, error) {
	if o.point == "" && o.serial == "" {
		return nil, nil
	}

	if key == "" || o.point == "" || o.serial == "" {
		return nil, fmt.Errorf("OCTOPUS_API_KEY and the %s meter point and serial must all be set", o.fuel)
	}

	c := octopus.Client{
		BaseURL: octopus.BaseURL,
		Key:     key,
		Fuel:    o.fuel,
		Point:   o.point,
		Serial:  o.serial,
	}

	// Electricity meters report energy already
	if o.fuel == octopus.FuelGas {
		c.Units = octopus.UnitsM3
		c.CalorificValue = gasCalorificValue
	}

	return &c, nil
}

// usageLoop records the energy used until the context is cancelled. A failed load of the history or
// fetch is retried at the next one, and nothing is fetched until the history has loaded, so that half
// hours already recorded aren't added again.
func usageLoop(ctx context.Context, c *octopus.Client, log *history.Log, dir string) {
	var (
		after  time.Time
		err    error
		loaded bool
	)

	t := time.NewTicker(usageEvery)
	defer t.Stop()

	for {
		if !loaded {
			after, err = latestUsage(dir, metric(c.Fuel))
			loaded = err == nil
		}

		if loaded {
			after, err = recordUsage(ctx, c, log, after)
		}

		if err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, err)
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// latestUsage returns the start of the newest half hour of the metric in the history, or the zero time
// if there is none.
func latestUsage(dir string, m history.Metric) (time.Time, error) {
	samples, err := history.Load(dir, time.Now().Add(-usageBackfill))
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to load %s history: %w", m, err)
	}

	var latest time.Time

	for _, s := range samples {
		if s.Metric == m {
			latest = s.At
		}
	}

	return latest, nil
}

// latest returns the later of the two times.
func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}

// recordUsage adds the half hours after the time to the log, returning the start of the newest.
func recordUsage(ctx context.Context, c *octopus.Client, log *history.Log, after time.Time) (time.Time, error) {
	from := after
	if from.IsZero() {
		from = time.Now().Add(-usageBackfill)
	}

	intervals, err := c.Consumption(ctx, from)
	if err != nil {
		return after, err
	}

	m := metric(c.Fuel)
	newest := after

	for _, i := range intervals {
		if !i.Start.After(after) {
			continue
		}

		log.Add(history.Sample{Device: string(m), Metric: m, At: i.Start, Value: i.KWh})

		newest = latest(newest, i.Start)
	}

	return newest, nil
}
