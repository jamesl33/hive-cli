package status

import (
	"context"
	"fmt"

	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// Status defines the struct for the status command.
type Status struct {
	options *z2m.Options
}

// NewStatus creates a new command for displaying the state of the receiver.
func NewStatus(options *z2m.Options) *cobra.Command {
	status := Status{options: options}

	cmd := cobra.Command{
		Short: "Show the current state of the receiver",
		Use:   "status",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return status.Run(cmd.Context()) },
	}

	return &cmd
}

// Run displays the state of the receiver.
func (s *Status) Run(ctx context.Context) error {
	session, err := z2m.Open(ctx, s.options)
	if err != nil {
		return fmt.Errorf("failed to open session: %w", err)
	}

	defer session.Close()

	err = session.Read(ctx, hive.StatusKeys(session.Receiver))
	if err != nil {
		return fmt.Errorf("failed to read state: %w", err)
	}

	_, err = fmt.Println(hive.Interpret(session.Receiver, session.State).String())

	return err
}
