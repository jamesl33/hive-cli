package record

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jamesl33/hive-cli/internal/dashboard"
	"github.com/jamesl33/hive-cli/internal/octopus"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// flushEvery is how often the measurements held in memory are written to disk. A power cut loses at
// most this much history.
const flushEvery = 10 * time.Minute

// Record defines the struct for the record command.
type Record struct {
	options *z2m.Options
	dir     string
	gas     gasOptions
}

// NewRecord creates a new command for recording the history shown by the dashboard.
func NewRecord(options *z2m.Options) *cobra.Command {
	r := Record{options: options}

	cmd := cobra.Command{
		Short: "Record the history of the sensors and heating until stopped",
		Use:   "record",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return r.Run(cmd.Context()) },
	}

	cmd.Flags().StringVar(
		&r.dir,
		"history",
		z2m.Env("HIVE_HISTORY", dashboard.DefaultDir()),
		"directory to record history in (HIVE_HISTORY)",
	)

	cmd.Flags().StringVar(
		&r.gas.mprn,
		"octopus-mprn",
		z2m.Env("OCTOPUS_MPRN", ""),
		"gas meter point number, to record the gas used; also needs OCTOPUS_API_KEY (OCTOPUS_MPRN)",
	)

	cmd.Flags().StringVar(
		&r.gas.serial,
		"octopus-serial",
		z2m.Env("OCTOPUS_GAS_SERIAL", ""),
		"gas meter serial number (OCTOPUS_GAS_SERIAL)",
	)

	cmd.Flags().StringVar(
		&r.gas.units,
		"gas-units",
		z2m.Env("OCTOPUS_GAS_UNITS", string(octopus.M3)),
		"what the gas meter reports: m3 for SMETS2 or kwh for SMETS1 (OCTOPUS_GAS_UNITS)",
	)

	cmd.Flags().Float64Var(&r.gas.cv, "gas-cv", 39.5, "calorific value in MJ/m³ from the gas bill, used when the meter reports m3")

	return &cmd
}

// Run records history until the context is cancelled.
func (r *Record) Run(ctx context.Context) error {
	log, err := dashboard.OpenLog(r.dir)
	if err != nil {
		return fmt.Errorf("failed to open history: %w", err)
	}

	// The key is only read from the environment, as a flag is visible to every user in the process list
	gas, err := r.gas.client(os.Getenv("OCTOPUS_API_KEY"))
	if err != nil {
		return err
	}

	feed, err := z2m.Watch(ctx, r.options)
	if err != nil {
		return fmt.Errorf("failed to watch devices: %w", err)
	}

	defer feed.Close()

	ctx, stop := context.WithCancel(ctx)
	defer stop()

	var wg sync.WaitGroup

	wg.Go(func() { flushLoop(ctx, log) })

	if gas != nil {
		wg.Go(func() { gasLoop(ctx, gas, log, r.dir) })
	}

	err = consume(ctx, feed, log)

	stop()
	wg.Wait()

	return errors.Join(err, log.Flush(time.Now()))
}

// consume adds the measurements in the feed to the log until the context is cancelled or the feed fails.
func consume(ctx context.Context, feed *z2m.Feed, log *dashboard.Log) error {
	reports := dashboard.NewReports(feed.Receiver)

	for {
		u, err := feed.Next(ctx)
		if ctx.Err() != nil {
			return nil
		}

		if err != nil {
			return err
		}

		for _, s := range reports.Samples(u) {
			log.Add(s)
		}
	}
}

// flushLoop writes the history to disk regularly, and on SIGUSR1, until the context is cancelled.
func flushLoop(ctx context.Context, log *dashboard.Log) {
	t := time.NewTicker(flushEvery)
	defer t.Stop()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGUSR1)

	defer signal.Stop(sigs)

	for {
		var now time.Time

		select {
		case <-ctx.Done():
			return
		case now = <-t.C:
		case <-sigs:
			now = time.Now()
		}

		err := log.Flush(now)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
}
