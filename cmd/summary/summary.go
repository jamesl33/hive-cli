package summary

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jamesl33/hive-cli/internal/dashboard"
	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// dateLayout is how a day is written on the command line.
const dateLayout = "2006-01-02"

var (
	// bold is the style of the values which matter most.
	bold = lipgloss.NewStyle().Bold(true)

	// dim is the style of labels and of the outdoor sensor.
	dim = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	// warm is the style of the warmest room.
	warm = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))

	// cool is the style of the coolest room.
	cool = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	// cell is the style of a number in the table.
	cell = lipgloss.NewStyle().Width(6).Align(lipgloss.Right)

	// statLabel is the style of the label of a footer line, as wide as the longest fuel name.
	statLabel = lipgloss.NewStyle().Width(len("electricity") + 1)

	// statValue is the style of the value of a footer line.
	statValue = lipgloss.NewStyle().Width(7).Align(lipgloss.Right)
)

// Summary defines the struct for the summary command.
type Summary struct {
	dir      string
	outdoors string
}

// NewSummary creates a new command for summarising recorded history.
func NewSummary() *cobra.Command {
	s := Summary{}

	cmd := cobra.Command{
		Short: "Summarise a day or range of days of recorded history: room temperatures, the spread between them and boiler use",
		Use:   "summary [FIRST-YYYY-MM-DD [LAST-YYYY-MM-DD]]",
		Args:  cobra.MaximumNArgs(2),
		RunE:  func(cmd *cobra.Command, args []string) error { return s.Run(cmd.OutOrStdout(), args) },
	}

	cmd.Flags().StringVar(
		&s.dir,
		"history",
		z2m.Env("HIVE_HISTORY", history.DefaultDir()),
		"directory history is recorded in (HIVE_HISTORY)",
	)

	cmd.Flags().StringVar(
		&s.outdoors,
		"outdoors",
		"tah_outdoors",
		"friendly name of the outdoor sensor, left out of the spread",
	)

	return &cmd
}

// Run prints the summary from the first day to the last, both inclusive and today unless given, in local
// time.
func (s *Summary) Run(w io.Writer, args []string) error {
	now := time.Now()
	y, m, d := now.Date()
	first := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	last := first

	for i, arg := range args {
		day, err := time.ParseInLocation(dateLayout, arg, now.Location())
		if err != nil {
			return fmt.Errorf("invalid date %q, want YYYY-MM-DD: %w", arg, err)
		}

		if i == 0 {
			first, last = day, day
		} else {
			last = day
		}
	}

	if last.Before(first) {
		return fmt.Errorf("last day %s is before first day %s", last.Format(dateLayout), first.Format(dateLayout))
	}

	end := last.AddDate(0, 0, 1)
	if end.After(now) {
		end = now
	}

	samples, err := history.Load(s.dir, first)
	if err != nil {
		return fmt.Errorf("failed to load history: %w", err)
	}

	if len(samples) == 0 {
		return fmt.Errorf("no history for %s in %s", first.Format(dateLayout), s.dir)
	}

	// Days before the history starts have nothing to summarise.
	requested := first

	y, m, d = samples[0].At.In(now.Location()).Date()
	if began := time.Date(y, m, d, 0, 0, 0, 0, now.Location()); began.After(first) {
		first = began
	}

	if !end.After(first) {
		return fmt.Errorf("no history for %s in %s", requested.Format(dateLayout), s.dir)
	}

	sum := dashboard.Summarize(samples, s.outdoors, first, end)

	write(w, sum, s.outdoors, first, end)

	return nil
}

// write writes the summary.
func write(w io.Writer, sum dashboard.Summary, outdoors string, from, to time.Time) {
	devices := slices.Clone(sum.Rooms)
	if sum.Outdoors != nil {
		devices = append(devices, *sum.Outdoors)
	}

	width := 0
	for _, d := range devices {
		width = max(width, lipgloss.Width(d.Device))
	}

	name := lipgloss.NewStyle().Width(width + 2)
	styles := rowStyles(sum, outdoors)

	writeHeader(w, name, from, to)

	for _, d := range devices {
		writeRow(w, name, styles[d.Device], d)
	}

	writeFooter(w, sum)
}

