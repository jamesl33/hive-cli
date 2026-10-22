package dashboard

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jamesl33/hive-cli/internal/dashboard"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// Dashboard defines the struct for the dashboard command.
type Dashboard struct {
	options *z2m.Options
}

// NewDashboard creates a new command for displaying a live dashboard of the heating and sensors.
func NewDashboard(options *z2m.Options) *cobra.Command {
	d := Dashboard{options: options}

	cmd := cobra.Command{
		Short: "Show a live dashboard of the heating and sensors",
		Use:   "dashboard",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return d.Run(cmd.Context()) },
	}

	return &cmd
}

// Run displays the dashboard until it is closed.
func (d *Dashboard) Run(ctx context.Context) error {
	feed, err := z2m.Watch(ctx, d.options)
	if err != nil {
		return fmt.Errorf("failed to watch devices: %w", err)
	}

	defer feed.Close()

	model := dashboard.New(ctx, feed, dashboard.NewStore(feed.Receiver))

	final, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	if err != nil {
		return fmt.Errorf("failed to run dashboard: %w", err)
	}

	return final.(dashboard.Model).Err()
}
