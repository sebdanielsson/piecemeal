// Command seeder seeds generated content for the end-to-end tests. It
// writes the .torrent file and a magnet URI naming itself as a peer to
// -out, then seeds until stopped.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/anacrolix/torrent"

	"github.com/sebdanielsson/piecemeal/internal/engine/enginetest"
)

func main() {
	var (
		out    = flag.String("out", "/shared", "directory for seed.torrent, seed.magnet and seed.hash")
		data   = flag.String("data", "/seed", "directory for the seeded content")
		name   = flag.String("name", "Big.Buck.Bunny.2008.1080p.BluRay.x264-PIECEMEAL", "torrent name")
		file   = flag.String("file", "Big.Buck.Bunny.2008.1080p.BluRay.x264-PIECEMEAL.mkv", "file inside the torrent")
		size   = flag.Int("size", 64<<20, "file size in bytes; 0 seeds what is already in -data/-name")
		host   = flag.String("advertise", "127.0.0.1", "address peers reach this seeder at")
		listen = flag.Int("port", 6881, "peer port")
	)
	flag.Parse()
	files := map[string]int{*file: *size}
	if *size == 0 {
		files = nil
	}
	s, err := enginetest.StartSeeder(*data, *name, files, func(tc *torrent.ClientConfig) {
		tc.ListenHost = func(string) string { return "0.0.0.0" }
		tc.ListenPort = *listen
	})
	if err != nil {
		log.Fatal(err)
	}
	magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s&x.pe=%s:%d", s.Hash, s.Name, *host, *listen)
	for f, b := range map[string][]byte{
		"seed.torrent": s.Metainfo, "seed.magnet": []byte(magnet + "\n"), "seed.hash": []byte(s.Hash + "\n"),
	} {
		if err := os.WriteFile(filepath.Join(*out, f), b, 0o644); err != nil { //nolint:gosec // read by the other test containers
			s.Client.Close()
			log.Fatal(err)
		}
	}
	log.Printf("seeding %s (%s) on port %d", s.Name, s.Hash, *listen)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	<-sig
	s.Client.Close()
}
