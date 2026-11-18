package engine

import "slices"

// PieceAvailability returns, for each piece, how many connected peers have
// it. It is nil when the torrent has no metadata or isn't running.
func (e *Engine) PieceAvailability(hash string) ([]int, error) {
	e.mu.Lock()
	m, err := e.get(hash)
	if err != nil {
		e.mu.Unlock()
		return nil, err
	}
	t := m.t
	e.mu.Unlock()
	if t == nil || t.Info() == nil {
		return nil, nil
	}
	n := t.NumPieces()
	counts := make([]int, n)
	for _, pc := range t.PeerConns() {
		it := pc.PeerPieces().Iterator()
		for it.HasNext() {
			i := int(it.Next())
			if i >= n {
				break
			}
			counts[i]++
		}
	}
	return counts, nil
}

// DistributedCopies is how many complete copies connected peers hold
// together: the rarest piece's count, plus the fraction of pieces more
// peers have, as libtorrent reports it.
func DistributedCopies(counts []int) float64 {
	if len(counts) == 0 {
		return 0
	}
	low := slices.Min(counts)
	above := 0
	for _, c := range counts {
		if c > low {
			above++
		}
	}
	return float64(low) + float64(above)/float64(len(counts))
}

// AvailableFraction is the share of pieces in [start, end) that at least
// one connected peer has.
func AvailableFraction(counts []int, start, end int) float64 {
	start, end = max(start, 0), min(end, len(counts))
	if end <= start {
		return 0
	}
	n := 0
	for _, c := range counts[start:end] {
		if c > 0 {
			n++
		}
	}
	return float64(n) / float64(end-start)
}
