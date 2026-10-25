package analyze

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/jamesl33/hive-cli/internal/history"
)

// Status grades how healthy a measurement is.
type Status int

// Grades of a measurement.
const (
	// StatusOK is a healthy measurement.
	StatusOK Status = iota

	// StatusWarn is a measurement worth watching.
	StatusWarn

	// StatusBad is a measurement which points to a fault.
	StatusBad
)

// Thresholds above which a measurement is graded. They are margins to tune from logs, not fitted rules.
const (
	// ShortRun is how long a run must last to not be a short cycle.
	ShortRun = 5 * time.Minute

	// VeryShortRun is how long a run must last to not be a very short cycle.
	VeryShortRun = 2 * time.Minute

	// MildTemp is the outdoor °C above which the heating should not need to run most of the time.
	MildTemp = 8.0

	// TopUpWindow is how long after the target drops a run counts as a top-up for the setback.
	TopUpWindow = 30 * time.Minute

	// OvershootWindow is how long after a run the room still counts as heated by it.
	OvershootWindow = 30 * time.Minute

	// warmShortShare is the share of runs under ShortRun which is worth watching.
	warmShortShare = 0.25

	// badShortShare is the share of runs under ShortRun which points to a fault.
	badShortShare = 0.5

	// warmStarts is the starts in the busiest hour which are worth watching.
	warmStarts = 3

	// badStarts is the starts in the busiest hour which point to a fault.
	badStarts = 6

	// warmDuty is the share of time firing which is worth watching.
	warmDuty = 0.8

	// badDuty is the share of time firing which points to a fault.
	badDuty = 0.95

	// warmTopUps is the setback top-ups which are worth watching.
	warmTopUps = 1

	// badTopUps is the setback top-ups which point to a fault.
	badTopUps = 3

	// warmOvershoot is the °C over the target which is worth watching.
	warmOvershoot = 0.5

	// badOvershoot is the °C over the target which points to a fault.
	badOvershoot = 1.5

	// minRuns is how many complete runs the grades of their length need before they can be trusted.
	minRuns = 3

	// minBandSpan is the time an outdoor band needs to be known for before its duty can be trusted.
	minBandSpan = time.Hour

	// hoursInDay is how many bars the starts by hour histogram has.
	hoursInDay = 24
)

var (
	// runEdges are the lengths which split the run length histogram. A run goes in the first bucket whose
	// edge it is under, or the last.
	runEdges = []time.Duration{VeryShortRun, ShortRun, 10 * time.Minute, 20 * time.Minute}

	// runLabels name the buckets of the run length histogram.
	runLabels = []string{"<2m", "2–5m", "5–10m", "10–20m", "20m+"}

	// runStatuses are the grades of the buckets of the run length histogram.
	runStatuses = []Status{StatusBad, StatusWarn, StatusOK, StatusOK, StatusOK}

	// bandEdges are the outdoor °C which split the duty by outdoor temperature histogram.
	bandEdges = []float64{4, MildTemp, 12}

	// bandLabels name the buckets of the duty by outdoor temperature histogram.
	bandLabels = []string{"<4°C", "4–8°C", "8–12°C", "12°C+"}
)

// Run is a time the boiler was firing.
type Run struct {
	// Start is when the boiler started firing, or the start of the period if it already was.
	Start time.Time

	// End is when the boiler stopped firing, or the end of the period if it still was.
	End time.Time

	// Carried is true if the run began before the period, so its start and length are not its own.
	Carried bool

	// Open is true if the run had not ended by the end of the period, so its length is not its own.
	Open bool
}

// Length is how long the run lasted in the period.
func (r Run) Length() time.Duration { return r.End.Sub(r.Start) }

// point is the value of a series from a time.
type point struct {
	// At is when the series took the value.
	At time.Time

	// Value is the value of the series.
	Value float64
}

// Bucket is a bar of a histogram.
type Bucket struct {
	// Label names the bucket.
	Label string

	// Value is the size of the bar.
	Value float64

	// Status is the grade of the bucket.
	Status Status

	// Muted is true if there is too little behind the value to trust it.
	Muted bool
}

