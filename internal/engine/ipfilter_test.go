package engine

import (
	"net"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/iplist"
)

func TestParseIPFilter(t *testing.T) {
	in := `# comment
001.002.003.000 - 001.002.003.255 , 000 , eMule range
010.000.000.000 - 010.255.255.255 , 200 , allowed, skipped
Some org:192.168.1.0-192.168.1.255
203.0.113.0/24
198.51.100.7
2001:db8::/32
not an entry
`
	ranges, bad, err := ParseIPFilter(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if bad != 1 || len(ranges) != 5 {
		t.Fatalf("ranges %d (%v), bad %d", len(ranges), ranges, bad)
	}
	list := iplist.New(ranges)
	for ip, blocked := range map[string]bool{
		"1.2.3.4": true, "1.2.4.1": false, "10.1.1.1": false, "192.168.1.200": true,
		"203.0.113.255": true, "198.51.100.7": true, "198.51.100.8": false, "2001:db8::1": true, "2001:db9::1": false,
	} {
		if _, got := list.Lookup(net.ParseIP(ip)); got != blocked {
			t.Errorf("%s: blocked %v, want %v", ip, got, blocked)
		}
	}
}
