package engine

import (
	"sync"
)

// EventType says what an Event is about.
type EventType string

// Event types.
const (
	// EventTorrent carries a torrent's new status, after it was added or
	// anything about it changed.
	EventTorrent EventType = "torrent"
	// EventTorrentRemoved carries the hash of a removed torrent.
	EventTorrentRemoved EventType = "torrentRemoved"
	// EventTransfer carries the new transfer status.
	EventTransfer EventType = "transfer"
	// EventCategories means the categories changed.
	EventCategories EventType = "categories"
	// EventTags means the tags changed.
	EventTags EventType = "tags"
	// EventSettings means the settings changed.
	EventSettings EventType = "settings"
)

// Event is a change in the engine.
type Event struct {
	Type     EventType
	Torrent  *Torrent
	Hash     string
	Transfer *TransferStatus
}

// Bus fans engine events out to subscribers.
type Bus struct {
	mu     sync.Mutex
	subs   map[*Subscription]struct{}
	closed bool
}

// Subscription receives events until it is closed. A subscriber that falls
// more than its buffer behind is dropped: C is closed, and it should
// re-read the full state and subscribe again.
type Subscription struct {
	C   <-chan Event
	c   chan Event
	bus *Bus
}

func newBus() *Bus {
	return &Bus{subs: map[*Subscription]struct{}{}}
}

// Subscribe returns a subscription with room for buffer events.
func (b *Bus) Subscribe(buffer int) *Subscription {
	c := make(chan Event, buffer)
	s := &Subscription{C: c, c: c, bus: b}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(c)
		return s
	}
	b.subs[s] = struct{}{}
	return s
}

// Close ends the subscription.
func (s *Subscription) Close() {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	if _, ok := s.bus.subs[s]; ok {
		delete(s.bus.subs, s)
		close(s.c)
	}
}

func (b *Bus) publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		select {
		case s.c <- ev:
		default:
			delete(b.subs, s)
			close(s.c)
		}
	}
}

func (b *Bus) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for s := range b.subs {
		delete(b.subs, s)
		close(s.c)
	}
}