// Analysis is how the boiler behaved over a period, to point out an unhealthy setup.
type Analysis struct {
	// Runs is every time the boiler fired in the period.
	Runs []Run

	// Starts is how many times the boiler started in the period.
	Starts int

	// Complete is how many runs started and ended in the period, which is what their length is known for.
	Complete int

	// MedianRun is the median length of the complete runs.
	MedianRun time.Duration

	// ShortShare is the share of the complete runs under ShortRun.
	ShortShare float64

	// StartsPerHour is the starts averaged over the period.
	StartsPerHour float64

	// WorstHour is the most starts in a single hour.
	WorstHour int

	// Duty is the share of the period the boiler was firing.
	Duty float64

	// MildDuty is the share of the time the outdoors was above MildTemp the boiler was firing.
	MildDuty float64

	// MildKnown is true if MildDuty is worked out from enough time.
	MildKnown bool

	// TopUps is when runs started soon after the target dropped.
	TopUps []time.Time

	// Overshoot is the most °C the room was over the target while or soon after the boiler fired.
	Overshoot float64

	// RunLengths is the complete runs by length.
	RunLengths []Bucket

	// StartsByHour is the starts by hour of the day.
	StartsByHour []Bucket

	// DutyByOutdoors is the percent of time firing by outdoor temperature.
	DutyByOutdoors []Bucket
}

// MedianStatus grades the median run.
func (a Analysis) MedianStatus() Status {
	switch {
	case a.Complete < minRuns:
		return StatusOK
	case a.MedianRun < VeryShortRun:
		return StatusBad
	case a.MedianRun < ShortRun:
		return StatusWarn
	}

	return StatusOK
}

// ShortStatus grades the share of short runs.
func (a Analysis) ShortStatus() Status {
	if a.Complete < minRuns {
		return StatusOK
	}

	return grade(a.ShortShare, warmShortShare, badShortShare)
}

// StartsStatus grades the starts in the busiest hour.
func (a Analysis) StartsStatus() Status { return grade(float64(a.WorstHour), warmStarts, badStarts) }

// GradedDuty returns the duty which is graded: that in mild weather if known, as that is when the boiler
// should not be busy, and the duty overall if not.
func (a Analysis) GradedDuty() float64 {
	if a.MildKnown {
		return a.MildDuty
	}

	return a.Duty
}

// DutyStatus grades the duty.
func (a Analysis) DutyStatus() Status { return grade(a.GradedDuty(), warmDuty, badDuty) }

// TopUpStatus grades the setback top-ups.
func (a Analysis) TopUpStatus() Status { return grade(float64(len(a.TopUps)), warmTopUps, badTopUps) }

// OvershootStatus grades the overshoot.
func (a Analysis) OvershootStatus() Status { return grade(a.Overshoot, warmOvershoot, badOvershoot) }

// grade returns the status of a value which is worse the higher it is.
func grade(v, warn, bad float64) Status {
	switch {
	case v >= bad:
		return StatusBad
	case v >= warn:
		return StatusWarn
	}

	return StatusOK
}

// Analyze works out how the boiler behaved from the samples between from and to, oldest first. The
// receiver is the device which reports the boiler, target and room temperature, and outdoors the outdoor
// sensor.
func Analyze(samples []history.Sample, receiver, outdoors string, from, to time.Time) Analysis {
	runs := findRuns(samples, receiver, from, to)
	started := startedRuns(runs)
	lengths := completeLengths(started)
	period := to.Sub(from)

	a := Analysis{
		Runs:          runs,
		Starts:        len(started),
		Complete:      len(lengths),
		MedianRun:     median(lengths),
		ShortShare:    shortShare(lengths),
		StartsPerHour: float64(len(started)) / period.Hours(),
		WorstHour:     worstHour(started),
		Duty:          share(onTime(runs, from, to), period),
		TopUps:        topUps(started, targetDrops(samples, receiver, to)),
		Overshoot:     overshoot(samples, receiver, runs, from, to),
		RunLengths:    runLengthBuckets(lengths),
		StartsByHour:  startsByHour(started, from.Location()),
	}

	a.DutyByOutdoors, a.MildDuty, a.MildKnown = dutyByOutdoors(samples, outdoors, runs, from, to)

	return a
}

