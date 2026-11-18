// Package enginetest has helpers for tests that need a BitTorrent swarm: a
// seeder serving generated content on loopback, and client settings that
// keep anacrolix off the network.
package enginetest

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// Offline keeps a client off the network: loopback IPv4 only, no DHT,
// UPnP, local service discovery, trackers or WebTorrent.
func Offline(tc *torrent.ClientConfig) {
	tc.ListenHost = func(string) string { return "127.0.0.1" }
	tc.ListenPort = 0
	tc.NoDHT = true
	tc.NoDefaultPortForwarding = true
	tc.DisableIPv6 = true
	tc.DisableTrackers = true
	tc.LocalServiceDiscovery = nil
	tc.DisableWebtorrent = true
}

// Seeder is an anacrolix client seeding one generated torrent.
type Seeder struct {
	Client *torrent.Client
	// Metainfo is the .torrent file.
	Metainfo []byte
	// Hash is the infohash, lowercase hex.
	Hash string
	// Name is the torrent's name, the root folder or single file.
	Name string
	// Addr is the seeder's peer address, host:port.
	Addr string
	// Content maps each file's path inside the torrent to its data.
	Content map[string][]byte
}

// Magnet returns a magnet URI that names the seeder as a peer (x.pe), so
// metadata and data come from it without trackers or the DHT.
func (s *Seeder) Magnet() string {
	return fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s&x.pe=%s", s.Hash, url.QueryEscape(s.Name), url.QueryEscape(s.Addr))
}

// NewSeeder generates files (path to size) under a root folder named name
// and seeds them on loopback until the test ends.
func NewSeeder(t testing.TB, name string, files map[string]int) *Seeder {
	t.Helper()
	s, err := StartSeeder(t.TempDir(), name, files, Offline)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Client.Close() })
	return s
}

// StartSeeder generates files (path to size) under dir/name and seeds them;
// with no files it seeds what is already there. configure adjusts the
// client, such as where it listens; Close the client when done.
func StartSeeder(dir, name string, files map[string]int, configure func(*torrent.ClientConfig)) (*Seeder, error) {
	root := filepath.Join(dir, name)
	content := map[string][]byte{}
	for p, size := range files {
		b := make([]byte, size)
		_, _ = rand.Read(b)
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return nil, err
		}
		if err := os.WriteFile(full, b, 0o600); err != nil {
			return nil, err
		}
		content[p] = b
	}
	info := metainfo.Info{PieceLength: 256 << 10}
	if total := totalSize(files); files != nil && total < 8<<20 {
		info.PieceLength = 32 << 10
	}
	if err := info.BuildFromFilePath(root); err != nil {
		return nil, err
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		return nil, err
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes, Comment: "piecemeal test"}
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		return nil, err
	}

	tc := torrent.NewDefaultClientConfig()
	Offline(tc)
	configure(tc)
	tc.Seed = true
	tc.DataDir = dir
	tc.DefaultStorage = storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   dir,
		PieceCompletion: storage.NewMapPieceCompletion(),
		UsePartFiles:    g.Some(false),
	})
	tc.Slogger = slog.New(slog.DiscardHandler)
	cl, err := torrent.NewClient(tc)
	if err != nil {
		return nil, err
	}
	tor, err := cl.AddTorrent(&mi)
	if err != nil {
		cl.Close()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := tor.VerifyDataContext(ctx); err != nil {
		cl.Close()
		return nil, err
	}
	if !tor.Complete().Bool() {
		cl.Close()
		return nil, errors.New("seeder is not complete after verifying")
	}
	return &Seeder{
		Client: cl, Metainfo: buf.Bytes(), Hash: mi.HashInfoBytes().HexString(), Name: info.BestName(),
		Addr: fmt.Sprintf("127.0.0.1:%d", cl.LocalPort()), Content: content,
	}, nil
}

func totalSize(files map[string]int) int {
	n := 0
	for _, s := range files {
		n += s
	}
	return n
}
