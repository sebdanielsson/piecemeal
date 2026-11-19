package netbind

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// interfaces returns the loopback interface's name and an up interface
// that isn't loopback, if there is one.
func interfaces(t *testing.T) (loopback, other string) {
	t.Helper()
	ifs, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, ifi := range ifs {
		switch {
		case ifi.Flags&net.FlagLoopback != 0 && loopback == "":
			loopback = ifi.Name
		case ifi.Flags&net.FlagUp != 0 && ifi.Flags&net.FlagLoopback == 0 && other == "":
			addrs, _ := ifi.Addrs()
			if len(addrs) > 0 {
				other = ifi.Name
			}
		}
	}
	if loopback == "" {
		t.Skip("no loopback interface")
	}
	return loopback, other
}

func listener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return ln.Addr().String()
}

func TestNone(t *testing.T) {
	b, err := New("")
	if b != nil || err != nil {
		t.Fatalf("New(\"\") = %v, %v", b, err)
	}
}

func TestDial(t *testing.T) {
	loopback, other := interfaces(t)
	addr := listener(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	b, err := New(loopback)
	if err != nil {
		t.Fatal(err)
	}
	c, err := b.DialContext(ctx, "tcp4", addr)
	if err != nil {
		t.Fatalf("through %s: %v", loopback, err)
	}
	_ = c.Close()

	// Through another interface, loopback can't be reached: the dial
	// fails or, on Linux, waits for an answer that never comes.
	if other != "" {
		b, _ := New(other)
		octx, ocancel := context.WithTimeout(context.Background(), time.Second)
		defer ocancel()
		if c, err := b.DialContext(octx, "tcp4", addr); err == nil {
			_ = c.Close()
			t.Fatalf("reached loopback through %s", other)
		}
	}

	// A missing interface fails every socket at once.
	b, _ = New("pm-missing0")
	_, err = b.DialContext(ctx, "tcp4", addr)
	if err == nil || !strings.Contains(err.Error(), "pm-missing0") {
		t.Fatalf("through a missing interface: %v", err)
	}
	if _, err := b.ListenPacket("udp4", "127.0.0.1:0"); err == nil {
		t.Fatal("UDP socket on a missing interface")
	}
}

func TestListenPacket(t *testing.T) {
	loopback, _ := interfaces(t)
	b, _ := New(loopback)
	pc, err := b.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	echo, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	if _, err := pc.WriteTo([]byte("hi"), echo.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	_ = echo.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 8)
	if n, _, err := echo.ReadFrom(buf); err != nil || string(buf[:n]) != "hi" {
		t.Fatalf("got %q, %v", buf[:n], err)
	}
}

// TestResolverLoopback checks that a name server on loopback is asked
// directly, and any other through the interface.
func TestResolverLoopback(t *testing.T) {
	b, _ := New("pm-missing0")
	r := b.Resolver()
	ns, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ns.Close()
	c, err := r.Dial(context.Background(), "udp", ns.LocalAddr().String())
	if err != nil {
		t.Fatalf("loopback name server: %v", err)
	}
	_ = c.Close()
	if _, err := r.Dial(context.Background(), "udp", "192.0.2.53:53"); err == nil {
		t.Fatal("reached a name server around the interface")
	}
}

func TestState(t *testing.T) {
	b := &Binder{name: "wg0", lookup: func(string) (*net.Interface, error) {
		return &net.Interface{Index: 7, Flags: net.FlagUp}, nil
	}}
	if s := b.State(); !s.Up || s.Index != 7 {
		t.Fatalf("state %+v", s)
	}
	b.lookup = func(string) (*net.Interface, error) { return nil, errors.New("no such interface") }
	if s := b.State(); s.Up {
		t.Fatalf("missing interface is up: %+v", s)
	}
}
