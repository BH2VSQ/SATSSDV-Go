package main

import (
	"sync"
	"time"
)

// AppEvent is an immutable event snapshot delivered to the native Qt client.
type AppEvent struct {
	Sequence uint64      `json:"sequence"`
	Name     string      `json:"name"`
	Payload  interface{} `json:"payload,omitempty"`
	At       time.Time   `json:"at"`
}

// EventBroker keeps a bounded circular event history so a short-lived UI
// polling gap cannot stall audio/RX workers. Publishing overwrites the oldest
// slot in O(1) time; display telemetry is throttled by App before publishing.
type EventBroker struct {
	mu      sync.Mutex
	next    uint64
	maximum int
	start   int
	count   int
	events  []AppEvent
}

func NewEventBroker() *EventBroker {
	return &EventBroker{maximum: 512, events: make([]AppEvent, 512)}
}

func (b *EventBroker) Publish(name string, data interface{}) {
	if b == nil || name == "" {
		return
	}
	b.mu.Lock()
	b.next++
	event := AppEvent{Sequence: b.next, Name: name, Payload: data, At: time.Now().UTC()}
	if b.maximum <= 0 || len(b.events) != b.maximum {
		b.maximum = 512
		b.start = 0
		b.count = 0
		b.events = make([]AppEvent, b.maximum)
	}
	if b.count < b.maximum {
		index := (b.start + b.count) % b.maximum
		b.events[index] = event
		b.count++
	} else {
		b.events[b.start] = event
		b.start = (b.start + 1) % b.maximum
	}
	b.mu.Unlock()
}

func (b *EventBroker) Since(sequence uint64) ([]AppEvent, uint64, bool) {
	if b == nil {
		return nil, sequence, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.count == 0 {
		return nil, b.next, false
	}
	oldest := b.events[b.start].Sequence
	reset := sequence+1 < oldest
	if reset {
		sequence = oldest - 1
	}
	out := make([]AppEvent, 0, b.count)
	for i := 0; i < b.count; i++ {
		event := b.events[(b.start+i)%b.maximum]
		if event.Sequence > sequence {
			out = append(out, event)
		}
	}
	return out, b.next, reset
}

func (a *App) emit(name string, payload interface{}) {
	if a != nil && a.events != nil {
		a.events.Publish(name, payload)
	}
}
