package engine

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

// Checking: verifying a torrent's data reads all of it, and on hard drives
// several checks at once make the disks seek between torrents. So at most
// maxActiveCheckingTorrents torrents are checked at once, like qBittorrent's
// max_active_checking_torrents. The rest wait outside the client, queued for
// checking, so they neither download nor seed until their data is known.
//
// anacrolix hashes every piece of unknown completion as soon as a torrent has
// its info and storage, so a torrent that needs a check is only added to the
// client once it has a slot. A torrent needs a check when:
//
//   - some of its pieces have unknown completion and its files hold data:
//     after an import without --trust-data, after losing the piece
//     completion database, or when adding a torrent whose data is already on
//     disk. Without data there is nothing to read and the check takes no
//     time, so such torrents start at once, as in qBittorrent.
//   - it is rechecked: then every piece is hashed again, and a stopped
//     torrent is checked too and stays stopped.
//
// A stopped torrent that needs a check waits until it is started. While a
// torrent is checked it is in the client with data download and upload
// disallowed, so it requests nothing and serves nothing until the check is
// done.

type checkState uint8

const (
	checkNone checkState = iota
	// checkPending: waiting for a slot, outside the client.
	checkPending
	// checkActive: holds a slot and is being checked.
	checkActive
)

// checkPollInterval is how often a check is looked at even without news
// from anacrolix, in case some never came.
const checkPollInterval = time.Second

// waitingForCheck reports whether a torrent may take a check slot: it needs a
// check and nothing else holds it back.
func (m *managed) waitingForCheck() bool {
	return m.check == checkPending && m.info != nil && !m.moving && m.rec.Error == "" &&
		(!m.rec.Stopped || m.checkAll)
}

// queueCheckLocked marks a torrent to be checked before it runs; all hashes
// every piece instead of only those of unknown completion. A torrent already
// waiting keeps its place.
func (e *Engine) queueCheckLocked(m *managed, all bool) {
	if m.check == checkNone {
		e.checkSeq++
		m.checkSeq = e.checkSeq
		m.check = checkPending
	}
	if all {
		m.checkAll = true
	}
}

// needsCheckLocked reports whether a torrent's data has to be checked before
// it runs: some pieces' completion is unknown and its files hold data. It
// also returns the verified bytes, for the status while it waits.
func (e *Engine) needsCheckLocked(m *managed) (bool, int64) {
	if m.info == nil || !e.hasDataLocked(m) {
		return false, 0
	}
	done, unknown := e.completedFromStore(m)
	return unknown, done
}

// loadCompletionLocked reads how much of a torrent the completion database
// has verified, for while it's out of the client, and queues a check if
// pieces of unknown completion have data on disk. It reports whether it
// queued one.
func (e *Engine) loadCompletionLocked(m *managed) bool {
	if m.info == nil {
		return false
	}
	var unknown bool
	m.completed, unknown = e.completedFromStore(m)
	if unknown && e.hasDataLocked(m) {
		e.queueCheckLocked(m, false)
		return true
	}
	return false
}

// hasDataLocked reports whether any of a torrent's files exists and isn't
// empty, where its data is.
func (e *Engine) hasDataLocked(m *managed) bool {
	return m.hasDataIn(m.dataDir())
}

// hasDataIn reports whether any of a torrent's files exists in dir and
// isn't empty.
func (m *managed) hasDataIn(dir string) bool {
	paths := m.pathsOf()
	for _, rel := range paths.relativePaths(m.info) {
		fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		if err == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
			return true
		}
	}
	return false
}

// completedFromStore sums the pieces the completion database marks
// complete, for a torrent that isn't running, and reports whether any
// piece's completion is unknown.
func (e *Engine) completedFromStore(m *managed) (done int64, unknown bool) {
	for i := range m.info.NumPieces() {
		c, err := e.pc.Get(metainfo.PieceKey{InfoHash: m.hash, Index: i})
		switch {
		case err != nil || !c.Ok:
			unknown = true
		case c.Complete:
			done += m.info.Piece(i).Length()
		}
	}
	return done, unknown
}

