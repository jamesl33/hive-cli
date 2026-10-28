// Package measure works out the history measurements in device reports.
package measure

import (
	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/z2m"
)

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

	return extract(r.receiver, u, r.state[u.Device])
}

// extract returns the measurements in a report. The state is every attribute the device has
// reported, which the receiver needs to work out the state of the boiler.
func extract(r hive.Receiver, u z2m.Update, state map[string]any) []history.Sample {
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

	attrs := map[string]history.Metric{
		"local_temperature":         history.Temperature,
		"occupied_heating_setpoint": history.Setpoint,
	}

	for attr, metric := range attrs {
		v, ok := u.Payload[attr].(float64)
		if ok {
			samples = append(samples, history.Sample{Device: u.Device, Metric: metric, At: u.At, Value: v})
		}
	}

	// The positions are logged as reported, so the log shows which of them the valve follows
	positions := map[string]history.Metric{
		"heating_valve_position": history.ValveHeating,
		"idle_valve_position":    history.ValveIdle,
	}

	samples = append(samples, readingsAs(u, positions)...)

	run, running := u.Payload["running_state"]
	if running {
		v := history.ValveStateIdle
		if run == "heat" {
			v = history.ValveStateHeat
		}

		samples = append(samples, history.Sample{Device: u.Device, Metric: history.ValveState, At: u.At, Value: v})
	}

	open, ok := valveOpening(state)
	if !ok {
		return samples
	}

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
