package analyze

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jamesl33/hive-cli/internal/analyze"
	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

const (
	// dateLayout is how a day is written on the command line.
	dateLayout = "2006-01-02"

	// shortRun is how the length of a short run is written.
	shortRun = "5m"

	// barWidth is the width of the longest bar of a histogram.
	barWidth = 30

	// maxTopUps is how many top-up times are listed before the rest are left out.
	maxTopUps = 6
)

var (
	// bold is the style of the values which matter most.
	bold = lipgloss.NewStyle().Bold(true)

	// dim is the style of labels and details.
	dim = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	// warn is the style of a measurement worth watching.
	warn = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))

	// bad is the style of a measurement which points to a fault.
	bad = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	// statLabel is the style of the label of a check, as wide as the longest.
	statLabel = lipgloss.NewStyle().Width(len("setback top-ups") + 1)

	// statValue is the style of the value of a check.
	statValue = lipgloss.NewStyle().Width(8).Align(lipgloss.Right)

	// statGrade is the style of the grade of a check, as wide as the longest.
	statGrade = lipgloss.NewStyle().Width(len("warn"))

	// barLabel is the style of the label of a bar.
	barLabel = lipgloss.NewStyle().Width(len("10–20m") + 1)
)

// Analyze defines the struct for the analyze command.
type Analyze struct {
	// dir is the directory the history is recorded in.
	dir string

	// receiver is the friendly name of the Hive receiver, which reports the boiler, target and room
	// temperature.
	receiver string

	// outdoors is the friendly name of the outdoor sensor.
	outdoors string
}

// NewAnalyze creates a new command which flags an unhealthy boiler setup in recorded history.
func NewAnalyze() *cobra.Command {
	a := Analyze{}

	cmd := cobra.Command{
		Short: "Flag signs of an unhealthy boiler setup in recorded history: short cycling, high duty, setback top-ups and overshoot",
		Use:   "analyze [FIRST-YYYY-MM-DD [LAST-YYYY-MM-DD]]",
		Args:  cobra.MaximumNArgs(2),
		RunE:  func(cmd *cobra.Command, args []string) error { return a.Run(cmd.OutOrStdout(), args) },
	}

	cmd.Flags().StringVar(
		&a.dir,
		"history",
		z2m.Env("HIVE_HISTORY", history.DefaultDir()),
		"directory history is recorded in (HIVE_HISTORY)",
	)

	cmd.Flags().StringVar(
		&a.receiver,
		"receiver",
		"hive_receiver",
		"friendly name of the Hive receiver, which reports the boiler, target and room temperature",
	)

	cmd.Flags().StringVar(
		&a.outdoors,
		"outdoors",
		"tah_outdoors",
		"friendly name of the outdoor sensor",
	)

	return &cmd
}

// Run prints the analysis from the first day to the last, both inclusive and today unless given, in local
// time.
func (a *Analyze) Run(w io.Writer, args []string) error {
	now := time.Now()

	first, last, err := days(now, args)
	if err != nil {
		return err // Purposefully not wrapped
	}

	end := last.AddDate(0, 0, 1)
	if end.After(now) {
		end = now
	}

	samples, err := history.Load(a.dir, first)
	if err != nil {
		return fmt.Errorf("failed to load history: %w", err)
	}

	if len(samples) == 0 {
		return fmt.Errorf("no history for %s in %s", first.Format(dateLayout), a.dir)
	}

	// Days before the history starts have nothing to analyse.
	requested := first

	if began := samples[0].At; began.After(first) {
		first = began
	}

	if !end.After(first) {
		return fmt.Errorf("no history for %s in %s", requested.Format(dateLayout), a.dir)
	}

	analysis := analyze.Analyze(samples, a.receiver, a.outdoors, first, end)

	write(w, analysis, first, end)

	return nil
}

// days returns the first and last day from the arguments, or today if there are none.
func days(now time.Time, args []string) (time.Time, time.Time, error) {
	y, m, d := now.Date()
	first := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	last := first

	for i, arg := range args {
		day, err := parseDay(arg, now.Location())
		if err != nil {
			return first, last, err // Purposefully not wrapped
		}

		last = day
		if i == 0 {
			first = day
		}
	}

	if last.Before(first) {
		return first, last, fmt.Errorf("last day %s is before first day %s", last.Format(dateLayout), first.Format(dateLayout))
	}

	return first, last, nil
}

// parseDay returns the day written in the argument, in the location.
func parseDay(arg string, loc *time.Location) (time.Time, error) {
	day, err := time.ParseInLocation(dateLayout, arg, loc)
	if err != nil {
		return day, fmt.Errorf("invalid date %q, want YYYY-MM-DD: %w", arg, err)
	}

	return day, nil
}

