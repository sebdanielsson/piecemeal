package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupAndRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSetting(ctx, UpsertSettingParams{Key: "k", Value: `"before"`}); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(t.TempDir(), "live.db")
	if err := st.Backup(ctx, live); err != nil {
		t.Fatal(err)
	}
	// A backup from another process, while the database is open.
	other := filepath.Join(t.TempDir(), "other.db")
	if err := BackupFile(ctx, dir, other); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSetting(ctx, UpsertSettingParams{Key: "k", Value: `"after"`}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	for _, b := range []string{live, other} {
		replaced, err := Restore(ctx, dir, b)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(replaced); err != nil {
			t.Fatalf("the replaced database wasn't kept: %v", err)
		}
		st, err := Open(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		settings, err := st.ListSettings(ctx)
		_ = st.Close()
		if err != nil || len(settings) != 1 || settings[0].Value != `"before"` {
			t.Fatalf("restored settings: %+v %v", settings, err)
		}
	}

	// Not while piecemeal runs.
	unlock, err := LockDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, dir, live); !errors.Is(err, ErrLocked) {
		t.Fatalf("restoring while locked: %v", err)
	}
	unlock()

	notDB := filepath.Join(t.TempDir(), "x.db")
	if err := os.WriteFile(notDB, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, dir, notDB); err == nil || !strings.Contains(err.Error(), "not a piecemeal database") {
		t.Fatalf("restoring a non-database: %v", err)
	}
	if _, err := Restore(ctx, dir, filepath.Join(dir, "missing.db")); err == nil {
		t.Fatal("restoring a missing file worked")
	}
}
