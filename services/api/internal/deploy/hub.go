package deploy

import "sync"

// EventKind names a live update.
type EventKind string

// The live updates api publishes.
const (
	EventDeployment EventKind = "deployment"
	EventOperation  EventKind = "operation"
	// EventModules is a watched deployment's module state that changed.
	EventModules EventKind = "modules"
)

// Event is one live update about a deployment.
type Event struct {
	Kind       EventKind
	Deployment string
	Operation  *Operation    // EventOperation
	Summary    *Deployment   // EventDeployment
	Modules    []ModuleState // EventModules
}

// subscriberBuffer is how many events a slow stream may lag before it is dropped.
const subscriberBuffer = 64

// Hub fans events out to the open event streams. It never blocks a publisher: a stream that
// falls behind is closed, and its reconnect gets a fresh snapshot.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub { return &Hub{subs: map[chan Event]struct{}{}} }

// Subscribe returns a channel of every later event and a func that ends the subscription. The
// channel closes when the subscription ends or falls behind.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() { h.drop(ch) }
}

func (h *Hub) drop(ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
}

// Publish sends e to every subscriber.
func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}
