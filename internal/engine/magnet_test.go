package engine

import (
	"context"
	"errors"
	"testing"
)

// TestAddRefusesMagnetWithoutInfoHash checks that a magnet URI without a
// BitTorrent v1 info hash is refused instead of reaching anacrolix, which
// panics on a zero info hash.
func TestAddRefusesMagnetWithoutInfoHash(t *testing.T) {
	h := newHarness(t)
	for _, m := range []string{"magnet:?nothing", "magnet:?dn=only-a-name"} {
		if _, err := h.engine.Add(context.Background(), AddParams{Magnet: m}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", m, err)
		}
	}
}
