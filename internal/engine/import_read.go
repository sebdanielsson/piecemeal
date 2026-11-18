package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent/bencode"
)

// bdict is a decoded bencoded dictionary.
type bdict map[string]any

func readBencode(path string) (bdict, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the admin's import directory
	if err != nil {
		return nil, err
	}
	var d map[string]any
	if err := bencode.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

func (d bdict) str(k string) string {
	switch v := d[k].(type) {
	case string:
		return v
	case []byte:
		return string(v)
	}
	return ""
}

func (d bdict) int(k string) int64 {
	if v, ok := d[k].(int64); ok {
		return v
	}
	return 0
}

func (d bdict) dict(k string) bdict {
	if v, ok := d[k].(map[string]any); ok {
		return v
	}
	return bdict{}
}

func (d bdict) list(k string) []any {
	v, _ := d[k].([]any)
	return v
}

func (d bdict) strings(k string) []string {
	var out []string
	for _, v := range d.list(k) {
		switch s := v.(type) {
		case string:
			out = append(out, s)
		case []byte:
			out = append(out, string(s))
		}
	}
	return out
}

func unixTime(n int64) time.Time {
	if n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// ReadTransmission reads the torrents of a Transmission configuration
// directory: torrents/*.torrent with their resume/*.resume. Labels become
// tags, and the first label also the category: Radarr and Sonarr label
// their downloads in Transmission with what is their category in
// qBittorrent.
func ReadTransmission(dir string) ([]Imported, []error) {
	resumes, err := filepath.Glob(filepath.Join(dir, "resume", "*.resume"))
	if err != nil {
		return nil, []error{err}
	}
	if len(resumes) == 0 {
		return nil, []error{fmt.Errorf("no resume files in %s; give Transmission's configuration directory", filepath.Join(dir, "resume"))}
	}
	var out []Imported
	var errs []error
	for _, rp := range resumes {
		base := strings.TrimSuffix(filepath.Base(rp), ".resume")
		tp := filepath.Join(dir, "torrents", base+".torrent")
		mi, err := os.ReadFile(tp) //nolint:gosec // the admin's import directory
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: no torrent file: %w", rp, err))
			continue
		}
		r, err := readBencode(rp)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		labels := r.strings("labels")
		it := Imported{
			Source: rp, Metainfo: mi, SavePath: r.str("destination"), Tags: labels,
			Stopped: r.int("paused") != 0, AddedAt: unixTime(r.int("added-date")), CompletedAt: unixTime(r.int("done-date")),
			Uploaded: r.int("uploaded"), Downloaded: r.int("downloaded"), SeedingSeconds: r.int("seeding-time-seconds"),
			ActiveSeconds: r.int("seeding-time-seconds") + r.int("downloading-time-seconds"),
			Sequential:    r.int("sequential_download") != 0, Paths: r.strings("files"),
			RatioLimit: -2, SeedingTimeLimit: -2, InactiveSeedingTimeLimit: -2,
			Complete: r.dict("progress").str("blocks") == "all",
		}
		if len(labels) > 0 {
			it.Category = labels[0]
		}
		for _, v := range r.list("dnd") {
			n, _ := v.(int64)
			it.Skip = append(it.Skip, n != 0)
		}
		// Ratio and idle modes: 0 follows the global setting, 1 is the
		// torrent's own limit, 2 is unlimited.
		ratio := r.dict("ratio-limit")
		switch ratio.int("ratio-mode") {
		case 1:
			if v, err := strconv.ParseFloat(ratio.str("ratio-limit"), 64); err == nil {
				it.RatioLimit = v
			}
		case 2:
			it.RatioLimit = -1
		}
		idle := r.dict("idle-limit")
		switch idle.int("idle-mode") {
		case 1:
			it.InactiveSeedingTimeLimit = idle.int("idle-limit")
		case 2:
			it.InactiveSeedingTimeLimit = -1
		}
		// Unfinished data may still be in the incomplete directory.
		if inc := r.str("incomplete-dir"); !it.Complete && inc != "" && len(it.Paths) > 0 {
			if _, err := os.Stat(filepath.Join(it.SavePath, filepath.FromSlash(it.Paths[0]))); errors.Is(err, os.ErrNotExist) {
				if _, err := os.Stat(filepath.Join(inc, filepath.FromSlash(it.Paths[0]))); err == nil {
					it.SavePath = inc
				}
			}
		}
		out = append(out, it)
	}
	return out, errs
}

// ReadQBittorrent reads the torrents of qBittorrent's BT_backup directory:
// *.fastresume with their *.torrent. dir may also be qBittorrent's data
// directory that holds BT_backup.
func ReadQBittorrent(dir string) ([]Imported, []error) {
	if st, err := os.Stat(filepath.Join(dir, "BT_backup")); err == nil && st.IsDir() {
		dir = filepath.Join(dir, "BT_backup")
	}
	resumes, err := filepath.Glob(filepath.Join(dir, "*.fastresume"))
	if err != nil {
		return nil, []error{err}
	}
	if len(resumes) == 0 {
		return nil, []error{fmt.Errorf("no .fastresume files in %s; give qBittorrent's BT_backup directory", dir)}
	}
	var out []Imported
	var errs []error
	for _, rp := range resumes {
		tp := strings.TrimSuffix(rp, ".fastresume") + ".torrent"
		mi, err := os.ReadFile(tp) //nolint:gosec // the admin's import directory
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: no torrent file, so no metadata yet; add its magnet link instead", rp))
			continue
		}
		r, err := readBencode(rp)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		save := r.str("save_path")
		if save == "" {
			save = r.str("qBt-savePath")
		}
		it := Imported{
			Source: rp, Metainfo: mi, SavePath: save, Name: r.str("qBt-name"), Category: r.str("qBt-category"),
			Tags: r.strings("qBt-tags"), Stopped: r.int("paused") != 0, AddedAt: unixTime(r.int("added_time")),
			CompletedAt: unixTime(r.int("completed_time")), Uploaded: r.int("total_uploaded"),
			Downloaded: r.int("total_downloaded"), SeedingSeconds: r.int("seeding_time"), ActiveSeconds: r.int("active_time"),
			SeedingTimeLimit: r.int("qBt-seedingTimeLimit"), InactiveSeedingTimeLimit: r.int("qBt-inactiveSeedingTimeLimit"),
			Sequential: r.int("sequential_download") != 0, FirstLastPiece: r.int("qBt-firstLastPiecePriority") != 0,
			Paths: r.strings("mapped_files"), Complete: r.int("completed_time") > 0 || r.int("qBt-seedStatus") != 0,
		}
		// qBittorrent stores the ratio limit times 1000.
		switch rl := r.int("qBt-ratioLimit"); {
		case rl <= -2000:
			it.RatioLimit = -2
		case rl < 0:
			it.RatioLimit = -1
		default:
			it.RatioLimit = float64(rl) / 1000
		}
		if _, ok := r["qBt-seedingTimeLimit"]; !ok {
			it.SeedingTimeLimit = -2
		}
		if _, ok := r["qBt-inactiveSeedingTimeLimit"]; !ok {
			it.InactiveSeedingTimeLimit = -2
		}
		for _, v := range r.list("file_priority") {
			n, _ := v.(int64)
			it.Skip = append(it.Skip, n == 0)
		}
		for _, tier := range r.list("trackers") {
			if urls := (bdict{"t": tier}).strings("t"); len(urls) > 0 {
				it.Trackers = append(it.Trackers, urls)
			}
		}
		if seeds := r.strings("url-list"); len(seeds) > 0 {
			it.WebSeeds = seeds
		}
		out = append(out, it)
	}
	return out, errs
}
