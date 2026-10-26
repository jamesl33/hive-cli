package dashboard

import (
	"cmp"
	"slices"
	"time"

	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/jamesl33/hive-cli/internal/hive"
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
	reports *Reports

	// version changes whenever the series do, so a drawing can be reused until it does.
	version uint64
}

// NewStore returns an empty store.
func NewStore(r hive.Receiver) *Store {
	s := &Store{
		Receiver: r,
		Series:   make(map[history.Metric]map[string]Series),
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
func (r *Reports) Samples(u z2m.Update) []history.Sample {
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
func Extract(r hive.Receiver, u z2m.Update, state map[string]any) []history.Sample {
	if u.Device != r.Name {
		samples := readings(u, history.Temperature, history.Humidity, history.Battery)

		return append(samples, valveSamples(u, state)...)
	}

	samples := readings(u, history.Battery)

	return append(samples, receiverSamples(r, u, state)...)
}

// readings returns the metrics which the report contains, taken from the attribute of the same name.
func readings(u z2m.Update, metrics ...history.Metric) []history.Sample {
	var samples []history.Sample

	for _, metric := range metrics {
		v, ok := u.Payload[string(metric)].(float64)
		if !ok {
			continue
		}

		samples = append(samples, history.Sample{Device: u.Device, Metric: metric, At: u.At, Value: v})
	}

	return samples
}

// valveSamples returns the temperature, setpoint and valve opening of a radiator valve if the
// report changes them. Anything which does not report valve positions is not a radiator valve.
func valveSamples(u z2m.Update, state map[string]any) []history.Sample {
	_, ok := state["heating_valve_position"]
	if !ok {
		return nil
	}

	var samples []history.Sample

	for attr, metric := range map[string]history.Metric{
		"local_temperature":         history.Temperature,
		"occupied_heating_setpoint": history.Setpoint,
	} {
		v, ok := u.Payload[attr].(float64)
		if ok {
			samples = append(samples, history.Sample{Device: u.Device, Metric: metric, At: u.At, Value: v})
		}
	}

	// The positions are logged as reported, so the log shows which of them the valve follows
	samples = append(samples, readingsAs(u, map[string]history.Metric{
		"heating_valve_position": history.ValveHeating,
		"idle_valve_position":    history.ValveIdle,
	})...)

	open, ok := valveOpening(state)
	if !ok {
		return samples
	}

	_, running := u.Payload["running_state"]
	_, heating := u.Payload["heating_valve_position"]
	_, idle := u.Payload["idle_valve_position"]

	if running || heating || idle {
		samples = append(samples, history.Sample{Device: u.Device, Metric: history.Valve, At: u.At, Value: open})
	}

	return samples
}

// readingsAs returns the attributes which the report contains, as the metric each one is mapped to.
func readingsAs(u z2m.Update, metrics map[string]history.Metric) []history.Sample {
	var samples []history.Sample

	for attr, metric := range metrics {
		v, ok := u.Payload[attr].(float64)
		if ok {
			samples = append(samples, history.Sample{Device: u.Device, Metric: metric, At: u.At, Value: v})
		}
	}

	return samples
}

// valveOpening returns the percent the valve is told to be open: the opening for the running state,
// unless it is closed and the other opening is not, as which one the valve follows is not known.
func valveOpening(state map[string]any) (float64, bool) {
	attr, other := "idle_valve_position", "heating_valve_position"
	if state["running_state"] == "heat" {
		attr, other = other, attr
	}

	v, ok := state[attr].(float64)
	o, okOther := state[other].(float64)

	if okOther && (!ok || v == 0 && o != 0) {
		return o, true
	}

	return v, ok
}

// receiverSamples returns the room temperature, target and boiler state if the report changes them.
func receiverSamples(r hive.Receiver, u z2m.Update, state map[string]any) []history.Sample {
	var samples []history.Sample

	s := hive.Interpret(r, state)

	if _, ok := u.Payload[r.Key("local_temperature")]; ok && s.Temperature != nil {
		samples = append(samples, history.Sample{Device: u.Device, Metric: history.Temperature, At: u.At, Value: *s.Temperature})
	}

	if _, ok := u.Payload[r.Key("occupied_heating_setpoint")]; ok && s.Target != nil {
		samples = append(samples, history.Sample{Device: u.Device, Metric: history.Target, At: u.At, Value: *s.Target})
	}

	_, mode := u.Payload[r.Key("system_mode")]
	_, running := u.Payload[r.Key("running_state")]

	if (mode || running) && s.Mode != hive.ModeUnknown {
		samples = append(samples, history.Sample{Device: u.Device, Metric: history.Boiler, At: u.At, Value: boilerState(s)})
	}

	return samples
}

// boilerState works out whether the boiler is off, idle or firing.
func boilerState(s hive.State) float64 {
	if s.Mode == hive.ModeOff {
		return history.BoilerOff
	}

	if s.Active != nil && *s.Active {
		return history.BoilerOn
	}

	return history.BoilerIdle
}
