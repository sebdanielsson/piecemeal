package proxy_test

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/proxy"
)

// TestRealSOCKS5 checks CONNECT and the UDP relay against a SOCKS5 server
// you run, such as Dante, rather than piecemeal's own test proxy:
//
//	PIECEMEAL_TEST_SOCKS5=host:port \
//	PIECEMEAL_TEST_REACH=address-the-proxy-reaches-this-machine-at \
//	go test -run TestRealSOCKS5 ./internal/proxy
//
// PIECEMEAL_TEST_SOCKS5_USER and _PASSWORD add credentials.
func TestRealSOCKS5(t *testing.T) {
	addr, reach := os.Getenv("PIECEMEAL_TEST_SOCKS5"), os.Getenv("PIECEMEAL_TEST_REACH")
	if addr == "" || reach == "" {
		t.Skip("set PIECEMEAL_TEST_SOCKS5 and PIECEMEAL_TEST_REACH to run against a real SOCKS5 server")
	}
	srv := proxy.Server{
		Type: proxy.SOCKS5, Addr: addr,
		Username: os.Getenv("PIECEMEAL_TEST_SOCKS5_USER"), Password: os.Getenv("PIECEMEAL_TEST_SOCKS5_PASSWORD"),
	}

	// TCP, to an echo server on all addresses here.
	ln, err := net.Listen("tcp", ":0")
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
			go func() {
				defer c.Close()
				buf := make([]byte, 1024)
				n, _ := c.Read(buf)
				_, _ = c.Write(buf[:n])
			}()
		}
	}()
	tcpTarget := net.JoinHostPort(reach, strconv.Itoa(ln.Addr().(*net.TCPAddr).Port))
	c, err := srv.DialContext(context.Background(), "tcp", tcpTarget)
	if err != nil {
		t.Fatal(err)
	}
	checkEcho(t, c)

	// UDP, through the relay, to an echo server on all addresses here.
	pc, err := net.ListenUDP("udp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteToUDPAddrPort(buf[:n], from)
		}
	}()
	udpTarget := &net.UDPAddr{IP: net.ParseIP(reach), Port: pc.LocalAddr().(*net.UDPAddr).Port}
	relay, err := srv.ListenPacket(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	_ = relay.SetDeadline(time.Now().Add(10 * time.Second))
	for _, msg := range []string{"first datagram", "second datagram"} {
		if _, err := relay.WriteTo([]byte(msg), udpTarget); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 100)
		n, from, err := relay.ReadFrom(buf)
		if err != nil {
			t.Fatal(err)
		}
		if string(buf[:n]) != msg || from.(*net.UDPAddr).Port != udpTarget.Port {
			t.Fatalf("got %q from %v", buf[:n], from)
		}
	}
}
