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
	"sync/atomic"

	"github.com/anacrolix/torrent/iplist"

	"github.com/sebdanielsson/piecemeal/internal/settings"
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
	// eMule .dat. A line that only looks like one, such as a PeerGuardian
	// line whose description has a hyphen before a comma, falls through.
	if parts := strings.Split(line, ","); len(parts) >= 2 && strings.Contains(parts[0], "-") {
		if first, last, ok := ipRange(parts[0]); ok {
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

// peerFilter is the blocklist the client checks connections against.
// Its list is swapped when the filter settings change, without recreating
// the client.
type peerFilter struct{ list atomic.Pointer[iplist.IPList] }

func (f *peerFilter) Lookup(ip net.IP) (iplist.Range, bool) {
	return f.list.Load().Lookup(ip)
}

func (f *peerFilter) NumRanges() int { return f.list.Load().NumRanges() }

// blocked reports whether a peer address (host:port) is filtered.
func (f *peerFilter) blocked(addr string) bool {
	ap, err := netip.ParseAddrPort(addr)
	if err != nil {
		return false
	}
	_, ok := f.Lookup(net.IP(ap.Addr().Unmap().AsSlice()))
	return ok
}

// reloadFilter builds the blocklist from the settings and swaps it in. A
// filter file that can't be read leaves just the banned addresses.
func (e *Engine) reloadFilter(s settings.Settings) {
	var ranges []iplist.Range
	if s.IPFilterEnabled && s.IPFilterPath != "" {
		if r, bad, err := readIPFilter(s.IPFilterPath); err != nil {
			e.log.Error("not using the IP filter", "path", s.IPFilterPath, "error", err)
		} else {
			e.log.Info("IP filter loaded", "path", s.IPFilterPath, "ranges", len(r), "unreadableLines", bad)
			ranges = r
		}
	}
	b, bad, _ := ParseIPFilter(strings.NewReader(s.BannedIPs))
	if bad > 0 {
		e.log.Warn("ignoring banned IP entries that aren't addresses or CIDR ranges", "count", bad)
	}
	for i := range b {
		b[i].Description = "banned"
	}
	ranges = append(ranges, b...)
	if len(ranges) == 0 {
		e.filter.list.Store(nil)
		return
	}
	e.filter.list.Store(iplist.New(mergeRanges(ranges)))
}

// maxIPFilterSize bounds a filter file; published lists are a few tens
// of MiB.
const maxIPFilterSize = 512 << 20

func readIPFilter(path string) ([]iplist.Range, int, error) {
	// A device would never end, and opening a FIFO blocks until something
	// writes to it: check before opening.
	regular := func(st os.FileInfo) error {
		switch {
		case !st.Mode().IsRegular():
			return fmt.Errorf("%s is not a regular file", path)
		case st.Size() > maxIPFilterSize:
			return fmt.Errorf("%s is larger than %d MiB", path, maxIPFilterSize>>20)
		}
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	if err := regular(st); err != nil {
		return nil, 0, err
	}
	f, err := os.Open(path) //nolint:gosec // the admin's choice of file
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = f.Close() }()
	// It may have been replaced in between.
	if st, err = f.Stat(); err != nil {
		return nil, 0, err
	}
	if err := regular(st); err != nil {
		return nil, 0, err
	}
	// A file still growing is read up to the limit and then refused.
	b, err := io.ReadAll(io.LimitReader(f, maxIPFilterSize+1))
	if err != nil {
		return nil, 0, err
	}
	if len(b) > maxIPFilterSize {
		return nil, 0, fmt.Errorf("%s is larger than %d MiB", path, maxIPFilterSize>>20)
	}
	return ParseIPFilter(bytes.NewReader(b))
}

// dropBlockedPeersLocked closes connections to peers the filter now
// blocks.
func (e *Engine) dropBlockedPeersLocked() {
	for _, m := range e.torrents {
		if m.t == nil {
			continue
		}
		for _, pc := range m.t.PeerConns() {
			if e.filter.blocked(pc.RemoteAddr.String()) {
				_ = pc.Close()
			}
		}
	}
}

// mergeRanges sorts ranges and merges overlapping and adjacent ones of the
// same address family. iplist only checks the last range starting at or
// before an address, so with overlaps, a banned address inside a filtered
// range would let the rest of that range through.
func mergeRanges(ranges []iplist.Range) []iplist.Range {
	byFirst := func(a, b iplist.Range) int {
		return cmp.Or(bytes.Compare(a.First, b.First), bytes.Compare(a.Last, b.Last))
	}
	var out []iplist.Range
	for _, size := range []int{net.IPv4len, net.IPv6len} {
		family := slices.DeleteFunc(slices.Clone(ranges), func(r iplist.Range) bool { return len(r.First) != size })
		slices.SortFunc(family, byFirst)
		start := len(out)
		for _, r := range family {
			if n := len(out); n > start && bytes.Compare(r.First, nextIP(out[n-1].Last)) <= 0 {
				last := &out[n-1]
				if bytes.Compare(r.Last, last.Last) > 0 {
					last.Last = r.Last
				}
				if last.Description != r.Description && r.Description != "" {
					last.Description = cmp.Or(last.Description, r.Description)
				}
				continue
			}
			out = append(out, r)
		}
	}
	slices.SortFunc(out, byFirst)
	return out
}

// nextIP is ip plus one, or ip itself at the end of the address space.
func nextIP(ip net.IP) net.IP {
	next := slices.Clone(ip)
	for i := len(next) - 1; i >= 0; i-- {
		if next[i]++; next[i] != 0 {
			return next
		}
	}
	return ip
}