// rowStyles returns the style of the rows which stand out: the warmest and coolest room by mean, and the
// outdoor sensor. Every other row is left plain.
func rowStyles(sum dashboard.Summary, outdoors string) map[string]lipgloss.Style {
	styles := map[string]lipgloss.Style{outdoors: dim}
	if len(sum.Rooms) < 2 {
		return styles
	}

	byMean := func(a, b dashboard.DeviceSummary) int { return cmp.Compare(a.Mean, b.Mean) }
	hot := slices.MaxFunc(sum.Rooms, byMean)
	cold := slices.MinFunc(sum.Rooms, byMean)

	if hot.Mean == cold.Mean {
		return styles
	}

	styles[hot.Device] = warm
	styles[cold.Device] = cool

	return styles
}

// writeHeader writes the days, when they end if not whole and the column names.
func writeHeader(w io.Writer, name lipgloss.Style, from, to time.Time) {
	days := from.Format(dateLayout)

	// to is exclusive, so the last day is the one which holds the instant before it.
	last := to.Add(-time.Nanosecond).Format(dateLayout)
	if last != days {
		days += " – " + last
	}

	columns := dim.Inherit(cell)

	fmt.Fprint(w, bold.Render(days))

	// The time only says something when the period ends part way through a day.
	if to.Format("15:04:05") != "00:00:00" {
		fmt.Fprint(w, "  ", dim.Render("until "+to.Format("15:04")))
	}

	fmt.Fprintf(w, "\n\n%s%s%s%s\n", name.Render(""), columns.Render("mean"), columns.Render("min"), columns.Render("max"))
}

// writeRow writes the mean, min and max of a device in the style. Each cell is styled on its own, as the
// reset after the bold mean would end the colour.
func writeRow(w io.Writer, name, style lipgloss.Style, d dashboard.DeviceSummary) {
	label := style.Inherit(name).Render(d.Device)
	mean := style.Inherit(cell).Bold(true).Render(fmt.Sprintf("%.1f", d.Mean))
	lo := style.Inherit(cell).Render(fmt.Sprintf("%.1f", d.Min))
	hi := style.Inherit(cell).Render(fmt.Sprintf("%.1f", d.Max))

	row := label + mean + lo + hi

	fmt.Fprintln(w, row)
}

// writeFooter writes the spread between the rooms and the boiler use.
func writeFooter(w io.Writer, sum dashboard.Summary) {
	mins := int(sum.BoilerOn.Round(time.Minute).Minutes())

	fmt.Fprintln(w)
	writeStat(w, "spread", fmt.Sprintf("%.1f°C", sum.SpreadMean), fmt.Sprintf("mean · %.1f°C max", sum.SpreadMax))
	writeStat(w, "boiler", fmt.Sprintf("%dh%02dm", mins/60, mins%60), fmt.Sprintf("on · %d runs", sum.BoilerRuns))

	blank := true

	for _, fuel := range []history.Metric{history.Gas, history.Electricity} {
		kwh, ok := sum.Fuels[fuel]
		if !ok {
			continue
		}

		// Fuels are a group of their own, set apart by a blank line.
		if blank {
			fmt.Fprintln(w)

			blank = false
		}

		writeStat(w, string(fuel), fmt.Sprintf("%.1f", kwh), "kWh")
	}
}

// writeStat writes a line of the footer: a label, a value and its detail, each in a column of its own.
func writeStat(w io.Writer, label, value, detail string) {
	fmt.Fprintln(w, dim.Inherit(statLabel).Render(label)+bold.Inherit(statValue).Render(value)+" "+dim.Render(detail))
}
