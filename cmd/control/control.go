package control

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jamesl33/hive-cli/internal/control"
	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// Control defines the struct for the control command.
type Control struct {
	options *z2m.Options
	cfg     control.Config
	dir     string
	restore bool
	rooms   []string
	observe []string
}

// NewControl creates a new command for closing the radiator valves of rooms which are warm.
func NewControl(options *z2m.Options) *cobra.Command {
	c := Control{options: options, cfg: control.DefaultConfig()}

	cmd := cobra.Command{
		Short: "Set the radiator valve of each room from its recorded temperatures, then exit",
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

	flags.StringVar(
		&c.dir,
		"history",
		z2m.Env("HIVE_HISTORY", history.DefaultDir()),
		"directory the history is recorded in (HIVE_HISTORY)",
	)

	flags.Float64Var(
		&c.cfg.Kp,
		"kp",
		c.cfg.Kp,
		"percent the valve closes by for each °C the room warms",
	)

	flags.Float64Var(
		&c.cfg.Lo,
		"lo",
		c.cfg.Lo,
		"°C below the target where the valve starts to close",
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

	return &cmd
}

// Run works out the valves from the recorded history once and writes the ones which need it.
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

	ctrl := control.New(c.cfg, rooms, feed)

	if c.restore {
		return ctrl.Restore(ctx)
	}

	now := time.Now()

	samples, err := history.Load(c.dir, now.Add(-c.cfg.Window))
	if err != nil {
		return fmt.Errorf("failed to load history: %w", err)
	}

	for _, s := range samples {
		ctrl.Add(s)
	}

	decisions, err := ctrl.Tick(ctx, now)

	for _, d := range decisions {
		logDecision(d)
	}

	return err
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
