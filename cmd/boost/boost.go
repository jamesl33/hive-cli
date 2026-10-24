package boost

import (
	"context"
	"fmt"

	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// defaultTemp is the temperature used for a boost when none is given.
const defaultTemp = 18.0

// Boost defines the struct for the boost command.
type Boost struct {
	options *z2m.Options
}

// NewBoost creates a new command for boosting the heating.
func NewBoost(options *z2m.Options) *cobra.Command {
	boost := Boost{options: options}

	cmd := cobra.Command{
		Short: "Boost the heating for a duration (e.g. 30, 45m, 1h30m; max 6h), default 18°C",
		Use:   "boost <duration> [temp]",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  func(cmd *cobra.Command, args []string) error { return boost.Run(cmd.Context(), args) },
	}

	return &cmd
}

// Run boosts the heating.
func (b *Boost) Run(ctx context.Context, args []string) error {
	mins, err := hive.ParseMinutes(args[0])
	if err != nil {
		return err
	}

	temp := defaultTemp

	if len(args) == 2 {
		temp, err = hive.ParseTemp(args[1])
		if err != nil {
			return err
		}
	}

	session, err := z2m.Open(ctx, b.options)
	if err != nil {
		return fmt.Errorf("failed to open session: %w", err)
	}
	defer session.Close()

	return session.Apply(ctx, hive.HeatBoost(session.Receiver, mins, temp))
}
