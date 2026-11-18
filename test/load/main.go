// Command load measures piecemeal with many torrents: it writes data for n
// single-file torrents, adds them with the data in place, waits for
// piecemeal to verify them, and then measures memory, idle CPU and the
// latency of the list endpoints the web UI and the *arrs poll.
//
//	go run ./test/load -url http://localhost:8080 -key pm_... -dir /downloads/load -n 1000
//
// The data directory must be inside piecemeal's allowed roots. When
// piecemeal sees it under another path, as in a container, pass that path
// as -server-dir. The key needs full scope. With -keep the torrents stay,
// so piecemeal can be restarted and measured again with -measure.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

const tag = "loadtest"

type client struct {
	base string
	key  string
	http *http.Client
}

func main() {
	var (
		base      = flag.String("url", "http://localhost:8080", "piecemeal's address")
		key       = flag.String("key", os.Getenv("PIECEMEAL_API_KEY"), "full-scope API key (or PIECEMEAL_API_KEY)")
		n         = flag.Int("n", 1000, "torrents")
		size      = flag.Int64("size", 1<<20, "bytes per torrent")
		pieceSize = flag.Int64("piece", 256<<10, "piece size")
		dir       = flag.String("dir", "", "where to write the data, inside piecemeal's allowed roots")
		serverDir = flag.String("server-dir", "", "the same directory as piecemeal sees it, when that differs")
		workers   = flag.Int("workers", 8, "concurrent adds")
		idle      = flag.Duration("idle", 30*time.Second, "how long to measure idle CPU")
		keep      = flag.Bool("keep", false, "leave the torrents and data in place")
		measure   = flag.Bool("measure", false, "only measure the torrents already added, as after a restart")
	)
	flag.Parse()
	if *key == "" || (*dir == "" && !*measure) {
		flag.Usage()
		os.Exit(2)
	}
	if *serverDir == "" {
		*serverDir = *dir
	}
	c := &client{base: strings.TrimRight(*base, "/"), key: *key, http: &http.Client{Timeout: 2 * time.Minute}}
	if err := run(context.Background(), c, *n, *size, *pieceSize, *dir, *serverDir, *workers, *idle, *keep, *measure); err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, c *client, n int, size, pieceSize int64, dir, serverDir string, workers int, idle time.Duration, keep, measure bool) error {
	report := &report{}
	before, err := c.metrics(ctx)
	if err != nil {
		return fmt.Errorf("metrics (set PIECEMEAL_METRICS to auth or public): %w", err)
	}
	report.add("Memory before", memory(before))

	if !measure {
		start := time.Now()
		torrents, err := generate(dir, n, size, pieceSize)
		if err != nil {
			return err
		}
		report.add("Data written and hashed by this tool", time.Since(start).Round(time.Millisecond).String())

		start = time.Now()
		lat, err := c.addAll(ctx, torrents, serverDir, workers)
		if err != nil {
			return err
		}
		elapsed := time.Since(start)
		report.add("Adding", fmt.Sprintf("%s, %.0f torrents/s", elapsed.Round(time.Millisecond), float64(n)/elapsed.Seconds()))
		report.add("Add request", lat.String())
	}

	start := time.Now()
	count, err := c.waitComplete(ctx, 30*time.Minute)
	if err != nil {
		return err
	}
	report.add("Torrents", strconv.Itoa(count))
	report.add("Until all are verified and seeding", time.Since(start).Round(time.Millisecond).String())

	cpu0, err := c.metrics(ctx)
	if err != nil {
		return err
	}
	t0 := time.Now()
	time.Sleep(idle)
	cpu1, err := c.metrics(ctx)
	if err != nil {
		return err
	}
	used := cpu1["process_cpu_seconds_total"] - cpu0["process_cpu_seconds_total"]
	report.add("Idle CPU", fmt.Sprintf("%.1f%% of one core over %s", 100*used/time.Since(t0).Seconds(), idle))
	report.add("Memory with the torrents", memory(cpu1))
	report.add("Go heap in use", mib(cpu1["go_memstats_heap_inuse_bytes"]))
	report.add("Goroutines", fmt.Sprintf("%.0f", cpu1["go_goroutines"]))

	for _, ep := range []struct{ name, path string }{
		{"GET /api/piecemeal/v1/torrents", "/api/piecemeal/v1/torrents?tag=" + tag},
		{"GET /api/v2/torrents/info", "/api/v2/torrents/info?tag=" + tag},
		{"GET /api/v2/sync/maindata, full", "/api/v2/sync/maindata?rid=0"},
	} {
		lat, bytes, err := c.timeGET(ctx, ep.path, 10)
		if err != nil {
			return err
		}
		report.add(ep.name, fmt.Sprintf("%s, %s", lat, kib(bytes)))
	}
	lat, bytes, err := c.timeIncremental(ctx, 10)
	if err != nil {
		return err
	}
	report.add("GET /api/v2/sync/maindata, incremental", fmt.Sprintf("%s, %s", lat, kib(bytes)))

	if !keep && !measure {
		start := time.Now()
		if err := c.removeAll(ctx); err != nil {
			return err
		}
		report.add("Removing them with their data", time.Since(start).Round(time.Millisecond).String())
		_ = os.RemoveAll(dir)
	}
	report.print(os.Stdout)
	return nil
}

