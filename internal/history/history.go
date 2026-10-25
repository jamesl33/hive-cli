package history

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// daySpan is the time covered by each file.
	daySpan = 24 * time.Hour

	// dayLayout is the name of a day's file, without the extension.
	dayLayout = "2006-01-02"

	// ext is the extension of a day's file.
	ext = ".log"
)

// key identifies a series.
type key struct {
	device string
	metric Metric
}

// record is a measurement as written to disk. The value is a whole number of steps, see stepsPerUnit.
type record struct {
	at    time.Time
	key   key
	steps int
}

// stepsPerUnit is how finely a metric is recorded: temperatures and targets to 0.1°C, gas and electricity to the Wh
// and everything else to a whole number. Anything smaller is sensor noise, and recording it would
// write on every report.
func stepsPerUnit(m Metric) float64 {
	switch m {
	case Temperature, Target:
		return 10
	case Gas, Electricity:
		return 1000
	}

	return 1
}

// toSteps returns the sample's value in steps.
func toSteps(s Sample) int {
	return int(math.Round(s.Value * stepsPerUnit(s.Metric)))
}

// sample returns the record as a sample.
func (r record) sample() Sample {
	s := Sample{
		Device: r.key.device,
		Metric: r.key.metric,
		At:     r.at,
		Value:  float64(r.steps) / stepsPerUnit(r.key.metric),
	}

	return s
}

// appendRecord appends the record as a line: seconds since the epoch, metric, steps and device.
func appendRecord(b []byte, r record) []byte {
	b = strconv.AppendInt(b, r.at.Unix(), 10)
	b = append(b, ' ')
	b = append(b, r.key.metric...)
	b = append(b, ' ')
	b = strconv.AppendInt(b, int64(r.steps), 10)
	b = append(b, ' ')
	b = append(b, r.key.device...)

	return append(b, '\n')
}

// parseRecord parses a line written by appendRecord.
func parseRecord(line string) (record, bool) {
	secs, rest, ok := strings.Cut(line, " ")
	if !ok {
		return record{}, false
	}

	metric, rest, ok := strings.Cut(rest, " ")
	if !ok {
		return record{}, false
	}

	steps, device, ok := strings.Cut(rest, " ")
	if !ok || device == "" {
		return record{}, false
	}

	at, err := strconv.ParseInt(secs, 10, 64)
	if err != nil {
		return record{}, false
	}

	n, err := strconv.Atoi(steps)
	if err != nil {
		return record{}, false
	}

	r := record{at: time.Unix(at, 0).UTC(), key: key{device: device, metric: Metric(metric)}, steps: n}

	return r, true
}

// Log records measurements to disk with as little writing as possible, as it runs from an SD card.
//
// A measurement is dropped if it rounds to the value already recorded, and the rest are held in
// memory until Flush appends them to the file of their day. Files are named for the UTC day and are
// never deleted. Each file starts with the value of every series at the start of the day, so reading a
// day never needs the one before it.
type Log struct {
	dir string

	mu sync.Mutex

	// last is the most recent recorded value of each series.
	last map[key]int

	// pending is the lines not written yet, all for the day.
	pending []byte

	// day is the start of the day which pending belongs to.
	day time.Time
}

// OpenLog opens the history in the directory, creating it if needed.
func OpenLog(dir string) (*Log, error) {
	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", dir, err)
	}

	l := &Log{dir: dir, last: make(map[key]int)}

	days, err := listDays(dir)
	if err != nil {
		return nil, err
	}

	if len(days) == 0 {
		return l, nil
	}

	// The newest file holds the latest value of every series, so only the changes are recorded.
	newest := days[len(days)-1]

	records, err := readDay(dir, newest)
	if err != nil {
		return nil, err
	}

	for _, r := range records {
		l.last[r.key] = r.steps
	}

	l.day = newest

	return l, nil
}

// Add records a measurement, unless it is the same as the last one.
func (l *Log) Add(s Sample) {
	l.mu.Lock()
	defer l.mu.Unlock()

	k := key{device: s.Device, metric: s.Metric}
	steps := toSteps(s)

	// Energy used is not a state: a repeated value is another half hour, and is kept, and not carried
	// into the next day, as each half hour is recorded
	usage := IsUsage(s.Metric)

	old, ok := l.last[k]
	if ok && old == steps && !usage {
		return
	}

	l.roll(s.At)

	if !usage {
		l.last[k] = steps
	}

	l.pending = appendRecord(l.pending, record{at: s.At, key: k, steps: steps})
}

