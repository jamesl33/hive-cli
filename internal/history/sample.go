package history

import "time"

// Metric is a kind of measurement.
type Metric string

// Metrics which are recorded.
const (
	// Temperature is the °C a device measured.
	Temperature Metric = "temperature"

	// Humidity is the percent relative humidity a device measured.
	Humidity Metric = "humidity"

	// Battery is the percent charge left in a device.
	Battery Metric = "battery"

	// Boiler is the state of the boiler, one of the Boiler* values, reported by the Hive receiver.
	Boiler Metric = "boiler"

	// Target is the °C the Hive receiver is set to hold the room at.
	Target Metric = "target"

	// Setpoint is the °C a radiator valve is set to hold.
	Setpoint Metric = "setpoint"

	// Valve is the percent a radiator valve is told to be open, taken from the opening it is set to use
	// while heating or while idle, whichever applies. It is not a measured position.
	Valve Metric = "valve"

	// ValveHeating is the percent a radiator valve is set to open to while heating, as it reports it.
	ValveHeating Metric = "valve_heating"

	// ValveIdle is the percent a radiator valve is set to open to while idle, as it reports it.
	ValveIdle Metric = "valve_idle"

	// ValveState is what a radiator valve reports it is doing, one of the Valve* values.
	ValveState Metric = "valve_state"

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
	// BoilerOff is a boiler which is switched off.
	BoilerOff float64 = iota

	// BoilerIdle is a boiler which is on but not firing.
	BoilerIdle

	// BoilerOn is a boiler which is firing.
	BoilerOn
)

// Values of the ValveState metric.
const (
	// ValveStateIdle is a valve which is not heating.
	ValveStateIdle float64 = iota

	// ValveStateHeat is a valve which is heating.
	ValveStateHeat
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

// IsUsage returns whether the metric is energy used, which is a total for a half hour and not a state.
func IsUsage(m Metric) bool {
	return m == Gas || m == Electricity
}
