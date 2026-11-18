package engine

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzParseIPFilter checks filter files of any content parse without
// panicking, and that merged ranges are sorted and don't overlap.
func FuzzParseIPFilter(f *testing.F) {
	for _, s := range []string{
		"001.002.003.000 - 001.002.003.255 , 000 , eMule\n",
		"T-Mobile USA, Inc.:1.2.3.0-1.2.3.255\n",
		"2001:db8::/32\n198.51.100.7\n# comment\n",
		"1.2.3.4-1.2.3.1 , 0\n255.255.255.255\n0.0.0.0/0\n::/0\n",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		ranges, _, err := ParseIPFilter(strings.NewReader(in))
		if err != nil {
			return
		}
		merged := mergeRanges(ranges)
		for i, r := range merged {
			if bytes.Compare(r.First, r.Last) > 0 {
				t.Fatalf("range %d: first after last: %v", i, r)
			}
			if i > 0 && len(merged[i-1].Last) == len(r.First) && bytes.Compare(merged[i-1].Last, r.First) >= 0 {
				t.Fatalf("ranges %d and %d overlap: %v %v", i-1, i, merged[i-1], r)
			}
		}
	})
}

// FuzzCleanRelative checks that any accepted client path stays inside the
// save path.
func FuzzCleanRelative(f *testing.F) {
	for _, s := range []string{"a/b", "../x", `..\x`, "a/../../b", "/abs", "C:\\x", "a\x00b", ".", "a//b/./c"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, p string) {
		c, err := cleanRelative(p)
		if err != nil {
			return
		}
		if !filepath.IsLocal(filepath.FromSlash(c)) || strings.ContainsRune(c, 0) {
			t.Fatalf("%q accepted as %q", p, c)
		}
		if joined := filepath.Join("/save", filepath.FromSlash(c)); !strings.HasPrefix(joined, "/save/") {
			t.Fatalf("%q leads to %q", p, joined)
		}
	})
}

// FuzzParseSource checks add request bodies of any content don't panic.
func FuzzParseSource(f *testing.F) {
	f.Add([]byte("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"))
	f.Add([]byte("d4:infod4:name1:x6:lengthi1e12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaaee"))
	f.Add([]byte("d4:infoi1ee"))
	f.Add([]byte("l"))
	f.Fuzz(func(_ *testing.T, b []byte) {
		_, _, _ = ParseSource(b)
	})
}
