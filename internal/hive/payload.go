package hive

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// KV is one property in an ordered payload.
type KV struct {
	K string
	V any
}

// Payload is an ordered set of properties. Hive is sensitive to the order in which attributes are
// written, and Zigbee2MQTT writes them in payload order, so payloads keep their order rather than
// using a Go map.
type Payload []KV

// MarshalJSONTo implements json.MarshalerTo, writing the properties in order.
func (p Payload) MarshalJSONTo(enc *jsontext.Encoder) error {
	err := enc.WriteToken(jsontext.BeginObject)
	if err != nil {
		return err
	}

	for _, kv := range p {
		err = kv.marshalTo(enc)
		if err != nil {
			return err
		}
	}

	return enc.WriteToken(jsontext.EndObject)
}

// marshalTo writes the property as an object member.
func (kv KV) marshalTo(enc *jsontext.Encoder) error {
	err := enc.WriteToken(jsontext.String(kv.K))
	if err != nil {
		return err
	}

	return json.MarshalEncode(enc, kv.V)
}

// Bytes returns the JSON encoding of the payload.
func (p Payload) Bytes() ([]byte, error) {
	return json.Marshal(p)
}

// Get creates a payload which requests the given keys from the receiver.
func Get(keys []string) Payload {
	p := make(Payload, 0, len(keys))

	for _, k := range keys {
		p = append(p, KV{k, ""})
	}

	return p
}
