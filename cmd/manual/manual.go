package manual

import (
	"context"
	"fmt"

	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// Manual defines the struct for the manual command.
type Manual struct {
	options *z2m.Options
}

// NewManual creates a new command for holding a temperature.
func NewManual(options *z2m.Options) *cobra.Command {
	manual := Manual{options: options}

	cmd := cobra.Command{
		Short: "Hold a temperature until told otherwise",
		Use:   "manual <temp>",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return manual.Run(cmd.Context(), args[0]) },
	}

	return &cmd
}

// Run holds the given temperature.
func (m *Manual) Run(ctx context.Context, temp string) error {
	t, err := hive.ParseTemp(temp)
	if err != nil {
		return err
	}

	session, err := z2m.Open(ctx, m.options)
	if err != nil {
		return fmt.Errorf("failed to open session: %w", err)
	}

	defer session.Close()

	return session.Apply(ctx, hive.HeatManual(session.Receiver, t))
}