// write writes the analysis: a line per check, then a histogram of run lengths, starts by hour and duty
// by outdoor temperature.
func write(w io.Writer, a analyze.Analysis, from, to time.Time) {
	writeHeader(w, from, to)
	writeChecks(w, a, from.Location())

	writeHistogram(w, "run length", a.RunLengths, "")
	writeHistogram(w, "starts by hour", a.StartsByHour, "")
	writeHistogram(w, "duty by outdoors", a.DutyByOutdoors, "%")
}

// writeHeader writes the days and when they end if not whole.
func writeHeader(w io.Writer, from, to time.Time) {
	days := from.Format(dateLayout)

	// to is exclusive, so the last day is the one which holds the instant before it.
	last := to.Add(-time.Nanosecond).Format(dateLayout)
	if last != days {
		days += " – " + last
	}

	fmt.Fprint(w, bold.Render(days))

	// The time only says something when the period ends part way through a day.
	if to.Format("15:04:05") != "00:00:00" {
		fmt.Fprint(w, "  ", dim.Render("until "+to.Format("15:04")))
	}

	fmt.Fprint(w, "\n\n")
}

// writeChecks writes a line for each measurement of the analysis, in the location.
func writeChecks(w io.Writer, a analyze.Analysis, loc *time.Location) {
	duty := fmt.Sprintf("%s overall, outdoors unknown", percent(a.Duty))
	if a.MildKnown {
		duty = fmt.Sprintf("above %.0f°C · %s overall", analyze.MildTemp, percent(a.Duty))
	}

	writeCheck(w, "runs", fmt.Sprint(a.Starts), nil, "started")
	writeCheck(w, "median run", a.MedianRun.Round(time.Second).String(), status(a.MedianStatus()), "")
	writeCheck(w, "short runs", percent(a.ShortShare), status(a.ShortStatus()), "under "+shortRun)
	writeCheck(w, "starts/hour", fmt.Sprint(a.WorstHour), status(a.StartsStatus()), fmt.Sprintf("worst hour · %.1f mean", a.StartsPerHour))
	writeCheck(w, "duty", percent(a.GradedDuty()), status(a.DutyStatus()), duty)
	writeCheck(w, "setback top-ups", fmt.Sprint(len(a.TopUps)), status(a.TopUpStatus()), topUps(a.TopUps, loc))
	writeCheck(w, "overshoot", fmt.Sprintf("+%.1f", a.Overshoot), status(a.OvershootStatus()), "°C over target when heated")
}

// writeCheck writes a line of the analysis: a label, a value, its grade and its detail, each in a column of
// its own. A check with no grade leaves the column empty.
func writeCheck(w io.Writer, label, value string, grade *gradeStyle, detail string) {
	g := statGrade.Render("")
	if grade != nil {
		g = grade.style.Inherit(statGrade).Render(grade.word)
	}

	fmt.Fprintln(w, dim.Inherit(statLabel).Render(label)+bold.Inherit(statValue).Render(value)+"  "+g+" "+dim.Render(detail))
}

// gradeStyle is how a grade is written.
type gradeStyle struct {
	word  string
	style lipgloss.Style
}

// status returns how to write a grade.
func status(s analyze.Status) *gradeStyle {
	switch s {
	case analyze.StatusBad:
		g := gradeStyle{word: "bad", style: bad}

		return &g
	case analyze.StatusWarn:
		g := gradeStyle{word: "warn", style: warn}

		return &g
	}

	g := gradeStyle{word: "ok", style: lipgloss.NewStyle()}

	return &g
}

// percent writes a share as a whole percent.
func percent(share float64) string { return fmt.Sprintf("%.0f%%", 100*share) }

// topUps lists when the top-ups started, up to maxTopUps.
func topUps(starts []time.Time, loc *time.Location) string {
	var times []string

	for _, s := range starts[:min(len(starts), maxTopUps)] {
		times = append(times, s.In(loc).Format("01-02 15:04"))
	}

	list := strings.Join(times, " ")
	if len(starts) > maxTopUps {
		list += fmt.Sprintf(" +%d", len(starts)-maxTopUps)
	}

	return list
}

// writeHistogram writes a title and a bar for each bucket, scaled to the largest and followed by its value
// and the unit.
func writeHistogram(w io.Writer, title string, buckets []analyze.Bucket, unit string) {
	largest := 0.0
	for _, b := range buckets {
		largest = max(largest, b.Value)
	}

	fmt.Fprintf(w, "\n%s\n", dim.Render(title))

	for _, b := range buckets {
		fmt.Fprintln(w, barLabel.Render(dim.Render(b.Label))+bar(b, largest)+" "+bold.Render(fmt.Sprintf("%.0f%s", b.Value, unit)))
	}
}

// bar draws the bucket as a bar of at most barWidth, padded to it, coloured by its grade or dim if muted.
func bar(b analyze.Bucket, largest float64) string {
	n := 0
	if largest > 0 {
		n = int(b.Value/largest*barWidth + 0.5)
	}

	style := status(b.Status).style
	if b.Muted {
		style = dim
	}

	return style.Render(strings.Repeat("█", n)) + strings.Repeat(" ", barWidth-n)
}