// Flush writes the measurements which are held in memory, and starts the file for the day of now if
// that is a new day. The time is needed so a day with no changes still gets a file.
func (l *Log) Flush(now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.roll(now)

	return l.write()
}

// roll moves on to the day of the time if it is a new day, queuing the starting value of every
// series if the day has no file yet. A time before the current day, from a clock which went back, is
// recorded in the current day.
func (l *Log) roll(at time.Time) {
	day := at.UTC().Truncate(daySpan)
	if !day.After(l.day) {
		return
	}

	_ = l.write() // Kept in memory and written with the next flush if this fails

	l.day = day

	_, err := os.Stat(l.path())
	if err == nil {
		return
	}

	for _, k := range slices.SortedFunc(maps.Keys(l.last), compareKeys) {
		l.pending = appendRecord(l.pending, record{at: day, key: k, steps: l.last[k]})
	}
}

// compareKeys orders series by device, then metric.
func compareKeys(a, b key) int {
	return cmp.Or(cmp.Compare(a.device, b.device), cmp.Compare(a.metric, b.metric))
}

// write appends the pending lines to the file of the day.
func (l *Log) write() error {
	if len(l.pending) == 0 {
		return nil
	}

	f, err := os.OpenFile(l.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open history: %w", err)
	}
	defer f.Close() // Closing again after the checked close below is harmless

	_, err = f.Write(l.pending)
	if err != nil {
		return fmt.Errorf("failed to write history: %w", err)
	}

	// Without it a power cut can leave the file longer but filled with zeros
	err = f.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync history: %w", err)
	}

	err = f.Close()
	if err != nil {
		return fmt.Errorf("failed to write history: %w", err)
	}

	l.pending = l.pending[:0]

	return nil
}

// path returns the file of the current day.
func (l *Log) path() string {
	return dayPath(l.dir, l.day)
}

// dayPath returns the file of a day.
func dayPath(dir string, day time.Time) string {
	return filepath.Join(dir, day.Format(dayLayout)+ext)
}

// listDays returns the days which have a file, oldest first.
func listDays(dir string) ([]time.Time, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read history: %w", err)
	}

	var days []time.Time

	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ext)
		if !ok {
			continue
		}

		day, err := time.Parse(dayLayout, name)
		if err != nil {
			continue
		}

		days = append(days, day)
	}

	return days, nil
}

// readDay returns the records in the file of a day. A line which can't be read, such as the last
// one after a power cut, is skipped. A power cut can also leave a run of NUL bytes, which may sit
// before a good record without a newline, so they separate records as well.
func readDay(dir string, day time.Time) ([]record, error) {
	f, err := os.Open(dayPath(dir, day))
	if err != nil {
		return nil, fmt.Errorf("failed to read history: %w", err)
	}
	defer f.Close()

	var records []record

	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		for _, part := range strings.Split(scanner.Text(), "\x00") {
			r, ok := parseRecord(part)
			if ok {
				records = append(records, r)
			}
		}
	}

	err = scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("failed to read history: %w", err)
	}

	return records, nil
}

// Load returns the recorded samples from the start of the day of from, oldest first. A directory
// which doesn't exist has no history.
func Load(dir string, from time.Time) ([]Sample, error) {
	days, err := listDays(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	start := from.UTC().Truncate(daySpan)

	var samples []Sample

	for _, day := range days {
		if day.Before(start) {
			continue
		}

		records, err := readDay(dir, day)
		if err != nil {
			return nil, err
		}

		for _, r := range records {
			samples = append(samples, r.sample())
		}
	}

	slices.SortStableFunc(samples, func(a, b Sample) int { return a.At.Compare(b.At) })

	return samples, nil
}

// DefaultDir returns where history is recorded unless told otherwise.
func DefaultDir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state != "" {
		return filepath.Join(state, "hive")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "hive-history"
	}

	return filepath.Join(home, ".local", "state", "hive")
}
