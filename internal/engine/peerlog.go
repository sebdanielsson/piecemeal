package engine

import (
	"net"
	"slices"
	"sync"
	"time"
)

// PeerLogEntry is an address the IP filter refused, or a banned one: what
// qBittorrent's log/peers reports.
type PeerLogEntry struct {
	ID      int64
	IP      string
	Time    time.Time
	Blocked bool // refused by the IP filter; false for a banned address
	Reason  string
}

const (
	peerLogSize = 1000
	// peerLogQuiet is how long the same address isn't logged again: a
	// refused peer is offered again and again by trackers and the DHT.
	peerLogQuiet = time.Minute
)

type peerLog struct {
	mu      sync.Mutex
	next    int64
	entries []PeerLogEntry
	last    map[string]time.Time
	now     func() time.Time
}

func newPeerLog() *peerLog {
	return &peerLog{last: map[string]time.Time{}, now: time.Now}
}

// add records a refused address. desc is the filter range's description,
// "banned" for banned addresses.
func (l *peerLog) add(ip net.IP, desc string) {
	key := ip.String()
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if t, ok := l.last[key]; ok && now.Sub(t) < peerLogQuiet {
		return
	}
	if len(l.last) >= 4*peerLogSize {
		clear(l.last)
	}
	l.last[key] = now
	e := PeerLogEntry{ID: l.next, IP: key, Time: now, Blocked: true, Reason: "IP filter"}
	switch desc {
	case "banned":
		e.Blocked, e.Reason = false, "banned"
	case "":
	default:
		e.Reason = "IP filter: " + desc
	}
	l.next++
	l.entries = append(l.entries, e)
	if len(l.entries) > peerLogSize {
		l.entries = slices.Delete(l.entries, 0, len(l.entries)-peerLogSize)
	}
}

// PeerLog returns the refused and banned addresses logged after the entry
// with ID after, oldest first; -1 returns them all.
func (e *Engine) PeerLog(after int64) []PeerLogEntry {
	l := e.filter.log
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	i, _ := slices.BinarySearchFunc(l.entries, after+1, func(e PeerLogEntry, id int64) int {
		switch {
		case e.ID < id:
			return -1
		case e.ID > id:
			return 1
		}
		return 0
	})
	return slices.Clone(l.entries[i:])
}
