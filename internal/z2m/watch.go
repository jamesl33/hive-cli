package z2m

import (
	"context"
	"encoding/json/v2"
	"strings"
	"time"

	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/mqtt"
)

// Update is a state report from a device.
type Update struct {
	// Device is the friendly name of the device.
	Device string

	// At is when the report was received.
	At time.Time

	// Payload is the attributes in the report.
	Payload map[string]any
}

// Feed is a live stream of device reports.
type Feed struct {
	// Receiver is the Hive receiver.
	Receiver hive.Receiver

	// Thermostat is the friendly name of the Hive thermostat, or empty if there is none.
	Thermostat string

	// client is the broker connection.
	client *mqtt.Client

	// names is the friendly names of the known devices.
	names map[string]bool
}

// Watch connects to the broker and streams the reports of every device, asking the receiver for
// its current state. The connection is restored automatically if it drops.
func Watch(ctx context.Context, opts *Options) (*Feed, error) {
	c, err := mqtt.DialLive(opts.Broker, opts.Username, opts.Password)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	f, err := setup(ctx, c)
	if err != nil {
		c.Close()
		return nil, err
	}

	return f, nil
}

// setup finds the receiver and starts the stream of reports. The caller closes the client if it fails.
func setup(ctx context.Context, c *mqtt.Client) (*Feed, error) {
	devices, err := Devices(ctx, c)
	if err != nil {
		return nil, err
	}

	r, err := pickReceiver(devices)
	if err != nil {
		return nil, err
	}

	// Friendly names may contain "/", so subscribe to everything and match against the known names.
	err = c.Subscribe(baseTopic + "/#")
	if err != nil {
		return nil, err
	}

	b, err := hive.Get(hive.StatusKeys(r)).Bytes()
	if err != nil {
		return nil, err
	}

	err = c.Publish(baseTopic+"/"+r.Name+"/get", b)
	if err != nil {
		return nil, err
	}

	names := make(map[string]bool, len(devices))
	thermostat := ""

	for _, d := range devices {
		names[d.FriendlyName] = true

		if d.IsThermostat() {
			thermostat = d.FriendlyName
		}
	}

	f := &Feed{Receiver: r, Thermostat: thermostat, client: c, names: names}

	return f, nil
}

// Next blocks until a device reports its state. Retained reports are included since they are the
// last known state of a device.
func (f *Feed) Next(ctx context.Context) (Update, error) {
	for {
		m, err := f.client.Next(ctx, 0)
		if err != nil {
			return Update{}, err
		}

		u, ok := parseUpdate(m, time.Now(), f.names)
		if ok {
			return u, nil
		}
	}
}

// Connected returns whether the connection to the broker is currently up.
func (f *Feed) Connected() bool {
	return f.client.Connected()
}

// Close closes the connection.
func (f *Feed) Close() {
	f.client.Close()
}

// parseUpdate converts a message into a device report, ignoring anything which is not the state of
// one of the known devices (bridge messages, availability and so on).
func parseUpdate(m mqtt.Message, at time.Time, names map[string]bool) (Update, bool) {
	device, ok := strings.CutPrefix(m.Topic, baseTopic+"/")
	if !ok || !names[device] {
		return Update{}, false
	}

	var payload map[string]any

	if json.Unmarshal(m.Payload, &payload) != nil {
		return Update{}, false
	}

	return Update{Device: device, At: lastSeen(payload, at), Payload: payload}, true
}

// lastSeen returns when the device last reported according to Zigbee2MQTT, or the time the message was
// received if the report has no usable last_seen (it needs advanced.last_seen set to ISO_8601 or epoch).
// A replayed retained message would otherwise look as new as a live one. A time ahead of the message is
// ignored, as the clocks disagree.
func lastSeen(payload map[string]any, received time.Time) time.Time {
	var seen time.Time

	switch v := payload["last_seen"].(type) {
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return received
		}

		seen = t
	case float64:
		seen = time.UnixMilli(int64(v))
	default:
		return received
	}

	if seen.After(received) {
		return received
	}

	return seen
}