// findRuns returns the times the receiver fired the boiler in the period, oldest first. A run which began
// before the period starts at from.
func findRuns(samples []history.Sample, receiver string, from, to time.Time) []Run {
	var (
		runs []Run
		cur  Run
		on   bool
	)

	for _, s := range samples {
		if s.At.After(to) {
			break
		}

		if s.Metric != history.Boiler || s.Device != receiver {
			continue
		}

		firing := s.Value == history.BoilerOn

		if firing && !on {
			cur = Run{Start: s.At, Carried: s.At.Before(from)}
			if cur.Carried {
				cur.Start = from
			}
		}

		// A run which ended before the period has nothing in it.
		if !firing && on && s.At.After(from) {
			cur.End = s.At
			runs = append(runs, cur)
		}

		on = firing
	}

	if on {
		cur.End, cur.Open = to, true
		runs = append(runs, cur)
	}

	return runs
}

// startedRuns returns the runs which began in the period.
func startedRuns(runs []Run) []Run {
	return slices.DeleteFunc(slices.Clone(runs), func(r Run) bool { return r.Carried })
}

// completeLengths returns how long each of the runs lasted, sorted, leaving out those which had not ended.
func completeLengths(runs []Run) []time.Duration {
	var lengths []time.Duration

	for _, r := range runs {
		if !r.Open {
			lengths = append(lengths, r.Length())
		}
	}

	slices.Sort(lengths)

	return lengths
}

// median returns the middle of sorted lengths, or zero if there are none.
func median(sorted []time.Duration) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	return sorted[len(sorted)/2]
}

// shortShare returns the share of the lengths which are under ShortRun.
func shortShare(lengths []time.Duration) float64 {
	if len(lengths) == 0 {
		return 0
	}

	short := 0

	for _, l := range lengths {
		if l < ShortRun {
			short++
		}
	}

	return float64(short) / float64(len(lengths))
}

// worstHour returns the most runs which started in one hour.
func worstHour(runs []Run) int {
	byHour := make(map[time.Time]int)
	worst := 0

	for _, r := range runs {
		hour := r.Start.Truncate(time.Hour)
		byHour[hour]++
		worst = max(worst, byHour[hour])
	}

	return worst
}

// onTime returns how long the runs lasted between a and b.
func onTime(runs []Run, a, b time.Time) time.Duration {
	// Runs don't overlap, so they are sorted by end as well as start.
	first, _ := slices.BinarySearchFunc(runs, a, func(r Run, t time.Time) int { return r.End.Compare(t) })

	var total time.Duration

	for _, r := range runs[first:] {
		if !r.Start.Before(b) {
			break
		}

		total += overlap(r, a, b)
	}

	return total
}

// overlap returns how long the run lasted between a and b.
func overlap(r Run, a, b time.Time) time.Duration {
	lo, hi := a, b
	if r.Start.After(lo) {
		lo = r.Start
	}

	if r.End.Before(hi) {
		hi = r.End
	}

	return max(hi.Sub(lo), 0)
}

// targetDrops returns when the target of the receiver dropped, up to the time.
func targetDrops(samples []history.Sample, receiver string, to time.Time) []time.Time {
	var (
		drops []time.Time
		prev  float64
		known bool
	)

	for _, s := range samples {
		if s.At.After(to) {
			break
		}

		if s.Metric != history.Target || s.Device != receiver {
			continue
		}

		if known && s.Value < prev {
			drops = append(drops, s.At)
		}

		prev, known = s.Value, true
	}

	return drops
}

// topUps returns when each run started which began within TopUpWindow of a drop in the target.
func topUps(runs []Run, drops []time.Time) []time.Time {
	var starts []time.Time

	for _, r := range runs {
		// The newest drop at or before the start.
		i := sort.Search(len(drops), func(i int) bool { return drops[i].After(r.Start) })
		if i == 0 || r.Start.Sub(drops[i-1]) > TopUpWindow {
			continue
		}

		starts = append(starts, r.Start)
	}

	return starts
}

// overshoot returns the most °C the room temperature was over the target while the boiler fired or within
// OvershootWindow after. The target is the one the boiler stopped at, as a setback after it is not a
// miss.
func overshoot(samples []history.Sample, receiver string, runs []Run, from, to time.Time) float64 {
	targets := receiverPoints(samples, receiver, history.Target, to)
	peak := 0.0

	for _, s := range samples {
		if s.At.After(to) {
			break
		}

		if s.Metric != history.Temperature || s.Device != receiver || s.At.Before(from) {
			continue
		}

		r, ok := heatedBy(runs, s.At)
		if !ok {
			continue
		}

		// The target when the run ended, or now if it is still going. A setback is often the very reason
		// the boiler stopped, so it is not the target the run was aiming for.
		at := s.At
		if r.End.Before(at) {
			at = r.End.Add(-time.Nanosecond)
		}

		target, ok := targetAt(targets, at)
		if ok {
			peak = max(peak, s.Value-target)
		}
	}

	return peak
}

