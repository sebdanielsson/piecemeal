package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type backupOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

func (a *API) registerBackup() {
	op := a.operation(http.MethodGet, "/admin/backup", "getBackup", "Download a backup of the database", "Admin", accessAdmin)
	op.Description = "Admins only. A consistent copy of piecemeal's database: settings, users, API keys, " +
		"torrents, categories, tags and RSS. Restore it with `piecemeal restore FILE` while piecemeal is stopped. " +
		"Downloaded data isn't included; piece completion is rebuilt by checking the data."
	op.Responses = map[string]*huma.Response{"200": {
		Description: "The SQLite database.",
		Content:     map[string]*huma.MediaType{"application/vnd.sqlite3": {Schema: &huma.Schema{Type: "string", Format: "binary"}}},
	}}
	huma.Register(a.huma, op, func(ctx context.Context, _ *struct{}) (*backupOutput, error) {
		dir := a.System.DataDir
		if dir == "" {
			dir = os.TempDir()
		}
		var id [8]byte
		_, _ = rand.Read(id[:])
		tmp := filepath.Join(dir, ".backup-"+hex.EncodeToString(id[:])+".db")
		defer func() { _ = os.Remove(tmp) }()
		if err := a.Store.Backup(ctx, tmp); err != nil {
			return nil, err
		}
		b, err := os.ReadFile(tmp) //nolint:gosec // a file this handler just wrote
		if err != nil {
			return nil, err
		}
		a.Auth.Audit(ctx, actor(ctx), "backup.download", "")
		name := "piecemeal-" + time.Now().UTC().Format("20060102-150405") + ".db"
		return &backupOutput{
			ContentType: "application/vnd.sqlite3", ContentDisposition: `attachment; filename="` + name + `"`, Body: b,
		}, nil
	})
}
