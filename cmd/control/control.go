package control

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jamesl33/hive-cli/internal/control"
	"github.com/jamesl33/hive-cli/internal/dashboard"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// restoreTimeout is how long to wait to open the valves when stopping.
const restoreTimeout = 15 * time.Second

// Control defines the struct for the control command.
type Control struct {
	options *z2m.Options
	cfg     control.Config
	tick    time.Duration
	restore bool
	rooms   []string
	observe []string
	offsets []string
}

// NewControl creates a new command for closing the radiator valves of rooms which are warm.
func NewControl(options *z2m.Options) *cobra.Command {
	c := Control{options: options, cfg: control.DefaultConfig()}

	cmd := cobra.Command{
		Short: "Close the radiator valve of each room as it warms past the target",
		Use:   "control",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return c.Run(cmd.Context()) },
	}

	flags := cmd.Flags()

	flags.BoolVar(&c.restore, "restore", false, "open every valve fully and exit")
	flags.DurationVar(&c.tick, "tick", 5*time.Minute, "how often to work out the valves")
	flags.Float64Var(&c.cfg.Kp, "kp", c.cfg.Kp, "percent the valve closes by for each °C the room warms")
	flags.Float64Var(&c.cfg.Lo, "lo", c.cfg.Lo, "°C below the target where the valve starts to close")
	flags.Float64Var(&c.cfg.Min, "min", c.cfg.Min, "smallest valve opening in percent")
	flags.DurationVar(&c.cfg.Lead, "lead", c.cfg.Lead, "how far ahead to look using the rate the room is warming at")
	flags.DurationVar(&c.cfg.Window, "window", c.cfg.Window, "history which the rate is worked out from")
	flags.DurationVar(&c.cfg.Stale, "stale", c.cfg.Stale, "silence after which a sensor's valve is opened fully")
	flags.DurationVar(&c.cfg.WriteEvery, "write-every", c.cfg.WriteEvery, "shortest time between writes to one valve")
	flags.Float64Var(&c.cfg.WriteMin, "write-min", c.cfg.WriteMin, "smallest change in percent worth a write")

	flags.StringArrayVar(
		&c.rooms,
		"room",
		[]string{"office=tah_office:trv_office", "landing=tah_landing:trv_landing", "bedroom=tah_bedroom:trv_bedroom"},
		"room to control, as name=sensor:valve; repeat for each room",
	)

	flags.StringArrayVar(
		&c.observe,
		"observe",
		[]string{"lounge=tah_lounge:trv_kitchen", "stairs=tah_stairs:trv_stairs"},
		"room to log what would be done for but never write, as name=sensor:valve",
	)

	flags.StringArrayVar(&c.offsets, "offset", nil, "°C to hold a room above the thermostat target, as name=°C")

	return &cmd
}

// Run controls the valves until the context is cancelled.
func (c *Control) Run(ctx context.Context) error {
	rooms, err := c.parseRooms()
	if err != nil {
		return err
	}

	feed, err := z2m.Watch(ctx, c.options)
	if err != nil {
		return fmt.Errorf("failed to watch devices: %w", err)
	}
	defer feed.Close()

	if c.restore {
		return control.New(c.cfg, rooms, feed).Restore(ctx)
	}

	ctrl := control.New(c.cfg, rooms, feed)

	err = c.loop(ctx, feed, ctrl)

	stop, cancel := context.WithTimeout(context.Background(), restoreTimeout)
	defer cancel()

	return joinErr(err, ctrl.Restore(stop))
}

// loop feeds the reports to the controller and works out the valves every tick until the context is
// cancelled.
func (c *Control) loop(ctx context.Context, feed *z2m.Feed, ctrl *control.Controller) error {
	updates := make(chan z2m.Update)
	failed := make(chan error, 1)

	go func() {
		for {
			u, err := feed.Next(ctx)
			if err != nil {
				failed <- err

				return
			}

			select {
			case updates <- u:
			case <-ctx.Done():
				return
			}
		}
	}()

	reports := dashboard.NewReports(feed.Receiver)

	t := time.NewTicker(c.tick)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-failed:
			if ctx.Err() != nil {
				return nil
			}

			return err
		case u := <-updates:
			for _, s := range reports.Samples(u) {
				ctrl.Add(s)
			}

			open, ok := u.Payload["heating_valve_position"].(float64)
			if ok {
				ctrl.SetOpening(u.Device, open)
			}
		case now := <-t.C:
			decisions, err := ctrl.Tick(ctx, now)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}

			for _, d := range decisions {
				logDecision(d)
			}
		}
	}
}

// logDecision logs what the controller did for a room.
func logDecision(d control.Decision) {
	slog.Info(
		"tick",
		"room", d.Room,
		"temp", d.Temperature,
		"rate", d.Rate,
		"predicted", d.Predicted,
		"target", d.Target,
		"opening", d.Opening,
		"current", d.Current,
		"stale", d.Stale,
		"written", d.Written,
		"reason", d.Reason,
	)
}

// parseRooms returns the rooms to control and observe.
func (c *Control) parseRooms() ([]control.Room, error) {
	offsets := make(map[string]float64, len(c.offsets))

	for _, o := range c.offsets {
		name, value, ok := strings.Cut(o, "=")
		if !ok {
			return nil, fmt.Errorf("offset %q is not name=°C", o)
		}

		var v float64

		_, err := fmt.Sscanf(value, "%g", &v)
		if err != nil {
			return nil, fmt.Errorf("offset %q is not name=°C: %w", o, err)
		}

		offsets[name] = v
	}

	var rooms []control.Room

	for _, group := range []struct {
		specs   []string
		observe bool
	}{{c.rooms, false}, {c.observe, true}} {
		for _, spec := range group.specs {
			r, err := parseRoom(spec)
			if err != nil {
				return nil, err
			}

			r.Observe = group.observe
			r.Offset = offsets[r.Name]

			rooms = append(rooms, r)
		}
	}

	return rooms, nil
}

// parseRoom parses a room given as name=sensor:valve.
func parseRoom(spec string) (control.Room, error) {
	name, devices, ok := strings.Cut(spec, "=")
	sensor, valve, ok2 := strings.Cut(devices, ":")

	if !ok || !ok2 || name == "" || sensor == "" || valve == "" {
		return control.Room{}, fmt.Errorf("room %q is not name=sensor:valve", spec)
	}

	r := control.Room{Name: name, Sensor: sensor, Valve: valve}

	return r, nil
}

// joinErr returns the first error, as the second is only a failure to tidy up after it.
func joinErr(err, other error) error {
	if err != nil {
		return err
	}

	return other
}
