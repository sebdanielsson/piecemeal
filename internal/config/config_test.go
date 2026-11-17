package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c, err := FromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.Metrics != "auth" || !c.LocalLogin || !c.SecureCookies || c.Mmap {
		t.Fatalf("defaults: %+v", c)
	}
	if len(c.AllowedRoots) != 1 || c.AllowedRoots[0] != c.DownloadDir {
		t.Fatalf("allowed roots %v, download dir %s", c.AllowedRoots, c.DownloadDir)
	}
}

func TestParse(t *testing.T) {
	c, err := FromEnv(env(map[string]string{
		"PIECEMEAL_DOWNLOAD_DIR":    "/downloads",
		"PIECEMEAL_ALLOWED_ROOTS":   "/downloads, /media",
		"PIECEMEAL_NATPMP_GATEWAY":  "10.2.0.1",
		"PIECEMEAL_TRUSTED_PROXIES": "172.16.0.0/12,127.0.0.1",
		"PIECEMEAL_METRICS":         "public",
		"PIECEMEAL_MMAP":            "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.AllowedRoots) != 2 || c.AllowedRoots[1] != "/media" || c.NATPMPGateway.String() != "10.2.0.1" {
		t.Fatalf("parsed: %+v", c)
	}
	if len(c.TrustedProxies) != 2 || c.TrustedProxies[1].String() != "127.0.0.1/32" || !c.Mmap {
		t.Fatalf("parsed: %+v", c)
	}
}

func TestErrors(t *testing.T) {
	_, err := FromEnv(env(map[string]string{
		"PIECEMEAL_NATPMP_GATEWAY":  "not-an-ip",
		"PIECEMEAL_METRICS":         "sometimes",
		"PIECEMEAL_MMAP":            "perhaps",
		"PIECEMEAL_ALLOWED_ROOTS":   "relative/path",
		"PIECEMEAL_TRUSTED_PROXIES": "nonsense",
	}))
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"NATPMP_GATEWAY", "METRICS", "MMAP", "ALLOWED_ROOTS", "TRUSTED_PROXIES"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error doesn't mention %s: %v", want, err)
		}
	}
}
