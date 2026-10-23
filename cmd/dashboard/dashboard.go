package dashboard

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jamesl33/hive-cli/internal/dashboard"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// Dashboard defines the struct for the dashboard command.
type Dashboard struct {
	options *z2m.Options
	dir     string

	outdoors string
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

	cmd.Flags().StringVar(
		&d.dir,
		"history",
		z2m.Env("HIVE_HISTORY", dashboard.DefaultDir()),
		"directory with the history written by 'hive record' (HIVE_HISTORY)",
	)

	cmd.Flags().StringVar(&d.outdoors, "outdoors", "outdoors", "friendly name of the outdoor sensor, which 'o' hides on the charts")

	return &cmd
}

// Run displays the dashboard until it is closed.
func (d *Dashboard) Run(ctx context.Context) error {
	feed, err := z2m.Watch(ctx, d.options)
	if err != nil {
		return fmt.Errorf("failed to watch devices: %w", err)
	}

	defer feed.Close()

	store := dashboard.NewStore(feed.Receiver)
	store.Outdoors = d.outdoors

	model := dashboard.New(ctx, feed, store)

	err = store.Restore(d.dir, model.WindowStart(time.Now()))
	if err != nil {
		return fmt.Errorf("failed to load history: %w", err)
	}

	final, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	if err != nil {
		return fmt.Errorf("failed to run dashboard: %w", err)
	}

	return final.(dashboard.Model).Err()
}
