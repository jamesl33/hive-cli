package summary

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jamesl33/hive-cli/internal/dashboard"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// dateLayout is how a day is written on the command line.
const dateLayout = "2006-01-02"

// outdoors is the friendly name of the outdoor sensor, which is left out of the spread.
const outdoors = "outdoors"

// Summary defines the struct for the summary command.
type Summary struct {
	dir string
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

	sum := dashboard.Summarize(samples, outdoors, day, end)

	write(w, sum, day, end)

	return nil
}

// write writes the summary.
func write(w io.Writer, sum dashboard.Summary, from, to time.Time) {
	var (
		bold = lipgloss.NewStyle().Bold(true)
		dim  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
		warm = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
		cool = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
		cell = lipgloss.NewStyle().Width(6).Align(lipgloss.Right)
	)

	devices := slices.Clone(sum.Rooms)
	if sum.Outdoors != nil {
		devices = append(devices, *sum.Outdoors)
	}

	width := 0
	for _, d := range devices {
		width = max(width, lipgloss.Width(d.Device))
	}

	name := lipgloss.NewStyle().Width(width + 2)

	fmt.Fprintf(w, "%s  %s\n\n", bold.Render(from.Format(dateLayout)), dim.Render(from.Format("15:04")+"–"+to.Format("15:04")))
	fmt.Fprintf(w, "%s%s%s%s\n", name.Render(""), cell.Inherit(dim).Render("mean"), cell.Inherit(dim).Render("min"), cell.Inherit(dim).Render("max"))

	// The warmest and coolest room by mean stand out
	var hot, cold string

	if len(sum.Rooms) > 1 {
		hot = slices.MaxFunc(sum.Rooms, func(a, b dashboard.DeviceSummary) int { return cmp.Compare(a.Mean, b.Mean) }).Device
		cold = slices.MinFunc(sum.Rooms, func(a, b dashboard.DeviceSummary) int { return cmp.Compare(a.Mean, b.Mean) }).Device
	}

	for _, d := range devices {
		style := lipgloss.NewStyle()

		switch d.Device {
		case hot:
			style = warm
		case cold:
			style = cool
		case outdoors:
			style = dim
		}

		// Each cell is styled on its own, as the reset after the bold mean would end the colour
		fmt.Fprintln(
			w,
			style.Inherit(name).Render(d.Device)+
				style.Inherit(cell).Bold(true).Render(fmt.Sprintf("%.1f", d.Mean))+
				style.Inherit(cell).Render(fmt.Sprintf("%.1f", d.Min))+
				style.Inherit(cell).Render(fmt.Sprintf("%.1f", d.Max)),
		)
	}

	mins := int(sum.BoilerOn.Round(time.Minute).Minutes())

	fmt.Fprintf(w, "\n%s %s %s\n", dim.Render("spread"), bold.Render(fmt.Sprintf("%.1f°C", sum.SpreadMean)), dim.Render(fmt.Sprintf("mean · %.1f°C max", sum.SpreadMax)))
	fmt.Fprintf(w, "%s %s %s\n", dim.Render("boiler"), bold.Render(fmt.Sprintf("%dh%02dm", mins/60, mins%60)), dim.Render(fmt.Sprintf("on · %d runs", sum.BoilerRuns)))
}
