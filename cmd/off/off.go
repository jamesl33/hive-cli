package off

import (
	"context"
	"fmt"

	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// Off defines the struct for the off command.
type Off struct {
	options *z2m.Options
}

// NewOff creates a new command for turning the heating off.
func NewOff(options *z2m.Options) *cobra.Command {
	off := Off{options: options}

	cmd := cobra.Command{
		Short: "Turn the heating off (frost protection still applies)",
		Use:   "off",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return off.Run(cmd.Context()) },
	}

	return &cmd
}

// Run turns the heating off.
func (o *Off) Run(ctx context.Context) error {
	session, err := z2m.Open(ctx, o.options)
	if err != nil {
		return fmt.Errorf("failed to open session: %w", err)
	}

	defer session.Close()

	return session.Apply(ctx, hive.HeatOff(session.Receiver))
}