// generate writes n files of random data and returns their torrent files.
func generate(dir string, n int, size, pieceSize int64) ([][]byte, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	out := make([][]byte, n)
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			mi, err := generateOne(dir, i, size, pieceSize)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			out[i] = mi
		}()
	}
	wg.Wait()
	return out, firstErr
}

func generateOne(dir string, i int, size, pieceSize int64) ([]byte, error) {
	name := fmt.Sprintf("load-%05d.bin", i)
	path := filepath.Join(dir, name)
	data := make([]byte, size)
	var seed [32]byte
	binary.LittleEndian.PutUint64(seed[:], uint64(i)) //nolint:gosec // i is a non-negative index
	rng := rand.NewChaCha8(seed)
	_, _ = rng.Read(data)
	if err := os.WriteFile(path, data, 0o640); err != nil { //nolint:gosec // a test file
		return nil, err
	}
	info := metainfo.Info{PieceLength: pieceSize}
	if err := info.BuildFromFilePath(path); err != nil {
		return nil, err
	}
	b, err := bencode.Marshal(info)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := (&metainfo.MetaInfo{InfoBytes: b, CreatedBy: "piecemeal load test"}).Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *client) addAll(ctx context.Context, torrents [][]byte, savePath string, workers int) (latencies, error) {
	var mu sync.Mutex
	var lat latencies
	var firstErr error
	var wg sync.WaitGroup
	next := make(chan []byte)
	for range workers {
		wg.Go(func() {
			for mi := range next {
				body, _ := json.Marshal(map[string]any{"torrentFile": mi, "savePath": savePath, "tags": []string{tag}})
				start := time.Now()
				err := c.do(ctx, http.MethodPost, "/api/piecemeal/v1/torrents", body, nil)
				mu.Lock()
				lat = append(lat, time.Since(start))
				if err != nil && firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		})
	}
	for _, mi := range torrents {
		next <- mi
	}
	close(next)
	wg.Wait()
	return lat, firstErr
}

type torrentList struct {
	Items []struct {
		State    string `json:"state"`
		Complete bool   `json:"complete"`
	} `json:"items"`
}

// waitComplete waits until every load-test torrent is complete and
// seeding, and returns how many there are.
func (c *client) waitComplete(ctx context.Context, limit time.Duration) (int, error) {
	deadline := time.Now().Add(limit)
	for {
		var list torrentList
		if err := c.do(ctx, http.MethodGet, "/api/piecemeal/v1/torrents?tag="+tag, nil, &list); err != nil {
			return 0, err
		}
		done := 0
		for _, t := range list.Items {
			if t.Complete && (t.State == "seeding" || t.State == "stalledSeeding") {
				done++
			}
		}
		if len(list.Items) > 0 && done == len(list.Items) {
			return done, nil
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("%d of %d torrents verified after %s", done, len(list.Items), limit)
		}
		time.Sleep(time.Second)
	}
}

func (c *client) removeAll(ctx context.Context) error {
	var list struct {
		Items []struct {
			Hash string `json:"hash"`
		} `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/piecemeal/v1/torrents?tag="+tag, nil, &list); err != nil {
		return err
	}
	hashes := make([]string, 0, len(list.Items))
	for _, t := range list.Items {
		hashes = append(hashes, t.Hash)
	}
	if len(hashes) == 0 {
		return nil
	}
	body, _ := json.Marshal(map[string]any{"hashes": hashes, "action": "remove", "deleteData": true})
	return c.do(ctx, http.MethodPost, "/api/piecemeal/v1/torrents/actions", body, nil)
}

// pollInterval spaces the timed requests as clients poll, which also keeps
// a CPU that has slowed down between them in the measurement.
const pollInterval = time.Second

func (c *client) timeGET(ctx context.Context, path string, times int) (latencies, int, error) {
	var lat latencies
	size := 0
	for range times {
		time.Sleep(pollInterval)
		start := time.Now()
		b, err := c.get(ctx, path)
		if err != nil {
			return nil, 0, err
		}
		lat = append(lat, time.Since(start))
		size = len(b)
	}
	return lat, size, nil
}

// timeIncremental times sync/maindata as the *arrs and web UIs poll it:
// with the rid of the previous response.
func (c *client) timeIncremental(ctx context.Context, times int) (latencies, int, error) {
	b, err := c.get(ctx, "/api/v2/sync/maindata?rid=0")
	if err != nil {
		return nil, 0, err
	}
	var lat latencies
	size := 0
	for range times {
		var resp struct {
			RID int `json:"rid"`
		}
		if err := json.Unmarshal(b, &resp); err != nil {
			return nil, 0, err
		}
		time.Sleep(pollInterval)
		start := time.Now()
		if b, err = c.get(ctx, "/api/v2/sync/maindata?rid="+strconv.Itoa(resp.RID)); err != nil {
			return nil, 0, err
		}
		lat = append(lat, time.Since(start))
		size = len(b)
	}
	return lat, size, nil
}

// metrics reads the numeric samples without labels from /metrics.
func (c *client) metrics(ctx context.Context) (map[string]float64, error) {
	b, err := c.get(ctx, "/metrics")
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for line := range strings.SplitSeq(string(b), "\n") {
		name, value, ok := strings.Cut(line, " ")
		if !ok || strings.HasPrefix(line, "#") || strings.Contains(name, "{") {
			continue
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			out[name] = v
		}
	}
	return out, nil
}

func (c *client) get(ctx context.Context, path string) ([]byte, error) {
	var raw json.RawMessage
	if strings.HasPrefix(path, "/metrics") {
		return c.raw(ctx, http.MethodGet, path, nil)
	}
	err := c.do(ctx, http.MethodGet, path, nil, &raw)
	return raw, err
}

func (c *client) do(ctx context.Context, method, path string, body []byte, out any) error {
	b, err := c.raw(ctx, method, path, body)
	if err != nil || out == nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (c *client) raw(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	u, err := url.Parse(c.base + path)
	if err != nil {
		return nil, err
	}
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, bytes.TrimSpace(b))
	}
	return b, nil
}

type latencies []time.Duration

func (l latencies) String() string {
	if len(l) == 0 {
		return "none"
	}
	s := slices.Clone(l)
	slices.Sort(s)
	q := func(p float64) time.Duration {
		return s[min(len(s)-1, int(p*float64(len(s))))].Round(100 * time.Microsecond)
	}
	return fmt.Sprintf("p50 %s, p95 %s, max %s", q(0.5), q(0.95), s[len(s)-1].Round(100*time.Microsecond))
}

type report struct{ rows [][2]string }

func (r *report) add(k, v string) { r.rows = append(r.rows, [2]string{k, v}) }

func (r *report) print(w io.Writer) {
	_, _ = fmt.Fprintln(w, "| Measure | Result |")
	_, _ = fmt.Fprintln(w, "| --- | --- |")
	for _, row := range r.rows {
		_, _ = fmt.Fprintf(w, "| %s | %s |\n", row[0], row[1])
	}
}

// memory is the resident set size, or where the process collector can't
// read it (macOS), the memory the Go runtime got from the OS.
func memory(m map[string]float64) string {
	if rss := m["process_resident_memory_bytes"]; rss > 0 {
		return mib(rss) + " resident"
	}
	return mib(m["go_memstats_sys_bytes"]) + " from the OS (Go runtime)"
}

func mib(b float64) string { return fmt.Sprintf("%.0f MiB", b/(1<<20)) }

func kib(b int) string { return fmt.Sprintf("%.0f KiB", float64(b)/(1<<10)) }