// scheduleChecksLocked starts the torrents first in the check queue while
// slots are free. Checks already running when the limit is lowered finish.
func (e *Engine) scheduleChecksLocked() {
	if e.ctx == nil || e.ctx.Err() != nil {
		return // still loading, New schedules once it's ready; or closing
	}
	limit := max(e.settings.Get().MaxActiveCheckingTorrents, 1)
	active := 0
	var waiting []*managed
	for _, m := range e.torrents {
		switch {
		case m.check == checkActive:
			active++
		case m.waitingForCheck():
			waiting = append(waiting, m)
		}
	}
	if active >= limit || len(waiting) == 0 {
		return
	}
	slices.SortFunc(waiting, func(a, b *managed) int { return cmp.Compare(a.checkSeq, b.checkSeq) })
	var changed []*managed
	for _, m := range waiting {
		if active >= limit {
			break
		}
		if e.startCheckLocked(m) {
			active++
		}
		changed = append(changed, m)
	}
	e.refreshLocked(changed...)
}

// startCheckLocked adds a torrent to the client to be checked.
func (e *Engine) startCheckLocked(m *managed) bool {
	e.detachLocked(m)
	m.check = checkActive
	if err := e.attachLocked(m); err != nil {
		m.check = checkPending
		e.log.Error("starting a check", "hash", m.rec.InfoHash, "error", err)
		m.setError(err)
		return false
	}
	e.log.Info("checking torrent", "hash", m.rec.InfoHash, "name", m.rec.Name, "allPieces", m.checkAll)
	t := m.t
	e.wg.Go(func() { e.awaitCheck(m, t) })
	return true
}

// awaitCheck waits until every piece of a torrent being checked has known
// completion and none is waiting to be hashed, then ends the check.
// anacrolix has no event for that, so it looks again whenever a piece's
// state changes. That's cheap: pieces are hashed in order, and each look
// carries on from the first piece still unchecked.
func (e *Engine) awaitCheck(m *managed, t *torrent.Torrent) {
	// Subscribed before the first look, so no change after it goes unseen.
	changes := t.SubscribePieceStateChanges()
	defer changes.Close()
	tick := time.NewTicker(checkPollInterval)
	defer tick.Stop()
	n, next := t.NumPieces(), 0
	for {
		for next < n && pieceChecked(t.PieceState(next)) {
			next++
		}
		if next == n {
			// A piece passed over earlier could have been queued again.
			if next = firstUnchecked(t); next == n {
				break
			}
		}
		select {
		case <-changes.Values:
		case <-tick.C:
		case <-t.Closed():
			return
		case <-e.ctx.Done():
			return
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx.Err() != nil || e.torrents[m.hash] != m || m.t != t || m.check != checkActive {
		return // closing, or stopped, moved or removed meanwhile
	}
	e.finishCheckLocked(m)
}

// pieceChecked reports whether a piece's check is over: its completion is
// known and it isn't waiting to be hashed. A piece that can't be hashed,
// for want of a v2 hash or because the storage fails, counts as checked.
func pieceChecked(ps torrent.PieceState) bool {
	if ps.MissingPieceLayerHash || ps.Err != nil {
		return true
	}
	return ps.Ok && !ps.QueuedForHash && !ps.Hashing && !ps.Marking
}

func firstUnchecked(t *torrent.Torrent) int {
	i := 0
	for _, run := range t.PieceStateRuns() {
		if !pieceChecked(run.PieceState) {
			return i
		}
		i += run.Length
	}
	return i
}

// finishCheckLocked ends a torrent's check: it frees the slot for the next
// torrent, and the torrent runs as usual, or leaves the client if it is
// stopped.
func (e *Engine) finishCheckLocked(m *managed) {
	m.check, m.checkAll = checkNone, false
	e.log.Info("torrent checked", "hash", m.rec.InfoHash, "name", m.rec.Name, "complete", m.complete())
	if m.rec.Stopped {
		e.detachLocked(m)
	} else {
		m.t.AllowDataDownload()
		m.t.AllowDataUpload()
	}
	// Starts the next check, and holds this torrent back if the queue is
	// full.
	e.scheduleLocked(time.Now())
	e.refreshLocked(m)
}