// heatedBy returns the run which is firing at the time, or ended within OvershootWindow before.
func heatedBy(runs []Run, t time.Time) (Run, bool) {
	// The newest run which started at or before the time.
	i := sort.Search(len(runs), func(i int) bool { return runs[i].Start.After(t) })
	if i == 0 || t.After(runs[i-1].End.Add(OvershootWindow)) {
		return Run{}, false
	}

	return runs[i-1], true
}

// receiverPoints returns the points of a metric of a device up to the time.
func receiverPoints(samples []history.Sample, device string, metric history.Metric, to time.Time) []point {
	var points []point

	for _, s := range samples {
		if s.At.After(to) {
			break
		}

		if s.Metric == metric && s.Device == device {
			points = append(points, point{At: s.At, Value: s.Value})
		}
	}

	return points
}

// targetAt returns the value of the points at the time, which is the last one at or before it.
func targetAt(points []point, t time.Time) (float64, bool) {
	i := sort.Search(len(points), func(i int) bool { return points[i].At.After(t) })
	if i == 0 {
		return 0, false
	}

	return points[i-1].Value, true
}

// runLengthBuckets counts the sorted lengths into the run length histogram.
func runLengthBuckets(lengths []time.Duration) []Bucket {
	buckets := make([]Bucket, len(runLabels))
	for i, label := range runLabels {
		buckets[i] = Bucket{Label: label, Status: runStatuses[i]}
	}

	for _, l := range lengths {
		i := slices.IndexFunc(runEdges, func(edge time.Duration) bool { return l < edge })
		if i < 0 {
			i = len(runEdges)
		}

		buckets[i].Value++
	}

	return buckets
}

// startsByHour counts the starts into the hour of the day they began in, in the location.
func startsByHour(runs []Run, loc *time.Location) []Bucket {
	buckets := make([]Bucket, hoursInDay)
	for h := range buckets {
		buckets[h].Label = fmt.Sprintf("%02d", h)
	}

	for _, r := range runs {
		buckets[r.Start.In(loc).Hour()].Value++
	}

	return buckets
}

// dutyByOutdoors works out the percent of time the boiler fired in each band of outdoor temperature, and
// the share of time it fired above MildTemp, which is only known if the outdoors was known for long enough.
func dutyByOutdoors(samples []history.Sample, outdoors string, runs []Run, from, to time.Time) ([]Bucket, float64, bool) {
	points := receiverPoints(samples, outdoors, history.Temperature, to)
	spans := make([]time.Duration, len(bandLabels))
	firing := make([]time.Duration, len(bandLabels))

	for i, p := range points {
		start, end := p.At, to
		if start.Before(from) {
			start = from
		}

		if i+1 < len(points) && points[i+1].At.Before(end) {
			end = points[i+1].At
		}

		if !end.After(start) {
			continue
		}

		band := slices.IndexFunc(bandEdges, func(edge float64) bool { return p.Value < edge })
		if band < 0 {
			band = len(bandEdges)
		}

		spans[band] += end.Sub(start)
		firing[band] += onTime(runs, start, end)
	}

	buckets := make([]Bucket, len(bandLabels))
	for i, label := range bandLabels {
		buckets[i] = Bucket{Label: label, Value: 100 * share(firing[i], spans[i]), Muted: spans[i] < minBandSpan}
	}

	mild, known := mildDuty(spans, firing)

	return buckets, mild, known
}

// mildDuty returns the share of the time in the bands above MildTemp which the boiler fired, and whether
// the bands were known for long enough to say.
func mildDuty(spans, firing []time.Duration) (float64, bool) {
	var span, on time.Duration

	for i := slices.Index(bandEdges, MildTemp) + 1; i < len(spans); i++ {
		span += spans[i]
		on += firing[i]
	}

	return share(on, span), span >= minBandSpan
}

// share returns part as a share of whole, or zero if there is no whole.
func share(part, whole time.Duration) float64 {
	if whole <= 0 {
		return 0
	}

	return part.Seconds() / whole.Seconds()
}
