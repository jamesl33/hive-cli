package hive

import "time"

// Step is one publish to the receiver's /set topic, optionally followed by a pause.
type Step struct {
	// Payload is the message to publish.
	Payload Payload

	// Pause is how long to wait after publishing.
	Pause time.Duration
}

// Action is a full change: the steps to send and how to recognize success in the receiver's
// reported state.
type Action struct {
	// Desc describes the change; it's printed once the change is confirmed.
	Desc string

	// Steps are the payloads to publish, in order.
	Steps []Step

	// Verify is the properties to read back afterwards.
	Verify []string

	// Done returns true once the state reflects the change.
	Done func(state map[string]any) bool
}

// modeIs returns a Done function which checks that the string property key equals want.
func modeIs(key, want string) func(map[string]any) bool {
	return func(s map[string]any) bool {
		v, _ := s[key].(string)
		return v == want
	}
}
