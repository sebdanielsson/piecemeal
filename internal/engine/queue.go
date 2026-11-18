package engine

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/settings"
)

// Queueing: with it on, only a limited number of torrents run at once and
// the rest wait, queued, outside the client. Downloads take turns by queue
// position, seeds by when they completed. Forced torrents always run and
// don't count. Positions number the incomplete torrents from 1; complete
// ones have 0.

// normalizeQueueLocked renumbers the incomplete torrents 1..n in their
// current order, new ones (position 0) last.
func (e *Engine) normalizeQueueLocked() {
	var incomplete []*managed
	for _, m := range e.torrents {
		if m.info != nil && m.complete() {
			if m.rec.QueuePosition != 0 {
				m.rec.QueuePosition = 0
				m.dirty = true
			}
			continue
		}
		incomplete = append(incomplete, m)
	}
	slices.SortFunc(incomplete, func(a, b *managed) int {
		pa, pb := a.rec.QueuePosition, b.rec.QueuePosition
		switch {
		case pa == 0 && pb != 0:
			return 1
		case pb == 0 && pa != 0:
			return -1
		}
		return cmp.Or(cmp.Compare(pa, pb), cmp.Compare(a.rec.AddedAt, b.rec.AddedAt), cmp.Compare(a.rec.InfoHash, b.rec.InfoHash))
	})
	for i, m := range incomplete {
		if pos := int64(i + 1); m.rec.QueuePosition != pos {
			m.rec.QueuePosition = pos
			m.dirty = true
		}
	}
}

// QueueMove moves torrents in the download queue: "top", "bottom", "up" or
// "down".
func (e *Engine) QueueMove(ctx context.Context, how string, hashes ...string) error {
	e.mu.Lock()
	e.normalizeQueueLocked()
	queue := e.queueLocked()
	selected := map[*managed]bool{}
	for _, h := range hashes {
		if h == "all" {
			for _, m := range queue {
				selected[m] = true
			}
			continue
		}
		if m, err := e.get(h); err == nil {
			selected[m] = true
		}
	}
	switch how {
	case "top":
		slices.SortStableFunc(queue, func(a, b *managed) int { return boolOrder(!selected[a], !selected[b]) })
	case "bottom":
		slices.SortStableFunc(queue, func(a, b *managed) int { return boolOrder(selected[a], selected[b]) })
	case "up":
		for i := 1; i < len(queue); i++ {
			if selected[queue[i]] && !selected[queue[i-1]] {
				queue[i], queue[i-1] = queue[i-1], queue[i]
			}
		}
	case "down":
		for i := len(queue) - 2; i >= 0; i-- {
			if selected[queue[i]] && !selected[queue[i+1]] {
				queue[i], queue[i+1] = queue[i+1], queue[i]
			}
		}
	}
	for i, m := range queue {
		if pos := int64(i + 1); m.rec.QueuePosition != pos {
			m.rec.QueuePosition = pos
			m.dirty = true
		}
	}
	e.scheduleLocked(time.Now())
	e.refreshLocked(queue...)
	e.mu.Unlock()
	return e.checkpoint(ctx, false)
}

// boolOrder sorts false before true.
func boolOrder(a, b bool) int {
	switch {
	case a == b:
		return 0
	case !a:
		return -1
	default:
		return 1
	}
}

// queueLocked returns the incomplete torrents by queue position.
func (e *Engine) queueLocked() []*managed {
	var q []*managed
	for _, m := range e.torrents {
		if m.rec.QueuePosition > 0 {
			q = append(q, m)
		}
	}
	slices.SortFunc(q, func(a, b *managed) int { return cmp.Compare(a.rec.QueuePosition, b.rec.QueuePosition) })
	return q
}

// scheduleLocked starts and queues torrents to keep within the limits, or
// starts every runnable torrent when queueing is off.
func (e *Engine) scheduleLocked(now time.Time) {
	s := e.settings.Get()
	var downloads, seeds []*managed
	active := map[*managed]bool{}
	for _, m := range e.torrents {
		if m.rec.Stopped || m.moving || m.rec.Error != "" {
			m.queued = false
			continue
		}
		e.updateSlow(m, s, now)
		switch {
		case !s.QueueingEnabled, m.rec.Forced:
			active[m] = true
		case m.info != nil && m.complete():
			seeds = append(seeds, m)
		default:
			downloads = append(downloads, m)
		}
	}
	if s.QueueingEnabled {
		slices.SortFunc(downloads, func(a, b *managed) int {
			return cmp.Or(cmp.Compare(a.rec.QueuePosition, b.rec.QueuePosition), cmp.Compare(a.rec.AddedAt, b.rec.AddedAt))
		})
		slices.SortFunc(seeds, func(a, b *managed) int {
			return cmp.Or(cmp.Compare(a.rec.CompletedAt, b.rec.CompletedAt), cmp.Compare(a.rec.AddedAt, b.rec.AddedAt))
		})
		under := func(limit, n int) bool { return limit < 0 || n < limit }
		total := 0
		for _, group := range []struct {
			ms    []*managed
			limit int
		}{{downloads, s.MaxActiveDownloads}, {seeds, s.MaxActiveUploads}} {
			n := 0
			for _, m := range group.ms {
				if s.DontCountSlowTorrents && m.slow {
					active[m] = true
					continue
				}
				if under(group.limit, n) && under(s.MaxActiveTorrents, total) {
					active[m] = true
					n++
					total++
				}
			}
		}
	}
	for _, m := range e.torrents {
		if m.rec.Stopped || m.moving || m.rec.Error != "" {
			continue
		}
		if active[m] {
			m.queued = false
			if m.t == nil {
				if err := e.attachLocked(m); err != nil {
					m.setError(err)
				}
			}
			continue
		}
		if !m.queued {
			e.log.Info("torrent queued", "hash", m.rec.InfoHash, "name", m.rec.Name)
		}
		m.queued = true
		e.detachLocked(m)
	}
}

// updateSlow tracks whether a running torrent has been below the slow
// rates for long enough to count as slow.
func (e *Engine) updateSlow(m *managed, s settings.Settings, now time.Time) {
	if m.t == nil || m.rates.downR >= float64(s.SlowDownloadRate) || m.rates.upR >= float64(s.SlowUploadRate) {
		m.slowSince, m.slow = time.Time{}, false
		return
	}
	if m.slowSince.IsZero() {
		m.slowSince = now
	}
	m.slow = now.Sub(m.slowSince) >= time.Duration(s.SlowTorrentTime)*time.Second
}

// applySchedule turns the alternative limits on and off at the edges of
// the schedule's window. Only the edges: a manual toggle inside the window
// stays until the next one.
func (e *Engine) applySchedule(now time.Time) {
	s := e.settings.Get()
	if !s.AltSpeedSchedule {
		e.lastWindow = nil
		return
	}
	in := s.InAltSpeedWindow(now)
	if e.lastWindow != nil && *e.lastWindow == in {
		return
	}
	e.lastWindow = &in
	if _, locked := e.settings.Locked()["altSpeedEnabled"]; locked || s.AltSpeedEnabled == in {
		return
	}
	if _, err := e.settings.Set(e.ctxOrBackground(), map[string]any{"altSpeedEnabled": in}); err != nil {
		e.log.Error("alternative speed schedule", "error", err)
		return
	}
	e.log.Info("alternative speed limits switched by schedule", "on", in)
}
