package z2m

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/mqtt"
)

// quietWindow is how long to keep collecting after the receiver's first reply to a /get, since
// Zigbee2MQTT publishes once per attribute read.
const quietWindow = time.Second

// Session is a connection to a single receiver through Zigbee2MQTT.
type Session struct {
	// Receiver is the receiver being controlled.
	Receiver hive.Receiver

	// State is the attributes reported by the receiver so far.
	State map[string]any

	// client is the broker connection.
	client *mqtt.Client

	// topic is the receiver's Zigbee2MQTT topic.
	topic string
}

// Open connects to the broker and finds the receiver to control.
func Open(ctx context.Context, opts *Options) (*Session, error) {
	c, err := mqtt.Dial(opts.Broker, opts.Username, opts.Password, time.Now().Add(timeout))
	if err != nil {
		return nil, err
	}

	devices, err := Devices(ctx, c)
	if err != nil {
		c.Close()
		return nil, err
	}

	r, err := pickReceiver(devices)
	if err != nil {
		c.Close()
		return nil, err
	}

	s := &Session{Receiver: r, State: make(map[string]any), client: c, topic: baseTopic + "/" + r.Name}

	err = c.Subscribe(s.topic)
	if err != nil {
		s.Close()
		return nil, err
	}

	return s, nil
}

// Close closes the connection.
func (s *Session) Close() {
	s.client.Close()
}

// absorb merges a state message from the receiver into the session state, returning the
// properties it contained. Retained messages are ignored: they replay a cached state, which could
// satisfy a read or an action before the receiver has answered.
func (s *Session) absorb(m mqtt.Message) ([]string, bool) {
	if m.Topic != s.topic || m.Retained {
		return nil, false
	}

	var obj map[string]any

	if json.Unmarshal(m.Payload, &obj) != nil {
		return nil, false
	}

	props := make([]string, 0, len(obj))

	for p, v := range obj {
		s.State[p] = v
		props = append(props, p)
	}

	return props, true
}

// publish sends a payload to a subtopic of the receiver.
func (s *Session) publish(suffix string, p hive.Payload) error {
	b, err := p.Bytes()
	if err != nil {
		return err
	}

	return s.client.Publish(s.topic+"/"+suffix, b)
}

// reading tracks which of the requested properties the receiver has reported.
type reading struct {
	wanted   []string
	seen     map[string]bool
	complete bool
}

// record notes the reported properties, and whether every requested property has now been seen.
func (r *reading) record(reported []string) {
	for _, p := range reported {
		r.seen[p] = true
	}

	all := true
	for _, p := range r.wanted {
		all = all && r.seen[p]
	}

	r.complete = r.complete || all
}

// Read asks the receiver for the given properties and waits for the answers.
func (s *Session) Read(ctx context.Context, props []string) error {
	err := s.publish("get", hive.Get(props))
	if err != nil {
		return err
	}

	r := reading{wanted: props, seen: make(map[string]bool)}

	for {
		done, err := s.readNext(ctx, &r)
		if done {
			return err
		}
	}
}

// readNext processes the next message, returning done once reading has finished.
func (s *Session) readNext(ctx context.Context, r *reading) (bool, error) {
	var wait time.Duration
	if r.complete {
		wait = quietWindow
	}

	m, err := s.client.Next(ctx, wait)

	if err != nil {
		return true, s.readError(r, err)
	}

	reported, ok := s.absorb(m)
	if ok {
		r.record(reported)
	}

	return false, nil
}

// confirmError explains why an action could not be confirmed.
func (s *Session) confirmError(a hive.Action, err error) error {
	if !errors.Is(err, mqtt.ErrTimeout) {
		return err
	}

	return fmt.Errorf(
		"sent, but receiver %q did not confirm within %s (%s); check the Zigbee2MQTT log",
		s.Receiver.Name, timeout, a.Desc,
	)
}

// readError explains why reading from the receiver stopped. It returns nil when the read finished.
func (s *Session) readError(r *reading, err error) error {
	switch {
	case errors.Is(err, mqtt.ErrQuiet):
		return nil
	case errors.Is(err, mqtt.ErrTimeout) && r.complete:
		return nil
	case errors.Is(err, mqtt.ErrTimeout):
		return fmt.Errorf("no reply from receiver %q within %s: is it powered and paired?", s.Receiver.Name, timeout)
	}

	return err
}

// Apply sends the action and waits until the receiver reports the new state.
func (s *Session) Apply(ctx context.Context, a hive.Action) error {
	for _, step := range a.Steps {
		err := s.publish("set", step.Payload)
		if err != nil {
			return err
		}

		select {
		case <-time.After(step.Pause):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return s.verify(ctx, a)
}

// verify waits until the receiver reports that the action took effect.
func (s *Session) verify(ctx context.Context, a hive.Action) error {
	err := s.publish("get", hive.Get(a.Verify))
	if err != nil {
		return err
	}

	for {
		done, err := s.confirmNext(ctx, a)
		if done {
			return err
		}
	}
}

// confirmNext processes the next message, returning done once the action is confirmed or has
// failed.
func (s *Session) confirmNext(ctx context.Context, a hive.Action) (bool, error) {
	m, err := s.client.Next(ctx, 0)
	if err != nil {
		return true, s.confirmError(a, err)
	}

	_, ok := s.absorb(m)
	if !ok || !a.Done(s.State) {
		return false, nil
	}

	fmt.Println(a.Desc)

	return true, nil
}
