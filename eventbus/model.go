package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
)

// Func is a handler registered for a (domain, action) pair and invoked when a
// matching event is dispatched. It runs synchronously on the dispatching
// goroutine; returning an error is surfaced to the producer.
type Func func(ctx context.Context, data Data) error

// Data is a single event passed between domains. Params are opaque bytes
// (JSON by convention) so the bus stays decoupled from any concrete type: the
// producer encodes, the consumer decodes, and neither needs the other's struct
// beyond the shared contract.
type Data struct {
	Domain    string
	Action    string
	RawParams []byte
}

// String implements fmt.Stringer.
func (d Data) String() string {
	return fmt.Sprintf("eventbus.Data{Domain:%q, Action:%q, RawParams:%s}",
		d.Domain, d.Action, d.RawParams)
}

// NewData builds an event for the given domain and action, JSON-encoding params
// into RawParams. Pass nil params for an event that carries no payload.
func NewData(domain, action string, params any) (Data, error) {
	var raw []byte
	if params != nil {
		var err error
		if raw, err = json.Marshal(params); err != nil {
			return Data{}, fmt.Errorf("eventbus: marshal %s/%s params: %w", domain, action, err)
		}
	}
	return Data{Domain: domain, Action: action, RawParams: raw}, nil
}

// MustData is NewData that panics on a marshal error. Use it for static param
// types that cannot fail to encode (the common case), where an error would
// indicate a programming mistake rather than a runtime condition.
func MustData(domain, action string, params any) Data {
	d, err := NewData(domain, action, params)
	if err != nil {
		panic(err)
	}
	return d
}

// Decode unmarshals an event's JSON params into T. Handlers use it to recover
// the typed payload the producer encoded with NewData/MustData.
func Decode[T any](d Data) (T, error) {
	var out T
	if len(d.RawParams) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(d.RawParams, &out); err != nil {
		return out, fmt.Errorf("eventbus: decode %s/%s params into %T: %w", d.Domain, d.Action, out, err)
	}
	return out, nil
}
