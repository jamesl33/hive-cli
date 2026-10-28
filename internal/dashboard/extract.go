package dashboard

import (
	"cmp"
	"slices"
	"time"

	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/measure"
	"github.com/jamesl33/hive-cli/internal/z2m"
)

// Store holds everything shown by the dashboard.
type Store struct {
	// Receiver is the Hive receiver; it reports the boiler and the Hive temperature.
	Receiver hive.Receiver

	// Thermostat is the friendly name of the Hive thermostat, which is listed with the receiver.
	Thermostat string

	// Outdoors is the friendly name of the outdoor sensor, which the charts can leave out.
	Outdoors string

	// Order is the devices grouped as the receiver, the sensors and the radiator valves, and sorted by name
	// within each group, so each gets the same colour and key on every run.
	Order []string

	// Series is the samples of each device, by metric.
	Series map[history.Metric]map[string]Series

	// dir is where the history is loaded from, if any.
	dir string

	// reports works out the measurements in each device report.
	reports *measure.Reports

	// version changes whenever the series do, so a drawing can be reused until it does.
	version uint64
}

// NewStore returns an empty store.
func NewStore(r hive.Receiver) *Store {
	s := &Store{
		Receiver: r,
		Series:   make(map[history.Metric]map[string]Series),
		reports:  measure.NewReports(r),
	}

	return s
}

// Apply records the measurements in a device report.
func (s *Store) Apply(u z2m.Update) {
	for _, sample := range s.reports.Samples(u) {
		s.add(sample)
	}
}

// Restore replaces the series with the history recorded in the directory from the start of the day
// of from, and remembers the directory for Reload. Samples which are newer than the history, as they
// are not written yet, are kept.
func (s *Store) Restore(dir string, from time.Time) error {
	samples, err := history.Load(dir, from)
	if err != nil {
		return err
	}

	var newest time.Time
	if len(samples) > 0 {
		newest = samples[len(samples)-1].At
	}

	live := s.after(newest)

	s.dir = dir
	s.Series = make(map[history.Metric]map[string]Series)
	s.version++

	for _, sample := range append(samples, live...) {
		s.add(sample)
	}

	return nil
}

// after returns the samples which are newer than the time, oldest first.
func (s *Store) after(t time.Time) []history.Sample {
	var samples []history.Sample

	for metric, byDevice := range s.Series {
		for device, series := range byDevice {
			samples = append(samples, seriesAfter(series, t, device, metric)...)
		}
	}

	slices.SortStableFunc(samples, func(a, b history.Sample) int { return a.At.Compare(b.At) })

	return samples
}

// seriesAfter returns the points of a series which are newer than the time, as samples.
func seriesAfter(series Series, t time.Time, device string, metric history.Metric) []history.Sample {
	var samples []history.Sample

	for _, p := range series {
		if !p.At.After(t) {
			continue
		}

		samples = append(samples, history.Sample{Device: device, Metric: metric, At: p.At, Value: p.Value})
	}

	return samples
}

// Reload loads the history from the start of the day of from, if Restore has been given a directory.
func (s *Store) Reload(from time.Time) error {
	if s.dir == "" {
		return nil
	}

	return s.Restore(s.dir, from)
}

// add appends a sample to its series.
func (s *Store) add(sample history.Sample) {
	s.version++

	if s.Series[sample.Metric] == nil {
		s.Series[sample.Metric] = make(map[string]Series)
	}

	series := s.Series[sample.Metric]
	series[sample.Device] = series[sample.Device].Append(Point{At: sample.At, Value: sample.Value})

	// Energy use comes from the meter, not a device, so it has no place in the legend or the keys
	if history.IsUsage(sample.Metric) {
		return
	}

	// A device which first reports a valve opening moves to the radiator valves
	if !slices.Contains(s.Order, sample.Device) || sample.Metric == history.Valve && len(series[sample.Device]) == 1 {
		s.sortOrder(sample.Device)
	}
}

// sortOrder adds the device to the order if it is not in it, and sorts the order by group and name.
func (s *Store) sortOrder(device string) {
	if !slices.Contains(s.Order, device) {
		s.Order = append(s.Order, device)
	}

	slices.SortFunc(s.Order, func(a, b string) int {
		return cmp.Or(cmp.Compare(s.group(a), s.group(b)), cmp.Compare(a, b))
	})
}

// groupDevices returns the devices in the group, by name.
func (s *Store) groupDevices(g group) []string {
	var devices []string

	for _, device := range s.Order {
		if s.group(device) == g {
			devices = append(devices, device)
		}
	}

	return devices
}

// group says which group of devices the device is in.
func (s *Store) group(device string) group {
	switch {
	case device == s.Receiver.Name || device == s.Thermostat && device != "":
		return groupReceiver
	case len(s.Series[history.Valve][device]) > 0:
		return groupValves
	}

	return groupSensors
}
