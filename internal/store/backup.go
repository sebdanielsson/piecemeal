package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// FileName is the database's name in the data directory.
const FileName = "piecemeal.db"

// Backup writes a consistent copy of the database to dst, which must not
// exist. It works while piecemeal runs: SQLite copies one snapshot.
func (s *Store) Backup(ctx context.Context, dst string) error {
	if _, err := s.DB.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

// BackupFile copies the database in a data directory to dst without
// changing it, for a backup taken from another process while piecemeal
// runs.
func BackupFile(ctx context.Context, dataDir, dst string) error {
	src := filepath.Join(dataDir, FileName)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(10000)")
	q.Set("mode", "ro")
	db, err := sql.Open("sqlite", "file:"+src+"?"+q.Encode())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

// Restore replaces the database in a data directory with a backup, after
// checking the backup is a piecemeal database of a version this one can
// migrate. The database it replaces, with its WAL files, is kept next to
// it with a .replaced-<time> suffix. piecemeal must not be running.
func Restore(ctx context.Context, dataDir, backup string) (replaced string, err error) {
	unlock, err := LockDataDir(dataDir)
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := checkBackup(ctx, backup); err != nil {
		return "", err
	}
	dst := filepath.Join(dataDir, FileName)
	if _, err := os.Stat(dst); err == nil {
		replaced = dst + ".replaced-" + time.Now().UTC().Format("20060102T150405Z")
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Rename(dst+suffix, replaced+suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("keeping the current database: %w", err)
			}
		}
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return replaced, err
	}
	if err := copyFile(backup, dst); err != nil {
		return replaced, fmt.Errorf("restore: %w", err)
	}
	// Bring it up to the current schema now, so problems show here rather
	// than at the next start.
	st, err := Open(ctx, dataDir)
	if err != nil {
		return replaced, err
	}
	return replaced, st.Close()
}

// checkBackup opens a backup read-only and checks it's a piecemeal
// database no newer than this version knows.
func checkBackup(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	q := url.Values{}
	q.Set("mode", "ro")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	var version int64
	if err := db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied").Scan(&version); err != nil {
		return fmt.Errorf("%s is not a piecemeal database: %w", path, err)
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM torrents").Scan(&n); err != nil {
		return fmt.Errorf("%s is not a piecemeal database: %w", path, err)
	}
	latest, err := latestMigration()
	if err != nil {
		return err
	}
	if version > latest {
		return fmt.Errorf("%s is from a newer piecemeal (schema %d, this one knows %d)", path, version, latest)
	}
	return nil
}

func latestMigration() (int64, error) {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return 0, err
	}
	var latest int64
	for _, e := range entries {
		var v int64
		if _, err := fmt.Sscanf(e.Name(), "%05d_", &v); err == nil {
			latest = max(latest, v)
		}
	}
	return latest, nil
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(src) //nolint:gosec // the admin's backup file
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // the data directory
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}
