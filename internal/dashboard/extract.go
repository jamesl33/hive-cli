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

	// Order is the devices sorted by name, so each gets the same colour on every run.
	Order []string

	// Series is the samples of each device, by metric.
	Series map[Metric]map[string]Series

	// state is the attributes each device has reported so far.
	state map[string]map[string]any
}

// NewStore returns an empty store.
func NewStore(r hive.Receiver) *Store {
	s := &Store{
		Receiver: r,
		Series:   make(map[Metric]map[string]Series),
		state:    make(map[string]map[string]any),
	}

	return s
}

// Apply records the measurements in a device report.
func (s *Store) Apply(u z2m.Update) {
	if s.state[u.Device] == nil {
		s.state[u.Device] = make(map[string]any)
	}

	for k, v := range u.Payload {
		s.state[u.Device][k] = v
	}

	for _, sample := range Extract(s.Receiver, u, s.state[u.Device]) {
		s.add(sample)
	}
}

// add appends a sample to its series.
func (s *Store) add(sample Sample) {
	if s.Series[sample.Metric] == nil {
		s.Series[sample.Metric] = make(map[string]Series)
	}

	series := s.Series[sample.Metric]
	series[sample.Device] = series[sample.Device].Append(Point{At: sample.At, Value: sample.Value})

	i, found := slices.BinarySearch(s.Order, sample.Device)
	if !found {
		s.Order = slices.Insert(s.Order, i, sample.Device)
	}
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

// receiverSamples returns the room temperature and boiler state if the report changes them.
func receiverSamples(r hive.Receiver, u z2m.Update, state map[string]any) []Sample {
	var samples []Sample

	s := hive.Interpret(r, state)

	if _, ok := u.Payload[r.Key("local_temperature")]; ok && s.Temperature != nil {
		samples = append(samples, Sample{Device: u.Device, Metric: Temperature, At: u.At, Value: *s.Temperature})
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
