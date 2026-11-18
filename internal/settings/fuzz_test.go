package settings

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sebdanielsson/piecemeal/internal/store"
)

// FuzzUpdate checks any settings patch is either refused or leaves valid
// settings.
func FuzzUpdate(f *testing.F) {
	for _, s := range []string{
		`{"downloadLimit":1024}`, `{"peerPort":-1}`, `{"altSpeedScheduleFrom":"25:00"}`, `{"encryption":"x"}`,
		`{"rssRefreshInterval":0}`, `{"bannedIPs":"1.2.3.4\nx"}`, `[]`, `{"unknown":1}`, `{"ratioLimit":1e309}`,
	} {
		f.Add(s)
	}
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { st.Close() })
	m, err := Load(ctx, st, Defaults("/downloads"), nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, patch string) {
		s, err := m.Update(ctx, json.RawMessage(patch))
		if err != nil {
			return
		}
		if err := s.Validate(); err != nil {
			t.Fatalf("patch %s left invalid settings: %v", patch, err)
		}
	})
}

func FuzzParseClock(f *testing.F) {
	for _, s := range []string{"08:00", "23:59", "24:00", "8:0", "", "aa:bb"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if m, err := ParseClock(s); err == nil && (m < 0 || m >= 24*60) {
			t.Fatalf("%q gave %d minutes", s, m)
		}
	})
}
