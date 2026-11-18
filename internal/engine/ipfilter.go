package engine

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/anacrolix/torrent/iplist"
)

// ParseIPFilter reads an IP filter in any of the common formats, line by
// line:
//
//   - eMule .dat: "first - last , access , description"; ranges with an
//     access level of 128 or more are allowed and skipped
//   - PeerGuardian .p2p: "description:first-last"
//   - one IP address or CIDR range per line
//
// Blank lines and lines starting with # or // are ignored. Lines that
// parse as none of these are counted in bad.
func ParseIPFilter(r io.Reader) (ranges []iplist.Range, bad int, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		rng, ok := parseFilterLine(line)
		if !ok {
			bad++
			continue
		}
		if rng.First != nil {
			ranges = append(ranges, rng)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, bad, err
	}
	slices.SortFunc(ranges, func(a, b iplist.Range) int { return bytes.Compare(a.First, b.First) })
	return ranges, bad, nil
}

// parseFilterLine parses one line. A zero Range with ok means a range that
// is allowed rather than blocked.
func parseFilterLine(line string) (iplist.Range, bool) {
	// eMule .dat
	if parts := strings.Split(line, ","); len(parts) >= 2 && strings.Contains(parts[0], "-") {
		first, last, ok := ipRange(parts[0])
		if !ok {
			return iplist.Range{}, false
		}
		level, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return iplist.Range{}, false
		}
		if level >= 128 {
			return iplist.Range{}, true
		}
		desc := ""
		if len(parts) > 2 {
			desc = strings.TrimSpace(strings.Join(parts[2:], ","))
		}
		return iplist.Range{First: first, Last: last, Description: desc}, true
	}
	// PeerGuardian: the description can contain colons, the range can't.
	if i := strings.LastIndex(line, ":"); i > 0 && strings.Contains(line[i:], "-") {
		if first, last, ok := ipRange(line[i+1:]); ok {
			return iplist.Range{First: first, Last: last, Description: line[:i]}, true
		}
	}
	// A CIDR range or a single address.
	if p, err := netip.ParsePrefix(line); err == nil {
		p = p.Masked()
		first := p.Addr()
		last := lastInPrefix(p)
		return iplist.Range{First: net.IP(first.AsSlice()), Last: net.IP(last.AsSlice())}, true
	}
	if a, err := netip.ParseAddr(line); err == nil {
		ip := net.IP(a.Unmap().AsSlice())
		return iplist.Range{First: ip, Last: ip}, true
	}
	return iplist.Range{}, false
}

func ipRange(s string) (net.IP, net.IP, bool) {
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return nil, nil, false
	}
	first, err1 := netip.ParseAddr(trimZeros(strings.TrimSpace(a)))
	last, err2 := netip.ParseAddr(trimZeros(strings.TrimSpace(b)))
	if err1 != nil || err2 != nil || first.Is4() != last.Is4() || last.Less(first) {
		return nil, nil, false
	}
	return net.IP(first.AsSlice()), net.IP(last.AsSlice()), true
}

// trimZeros drops the leading zeros eMule files pad IPv4 octets with
// (001.002.003.004), which netip refuses.
func trimZeros(s string) string {
	if strings.Contains(s, ":") {
		return s
	}
	parts := strings.Split(s, ".")
	for i, p := range parts {
		if t := strings.TrimLeft(p, "0"); t != "" {
			parts[i] = t
		} else {
			parts[i] = "0"
		}
	}
	return strings.Join(parts, ".")
}

func lastInPrefix(p netip.Prefix) netip.Addr {
	b := p.Addr().AsSlice()
	for i := range b {
		bit := i * 8
		switch {
		case bit >= p.Bits():
			b[i] = 0xff
		case bit+8 > p.Bits():
			b[i] |= 0xff >> (p.Bits() - bit)
		}
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}

// blocklist builds the client's IP blocklist from the settings, or nil
// for none.
func (e *Engine) blocklist(enabled bool, path, banned string) (iplist.Ranger, error) {
	var ranges []iplist.Range
	if enabled && path != "" {
		f, err := os.Open(path) //nolint:gosec // the admin's choice of file
		if err != nil {
			return nil, fmt.Errorf("IP filter: %w", err)
		}
		r, bad, err := ParseIPFilter(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("IP filter %s: %w", path, err)
		}
		e.log.Info("IP filter loaded", "path", path, "ranges", len(r), "unreadableLines", bad)
		ranges = r
	}
	b, bad, err := ParseIPFilter(strings.NewReader(banned))
	if err != nil {
		return nil, err
	}
	if bad > 0 {
		e.log.Warn("ignoring banned IP entries that aren't addresses or CIDR ranges", "count", bad)
	}
	for i := range b {
		b[i].Description = "banned"
	}
	ranges = append(ranges, b...)
	if len(ranges) == 0 {
		return nil, nil
	}
	slices.SortFunc(ranges, func(a, b iplist.Range) int {
		return cmp.Or(bytes.Compare(a.First, b.First), bytes.Compare(a.Last, b.Last))
	})
	return iplist.New(ranges), nil
}
