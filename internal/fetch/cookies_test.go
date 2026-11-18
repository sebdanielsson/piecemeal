package fetch

import (
	"net/url"
	"testing"
	"time"
)

func TestCookieMatching(t *testing.T) {
	f := New(nil)
	now := time.Unix(1_000_000, 0)
	f.jar.now = func() time.Time { return now }
	f.SetCookies([]Cookie{
		{Name: "uid", Domain: ".tracker.example", Path: "/", Value: "1"},
		{Name: "pass", Domain: "tracker.example", Path: "/dl", Value: "2"},
		{Name: "old", Domain: "tracker.example", Value: "3", ExpirationDate: now.Unix()},
		{Name: "nodomain", Value: "4"},
	})
	for raw, want := range map[string]string{
		"https://tracker.example/":           "uid",
		"https://www.tracker.example/dl/1":   "uid pass",
		"https://tracker.example/dl":         "uid pass",
		"https://tracker.example/dlx":        "uid",
		"https://eviltracker.example/dl/1":   "",
		"https://tracker.example.evil.com/x": "",
	} {
		u, _ := url.Parse(raw)
		got := ""
		for _, c := range f.jar.Cookies(u) {
			if got != "" {
				got += " "
			}
			got += c.Name
		}
		if got != want {
			t.Errorf("%s: %q, want %q", raw, got, want)
		}
	}
	if cs := f.Cookies(); len(cs) != 4 {
		t.Fatalf("Cookies: %v", cs)
	}
}
