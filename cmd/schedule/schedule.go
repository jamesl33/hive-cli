package schedule

import (
	"context"
	"fmt"

	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// Schedule defines the struct for the schedule command.
type Schedule struct {
	options *z2m.Options
}

// NewSchedule creates a new command for following the schedule stored on the thermostat.
func NewSchedule(options *z2m.Options) *cobra.Command {
	schedule := Schedule{options: options}

	cmd := cobra.Command{
		Short: "Follow the schedule stored on the thermostat",
		Use:   "schedule",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return schedule.Run(cmd.Context()) },
	}

	return &cmd
}

// Run switches the heating to the schedule.
func (s *Schedule) Run(ctx context.Context) error {
	session, err := z2m.Open(ctx, s.options)
	if err != nil {
		return fmt.Errorf("failed to open session: %w", err)
	}

	defer session.Close()

	return session.Apply(ctx, hive.HeatSchedule(session.Receiver))
}
