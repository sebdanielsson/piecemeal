package auth

import (
	"strings"
	"testing"
	"time"
)

func TestSafeNext(t *testing.T) {
	for next, want := range map[string]string{
		"/torrents?x=1":        "/torrents?x=1",
		"/":                    "/",
		"":                     "/",
		"//evil.example":       "/",
		"/\t/evil.example":     "/",
		"/\n/evil.example":     "/",
		"/ /evil.example":      "/",
		`/\evil.example`:       "/",
		"https://evil.example": "/",
		"/a\x7f":               "/",
	} {
		if got := SafeNext(next); got != want {
			t.Errorf("SafeNext(%q) = %q, want %q", next, got, want)
		}
	}
}

func TestSealedFlow(t *testing.T) {
	o := NewOIDC(OIDCConfig{}, nil)
	f := oidcFlow{State: "s", Nonce: "n", Verifier: "v", Next: "/x", Expires: time.Now().Add(time.Minute).Unix()}
	sealed := o.sealFlow(f)
	if got, ok := o.openFlow(sealed); !ok || got != f {
		t.Fatalf("round trip: %+v %v", got, ok)
	}
	if strings.Contains(sealed, "/x") {
		t.Fatal("the flow isn't encrypted")
	}
	tampered := []byte(sealed)
	tampered[len(tampered)-2] ^= 1
	if _, ok := o.openFlow(string(tampered)); ok {
		t.Fatal("a tampered flow opened")
	}
	if _, ok := NewOIDC(OIDCConfig{}, nil).openFlow(sealed); ok {
		t.Fatal("another process's flow opened")
	}
	f.Expires = time.Now().Add(-time.Second).Unix()
	if _, ok := o.openFlow(o.sealFlow(f)); ok {
		t.Fatal("an expired flow opened")
	}
}
