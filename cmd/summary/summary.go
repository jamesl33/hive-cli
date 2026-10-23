package summary

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/jamesl33/hive-cli/internal/dashboard"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// dateLayout is how a day is written on the command line.
const dateLayout = "2006-01-02"

// Summary defines the struct for the summary command.
type Summary struct {
	dir      string
	outdoors string
}

// NewSummary creates a new command for summarising a day of recorded history.
func NewSummary() *cobra.Command {
	s := Summary{}

	cmd := cobra.Command{
		Short: "Summarise a day of recorded history: room temperatures, the spread between them and boiler use",
		Use:   "summary [YYYY-MM-DD]",
		Args:  cobra.MaximumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return s.Run(cmd.OutOrStdout(), args) },
	}

	cmd.Flags().StringVar(
		&s.dir,
		"history",
		z2m.Env("HIVE_HISTORY", dashboard.DefaultDir()),
		"directory history is recorded in (HIVE_HISTORY)",
	)

	cmd.Flags().StringVar(&s.outdoors, "outdoors", "outdoors", "friendly name of the outdoor sensor, left out of the spread")

	return &cmd
}

// Run prints the summary of the day, today unless given, in local time.
func (s *Summary) Run(w io.Writer, args []string) error {
	now := time.Now()
	y, m, d := now.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, now.Location())

	if len(args) == 1 {
		var err error

		day, err = time.ParseInLocation(dateLayout, args[0], now.Location())
		if err != nil {
			return fmt.Errorf("invalid date %q, want YYYY-MM-DD: %w", args[0], err)
		}
	}

	end := day.AddDate(0, 0, 1)
	if end.After(now) {
		end = now
	}

	samples, err := dashboard.Load(s.dir, day)
	if err != nil {
		return fmt.Errorf("failed to load history: %w", err)
	}

	if len(samples) == 0 || !end.After(day) {
		return fmt.Errorf("no history for %s in %s", day.Format(dateLayout), s.dir)
	}

	sum := dashboard.Summarize(samples, s.outdoors, day, end)

	return write(w, sum, day, end)
}

// write writes the summary.
func write(w io.Writer, sum dashboard.Summary, from, to time.Time) error {
	fmt.Fprintf(w, "%s %s-%s\n\n", from.Format(dateLayout), from.Format("15:04"), to.Format("15:04"))

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	fmt.Fprintln(tw, "\tmean\tmin\tmax")

	for _, d := range sum.Rooms {
		fmt.Fprintf(tw, "%s\t%.1f\t%.1f\t%.1f\n", d.Device, d.Mean, d.Min, d.Max)
	}

	if sum.Outdoors != nil {
		d := sum.Outdoors
		fmt.Fprintf(tw, "%s\t%.1f\t%.1f\t%.1f\n", d.Device, d.Mean, d.Min, d.Max)
	}

	err := tw.Flush()
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "\nspread between rooms: mean %.1f°C, max %.1f°C\n", sum.SpreadMean, sum.SpreadMax)
	mins := int(sum.BoilerOn.Round(time.Minute).Minutes())
	fmt.Fprintf(w, "boiler: on %dh%02dm, %d runs\n", mins/60, mins%60, sum.BoilerRuns)

	return nil
}
