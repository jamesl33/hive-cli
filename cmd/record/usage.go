package record

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/jamesl33/hive-cli/internal/octopus"
)

const (
	// usageEvery is how often the smart meter readings are fetched. Octopus publishes them about a day late.
	usageEvery = time.Hour

	// usageBackfill is how far back the first fetch goes, within what the history keeps.
	usageBackfill = 28 * 24 * time.Hour

	// usageRefetch is how far back later fetches go, to pick up half hours which were published late.
	usageRefetch = 3 * 24 * time.Hour

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
		seen   map[int64]bool
		err    error
		loaded bool
	)

	t := time.NewTicker(usageEvery)
	defer t.Stop()

	for {
		if !loaded {
			seen, err = recordedUsage(dir, metric(c.Fuel))
			loaded = err == nil
		}

		if loaded {
			err = recordUsage(ctx, c, log, seen)
		}

		if err != nil && ctx.Err() == nil {
			slog.Error("usage", "fuel", c.Fuel, "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// recordedUsage returns the start of every half hour of the metric in the history, in seconds since the
// epoch, which unlike a time.Time compares equal whatever its location. It is for one meter, as each
// meter's use is its own metric.
func recordedUsage(dir string, m history.Metric) (map[int64]bool, error) {
	samples, err := history.Load(dir, time.Now().Add(-usageBackfill))
	if err != nil {
		return nil, fmt.Errorf("failed to load %s history: %w", m, err)
	}

	seen := make(map[int64]bool)

	for _, s := range samples {
		if s.Metric == m {
			seen[s.At.Unix()] = true
		}
	}

	return seen, nil
}

// usageFrom returns where to fetch from: a few days before the newest half hour in seen, as the API can
// publish a half hour after a later one, or the whole backfill if there is none. Half hours before it
// are dropped from seen, as they would never be fetched again.
func usageFrom(seen map[int64]bool, now time.Time) time.Time {
	from := now.Add(-usageBackfill)

	var newest int64

	for start := range seen {
		newest = max(newest, start)
	}

	refetch := time.Unix(newest, 0).Add(-usageRefetch)
	if newest != 0 && refetch.After(from) {
		from = refetch
	}

	for start := range seen {
		if start < from.Unix() {
			delete(seen, start)
		}
	}

	return from
}

// recordUsage adds the half hours which are not in seen to the log, and to seen.
func recordUsage(ctx context.Context, c *octopus.Client, log *history.Log, seen map[int64]bool) error {
	intervals, err := c.Consumption(ctx, usageFrom(seen, time.Now()))
	if err != nil {
		return err
	}

	m := metric(c.Fuel)

	for _, i := range intervals {
		if seen[i.Start.Unix()] {
			continue
		}

		seen[i.Start.Unix()] = true

		log.Add(history.Sample{Device: string(m), Metric: m, At: i.Start, Value: i.KWh})
	}

	return nil
}
