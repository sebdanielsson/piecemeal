// Package metrics exports engine state to Prometheus. Values are read from
// the engine's snapshot at scrape time.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

// Source is what the collector reads; *engine.Engine implements it.
type Source interface {
	List() []engine.Torrent
	Transfer() engine.TransferStatus
}

// Collector is a prometheus.Collector for the engine.
type Collector struct{ src Source }

// NewCollector returns a collector reading src.
func NewCollector(src Source) *Collector { return &Collector{src: src} }

var (
	torrentLabels = []string{"hash", "name"}

	descTorrents    = prometheus.NewDesc("piecemeal_torrents", "Torrents by state.", []string{"state"}, nil)
	descPort        = prometheus.NewDesc("piecemeal_listen_port", "The port peers connect to.", nil, nil)
	descPeers       = prometheus.NewDesc("piecemeal_peers", "Connected peers across all torrents.", nil, nil)
	descDHT         = prometheus.NewDesc("piecemeal_dht_nodes", "Good DHT nodes.", nil, nil)
	descRate        = prometheus.NewDesc("piecemeal_rate_bytes", "Transfer rate in bytes per second.", []string{"direction"}, nil)
	descLimit       = prometheus.NewDesc("piecemeal_rate_limit_bytes", "Rate limit in effect in bytes per second, 0 for unlimited.", []string{"direction"}, nil)
	descSession     = prometheus.NewDesc("piecemeal_session_bytes_total", "Payload bytes transferred since start.", []string{"direction"}, nil)
	descFree        = prometheus.NewDesc("piecemeal_free_space_bytes", "Free space on the default save path.", nil, nil)
	descDownloaded  = prometheus.NewDesc("piecemeal_torrent_downloaded_bytes_total", "Payload bytes downloaded, all time.", torrentLabels, nil)
	descUploaded    = prometheus.NewDesc("piecemeal_torrent_uploaded_bytes_total", "Payload bytes uploaded, all time.", torrentLabels, nil)
	descCompleted   = prometheus.NewDesc("piecemeal_torrent_completed_bytes", "Wanted bytes downloaded and verified.", torrentLabels, nil)
	descSize        = prometheus.NewDesc("piecemeal_torrent_size_bytes", "Bytes in the wanted files.", torrentLabels, nil)
	descTorrentRate = prometheus.NewDesc("piecemeal_torrent_rate_bytes", "Transfer rate in bytes per second.", append(torrentLabels, "direction"), nil)
	descTorrentPeer = prometheus.NewDesc("piecemeal_torrent_peers", "Connected peers.", append(torrentLabels, "kind"), nil)
)

// Describe implements prometheus.Collector.
func (*Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{
		descTorrents, descPort, descPeers, descDHT, descRate, descLimit, descSession, descFree,
		descDownloaded, descUploaded, descCompleted, descSize, descTorrentRate, descTorrentPeer,
	} {
		ch <- d
	}
}

// Collect implements prometheus.Collector.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	tr := c.src.Transfer()
	gauge := func(d *prometheus.Desc, v float64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, labels...)
	}
	counter := func(d *prometheus.Desc, v float64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v, labels...)
	}
	gauge(descPort, float64(tr.Port))
	gauge(descPeers, float64(tr.Peers))
	gauge(descDHT, float64(tr.DHTNodes))
	gauge(descRate, float64(tr.DownRate), "down")
	gauge(descRate, float64(tr.UpRate), "up")
	gauge(descLimit, float64(tr.DownloadLimit), "down")
	gauge(descLimit, float64(tr.UploadLimit), "up")
	counter(descSession, float64(tr.SessionDownloaded), "down")
	counter(descSession, float64(tr.SessionUploaded), "up")
	if tr.FreeSpace >= 0 {
		gauge(descFree, float64(tr.FreeSpace))
	}

	states := map[engine.State]int{}
	for _, t := range c.src.List() {
		states[t.State]++
		l := []string{t.Hash, t.Name}
		counter(descDownloaded, float64(t.Downloaded), l...)
		counter(descUploaded, float64(t.Uploaded), l...)
		gauge(descCompleted, float64(t.Completed), l...)
		gauge(descSize, float64(t.Size), l...)
		gauge(descTorrentRate, float64(t.DownRate), append(l, "down")...)
		gauge(descTorrentRate, float64(t.UpRate), append(l, "up")...)
		gauge(descTorrentPeer, float64(t.Seeds), append(l, "seed")...)
		gauge(descTorrentPeer, float64(t.Peers), append(l, "leech")...)
	}
	for _, s := range []engine.State{
		engine.StateMetadata, engine.StateChecking, engine.StateDownloading, engine.StateStalledDownloading,
		engine.StateSeeding, engine.StateStalledSeeding, engine.StateStopped, engine.StateQueued,
		engine.StateMoving, engine.StateError,
	} {
		gauge(descTorrents, float64(states[s]), string(s))
	}
}
