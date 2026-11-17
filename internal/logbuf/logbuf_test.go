package logbuf

import (
	"log/slog"
	"testing"
)

func TestBuffer(t *testing.T) {
	b := New(3)
	log := slog.New(b.Handler(slog.DiscardHandler)).With("component", "x")
	log.Info("one")
	log.Warn("two", "n", 2)
	log.Debug("below info: not recorded")
	log.Error("three")
	log.Info("four")

	all := b.Since(0, slog.LevelDebug)
	if len(all) != 3 || all[0].Message != "two" || all[2].Message != "four" {
		t.Fatalf("entries %+v", all)
	}
	if all[0].Attrs != "component=x n=2" {
		t.Fatalf("attrs %q", all[0].Attrs)
	}
	warn := b.Since(0, slog.LevelWarn)
	if len(warn) != 2 {
		t.Fatalf("warn and above: %+v", warn)
	}
	if after := b.Since(all[1].ID, slog.LevelDebug); len(after) != 1 || after[0].Message != "four" {
		t.Fatalf("after: %+v", after)
	}
}
