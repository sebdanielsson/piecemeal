package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/sebdanielsson/piecemeal/internal/config"
	"github.com/sebdanielsson/piecemeal/internal/engine"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

// pathMaps are --map FROM=TO prefixes, for save paths that differ between
// the old client and piecemeal, such as different container mounts.
type pathMaps [][2]string

func (m *pathMaps) String() string { return fmt.Sprint(*m) }

func (m *pathMaps) Set(v string) error {
	from, to, ok := strings.Cut(v, "=")
	if !ok || from == "" || to == "" {
		return errors.New("want FROM=TO")
	}
	*m = append(*m, [2]string{filepath.Clean(from), filepath.Clean(to)})
	return nil
}

// apply maps a path by the longest matching prefix.
func (m pathMaps) apply(p string) string {
	p = filepath.Clean(p)
	best := -1
	for i, pm := range m {
		if (p == pm[0] || strings.HasPrefix(p, pm[0]+string(filepath.Separator))) && (best < 0 || len(pm[0]) > len(m[best][0])) {
			best = i
		}
	}
	if best < 0 {
		return p
	}
	return m[best][1] + strings.TrimPrefix(p, m[best][0])
}

// importTorrents runs `piecemeal import`.
func importTorrents(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	var maps pathMaps
	fs.Var(&maps, "map", "rewrite save paths starting with FROM to start with TO; repeatable")
	trust := fs.Bool("trust-data", false, "mark complete torrents' data as verified instead of checking it at the next start")
	dryRun := fs.Bool("dry-run", false, "show what would be imported without changing anything")
	fs.SetOutput(out)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(out, "usage: piecemeal import transmission|qbittorrent DIR [--map FROM=TO]... [--trust-data] [--dry-run]")
		_, _ = fmt.Fprintln(out, "DIR is Transmission's configuration directory or qBittorrent's BT_backup directory.")
		fs.PrintDefaults()
	}
	if len(args) < 2 {
		fs.Usage()
		return errors.New("missing client or directory")
	}
	client, dir := args[0], args[1]
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	var items []engine.Imported
	var errs []error
	switch client {
	case "transmission":
		items, errs = engine.ReadTransmission(dir)
	case "qbittorrent":
		items, errs = engine.ReadQBittorrent(dir)
	default:
		fs.Usage()
		return fmt.Errorf("unknown client %q", client)
	}
	for _, err := range errs {
		_, _ = fmt.Fprintln(out, "skipped:", err)
	}
	for i := range items {
		items[i].SavePath = maps.apply(items[i].SavePath)
	}

	cfg, err := config.FromOS()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	if err := engine.CheckRoots(cfg.AllowedRoots, cfg.DataDir, cfg.QBittorrentWebUIDir); err != nil {
		return err
	}
	unlock, err := store.LockDataDir(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("%w; stop piecemeal first", err)
	}
	defer unlock()
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	results := engine.Import(ctx, st, cfg.DataDir, items, engine.ImportOptions{
		AllowedRoots: cfg.AllowedRoots, Trust: *trust, DryRun: *dryRun,
	})
	imported, failed := 0, 0
	for _, r := range results {
		switch {
		case r.Err != nil:
			failed++
			_, _ = fmt.Fprintf(out, "failed:   %s: %v\n", r.Source, r.Err)
		case r.Skipped != "":
			_, _ = fmt.Fprintf(out, "skipped:  %s (%s)\n", r.Name, r.Skipped)
		default:
			imported++
			_, _ = fmt.Fprintf(out, "imported: %s\n", r.Name)
		}
	}
	verb := "imported"
	if *dryRun {
		verb = "would import"
	}
	_, _ = fmt.Fprintf(out, "%s %d torrents; %d failed, %d unreadable\n", verb, imported, failed, len(errs))
	if !*dryRun && imported > 0 && !*trust {
		_, _ = fmt.Fprintln(out, "piecemeal checks their data when it next starts.")
	}
	if failed > 0 {
		return fmt.Errorf("%d torrents failed", failed)
	}
	return nil
}
