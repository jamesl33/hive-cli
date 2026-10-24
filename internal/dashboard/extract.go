package dashboard

import (
	"slices"
	"time"

	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/z2m"
)

// Metric is a kind of measurement.
type Metric string

// Metrics shown by the dashboard.
const (
	Temperature Metric = "temperature"
	Humidity    Metric = "humidity"
	Battery     Metric = "battery"
	Boiler      Metric = "boiler"
	Target      Metric = "target"

	// Gas is the kWh of gas used in the half hour starting at the sample, from the smart meter. It is
	// drawn on the usage chart. As with every metric a repeated value is dropped, so a half hour
	// without a sample has the value of the one before.
	Gas Metric = "gas"

	// Electricity is the kWh of electricity used in the half hour starting at the sample, recorded
	// like Gas.
	Electricity Metric = "electricity"
)

// Values of the Boiler metric.
const (
	BoilerOff float64 = iota
	BoilerIdle
	BoilerOn
)

// Sample is a measurement from a device.
type Sample struct {
	// Device is the friendly name of the device.
	Device string

	// Metric is what was measured.
	Metric Metric

	// At is when the measurement was received.
	At time.Time

	// Value is the measurement; for the Boiler metric it is one of the Boiler* values.
	Value float64
}

// Store holds everything shown by the dashboard.
type Store struct {
	// Receiver is the Hive receiver; it reports the boiler and the Hive temperature.
	Receiver hive.Receiver

	// Outdoors is the friendly name of the outdoor sensor, which the charts can leave out.
	Outdoors string

	// Order is the devices sorted by name, so each gets the same colour on every run.
	Order []string

	// Series is the samples of each device, by metric.
	Series map[Metric]map[string]Series

	// dir is where the history is loaded from, if any.
	dir string

	// reports works out the measurements in each device report.
	reports *Reports

	// version changes whenever the series do, so a drawing can be reused until it does.
	version uint64
}

// NewStore returns an empty store.
func NewStore(r hive.Receiver) *Store {
	s := &Store{
		Receiver: r,
		Series:   make(map[Metric]map[string]Series),
		reports:  NewReports(r),
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
	samples, err := Load(dir, from)
	if err != nil {
		return err
	}

	var newest time.Time
	if len(samples) > 0 {
		newest = samples[len(samples)-1].At
	}

	live := s.after(newest)

	s.dir = dir
	s.Series = make(map[Metric]map[string]Series)
	s.version++

	for _, sample := range append(samples, live...) {
		s.add(sample)
	}

	return nil
}

// after returns the samples which are newer than the time, oldest first.
func (s *Store) after(t time.Time) []Sample {
	var samples []Sample

	for metric, byDevice := range s.Series {
		for device, series := range byDevice {
			samples = append(samples, seriesAfter(series, t, device, metric)...)
		}
	}

	slices.SortStableFunc(samples, func(a, b Sample) int { return a.At.Compare(b.At) })

	return samples
}

// seriesAfter returns the points of a series which are newer than the time, as samples.
func seriesAfter(series Series, t time.Time, device string, metric Metric) []Sample {
	var samples []Sample

	for _, p := range series {
		if !p.At.After(t) {
			continue
		}

		samples = append(samples, Sample{Device: device, Metric: metric, At: p.At, Value: p.Value})
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
func (s *Store) add(sample Sample) {
	s.version++

	if s.Series[sample.Metric] == nil {
		s.Series[sample.Metric] = make(map[string]Series)
	}

	series := s.Series[sample.Metric]
	series[sample.Device] = series[sample.Device].Append(Point{At: sample.At, Value: sample.Value})

	// Energy use comes from the meter, not a device, so it has no place in the legend or the keys
	if isUsage(sample.Metric) {
		return
	}

	i, found := slices.BinarySearch(s.Order, sample.Device)
	if !found {
		s.Order = slices.Insert(s.Order, i, sample.Device)
	}
}

// Reports works out the measurements in device reports.
type Reports struct {
	receiver hive.Receiver

	// state is the attributes each device has reported so far.
	state map[string]map[string]any
}

// NewReports returns a Reports which has seen no reports.
func NewReports(r hive.Receiver) *Reports {
	rp := &Reports{receiver: r, state: make(map[string]map[string]any)}

	return rp
}

// Samples returns the measurements in a report.
func (r *Reports) Samples(u z2m.Update) []Sample {
	if r.state[u.Device] == nil {
		r.state[u.Device] = make(map[string]any)
	}

	for k, v := range u.Payload {
		r.state[u.Device][k] = v
	}

	return Extract(r.receiver, u, r.state[u.Device])
}

// Extract returns the measurements in a report. The state is every attribute the device has
// reported, which the receiver needs to work out the state of the boiler.
func Extract(r hive.Receiver, u z2m.Update, state map[string]any) []Sample {
	if u.Device != r.Name {
		return readings(u, Temperature, Humidity, Battery)
	}

	samples := readings(u, Battery)

	return append(samples, receiverSamples(r, u, state)...)
}

// readings returns the metrics which the report contains, taken from the attribute of the same name.
func readings(u z2m.Update, metrics ...Metric) []Sample {
	var samples []Sample

	for _, metric := range metrics {
		v, ok := u.Payload[string(metric)].(float64)
		if !ok {
			continue
		}

		samples = append(samples, Sample{Device: u.Device, Metric: metric, At: u.At, Value: v})
	}

	return samples
}

// receiverSamples returns the room temperature, target and boiler state if the report changes them.
func receiverSamples(r hive.Receiver, u z2m.Update, state map[string]any) []Sample {
	var samples []Sample

	s := hive.Interpret(r, state)

	if _, ok := u.Payload[r.Key("local_temperature")]; ok && s.Temperature != nil {
		samples = append(samples, Sample{Device: u.Device, Metric: Temperature, At: u.At, Value: *s.Temperature})
	}

	if _, ok := u.Payload[r.Key("occupied_heating_setpoint")]; ok && s.Target != nil {
		samples = append(samples, Sample{Device: u.Device, Metric: Target, At: u.At, Value: *s.Target})
	}

	_, mode := u.Payload[r.Key("system_mode")]
	_, running := u.Payload[r.Key("running_state")]

	if (mode || running) && s.Mode != hive.ModeUnknown {
		samples = append(samples, Sample{Device: u.Device, Metric: Boiler, At: u.At, Value: boilerState(s)})
	}

	return samples
}

// boilerState works out whether the boiler is off, idle or firing.
func boilerState(s hive.State) float64 {
	if s.Mode == hive.ModeOff {
		return BoilerOff
	}

	if s.Active != nil && *s.Active {
		return BoilerOn
	}

	return BoilerIdle
}
