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
	"golang.org/x/sync/errgroup"
)

// Control defines the struct for the control command.
type Control struct {
	options *z2m.Options
	cfg     control.Config
	tick    time.Duration
	restore bool
	rooms   []string
	observe []string
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

	flags.BoolVar(
		&c.restore,
		"restore",
		false,
		"open every valve fully and exit",
	)

	flags.DurationVar(
		&c.tick,
		"tick",
		150*time.Second,
		"how often to work out the valves",
	)

	flags.Float64Var(
		&c.cfg.Kp,
		"kp",
		c.cfg.Kp,
		"percent the valve closes by for each °C the room warms",
	)

	flags.Float64Var(
		&c.cfg.Min,
		"min",
		c.cfg.Min,
		"smallest valve opening in percent",
	)

	flags.DurationVar(
		&c.cfg.Lead,
		"lead",
		c.cfg.Lead,
		"how far ahead to look using the rate the room is warming at",
	)

	flags.DurationVar(
		&c.cfg.Window,
		"window",
		c.cfg.Window,
		"history which the rate is worked out from",
	)

	flags.DurationVar(
		&c.cfg.Stale,
		"stale",
		c.cfg.Stale,
		"silence after which a sensor's valve is opened fully",
	)

	flags.DurationVar(
		&c.cfg.WriteEvery,
		"write-every",
		c.cfg.WriteEvery,
		"shortest time between writes to one valve",
	)

	flags.Float64Var(
		&c.cfg.WriteMin,
		"write-min",
		c.cfg.WriteMin,
		"smallest change in percent worth a write",
	)

	// TODO(jamesl33): Extract the default rooms, they are only right for my house.
	flags.StringArrayVar(
		&c.rooms,
		"room",
		[]string{"office=tah_office:trv_office", "landing=tah_landing:trv_landing", "bedroom=tah_bedroom:trv_bedroom"},
		"room to control, as name=sensor:valve; repeat for each room",
	)

	// TODO(jamesl33): Extract the default observed rooms, they are only right for my house.
	flags.StringArrayVar(
		&c.observe,
		"observe",
		[]string{"lounge=tah_lounge:trv_kitchen", "stairs=tah_stairs:trv_stairs"},
		"room to log what would be done for but never write, as name=sensor:valve",
	)

	return &cmd
}

// Run controls the valves until the context is cancelled. It leaves them as they are, as a restart
// would only open them to put them back; the service opens them with --restore when it stops.
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

	return c.loop(ctx, feed, ctrl)
}

// loop reads the feed and works out the valves every tick until the context is cancelled or the feed
// fails.
func (c *Control) loop(ctx context.Context, feed *z2m.Feed, ctrl *control.Controller) error {
	updates := make(chan z2m.Update)

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error { return forward(ctx, feed, updates) })
	g.Go(func() error { return c.process(ctx, feed, ctrl, updates) })

	return g.Wait()
}

// forward sends the reports of the feed on until the context is cancelled.
func forward(ctx context.Context, feed *z2m.Feed, updates chan<- z2m.Update) error {
	for {
		u, err := feed.Next(ctx)
		if ctx.Err() != nil {
			return nil
		}

		if err != nil {
			return err
		}

		select {
		case updates <- u:
		case <-ctx.Done():
			return nil
		}
	}
}

// process feeds the reports to the controller and works out the valves every tick until the context is
// cancelled.
func (c *Control) process(ctx context.Context, feed *z2m.Feed, ctrl *control.Controller, updates <-chan z2m.Update) error {
	reports := dashboard.NewReports(feed.Receiver)

	t := time.NewTicker(c.tick)
	defer t.Stop()

	// The first tick is as soon as the first readings are in, not a whole interval after starting
	ticked := false

	for {
		select {
		case <-ctx.Done():
			return nil
		case u := <-updates:
			take(ctrl, reports, u)

			if !ticked && ctrl.Ready() {
				ticked = true

				t.Reset(c.tick)
				runTick(ctx, ctrl, time.Now())
			}
		case now := <-t.C:
			ticked = true

			runTick(ctx, ctrl, now)
		}
	}
}

// take gives the controller the measurements in a report, and the opening if it is from a valve.
func take(ctrl *control.Controller, reports *dashboard.Reports, u z2m.Update) {
	for _, s := range reports.Samples(u) {
		ctrl.Add(s)
	}

	open, ok := u.Payload["heating_valve_position"].(float64)
	if ok {
		ctrl.SetOpening(u.Device, open)
	}
}

// runTick works out the valves and logs what was decided for each room.
func runTick(ctx context.Context, ctrl *control.Controller, now time.Time) {
	decisions, err := ctrl.Tick(ctx, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	for _, d := range decisions {
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
}

// parseRooms returns the rooms to control and observe.
func (c *Control) parseRooms() ([]control.Room, error) {
	rooms, err := parseSpecs(c.rooms, false)
	if err != nil {
		return nil, err
	}

	observed, err := parseSpecs(c.observe, true)
	if err != nil {
		return nil, err
	}

	return append(rooms, observed...), nil
}

// parseSpecs parses rooms given as name=sensor:valve, which are only observed if observe is set.
func parseSpecs(specs []string, observe bool) ([]control.Room, error) {
	var rooms []control.Room

	for _, spec := range specs {
		r, err := parseRoom(spec)
		if err != nil {
			return nil, err
		}

		r.Observe = observe

		rooms = append(rooms, r)
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
