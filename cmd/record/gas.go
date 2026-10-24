package record

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jamesl33/hive-cli/internal/dashboard"
	"github.com/jamesl33/hive-cli/internal/octopus"
)

const (
	// gasEvery is how often the smart meter readings are fetched. Octopus publishes them about a day late.
	gasEvery = time.Hour

	// gasBackfill is how far back the first fetch goes, within what the history keeps.
	gasBackfill = 28 * 24 * time.Hour

	// gasDevice is the name gas readings are recorded under.
	gasDevice = "gas"
)

// gasOptions defines the options for recording the gas used, which is skipped if none are set.
type gasOptions struct {
	mprn   string
	serial string
	units  string
	cv     float64
}

// client returns the Octopus client, or nil if gas isn't being recorded.
func (o gasOptions) client(key string) (*octopus.Client, error) {
	if key == "" && o.mprn == "" && o.serial == "" {
		return nil, nil
	}

	if key == "" || o.mprn == "" || o.serial == "" {
		return nil, fmt.Errorf("OCTOPUS_API_KEY, --octopus-mprn and --octopus-serial must all be set")
	}

	units := octopus.Units(o.units)
	if units != octopus.M3 && units != octopus.KWh {
		return nil, fmt.Errorf("unknown gas units %q, expected %s or %s", o.units, octopus.M3, octopus.KWh)
	}

	c := octopus.Client{
		BaseURL:        octopus.BaseURL,
		Key:            key,
		MPRN:           o.mprn,
		Serial:         o.serial,
		Units:          units,
		CalorificValue: o.cv,
	}

	return &c, nil
}

// gasLoop records the gas used until the context is cancelled. A failed fetch is retried at the next one.
func gasLoop(ctx context.Context, c *octopus.Client, log *dashboard.Log, dir string) {
	after, err := latestGas(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	t := time.NewTicker(gasEvery)
	defer t.Stop()

	for {
		after, err = recordGas(ctx, c, log, after)
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

// latestGas returns the start of the newest half hour in the history, or the zero time if there is none.
func latestGas(dir string) (time.Time, error) {
	samples, err := dashboard.Load(dir, time.Now().Add(-gasBackfill))
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to load gas history: %w", err)
	}

	var latest time.Time

	for _, s := range samples {
		if s.Metric == dashboard.Gas {
			latest = s.At
		}
	}

	return latest, nil
}

// recordGas adds the half hours after the time to the log, returning the start of the newest.
func recordGas(ctx context.Context, c *octopus.Client, log *dashboard.Log, after time.Time) (time.Time, error) {
	from := after
	if from.IsZero() {
		from = time.Now().Add(-gasBackfill)
	}

	intervals, err := c.Consumption(ctx, from)
	if err != nil {
		return after, err
	}

	for _, i := range intervals {
		if !i.Start.After(after) {
			continue
		}

		log.Add(dashboard.Sample{Device: gasDevice, Metric: dashboard.Gas, At: i.Start, Value: i.KWh})

		after = i.Start
	}

	return after, nil
}
