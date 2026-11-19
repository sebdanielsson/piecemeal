package proxy

import (
	"bufio"
	"encoding/base64"
	"net"
	"net/http"
	"net/url"
)

// httpConnect asks an HTTP proxy for a tunnel to addr with CONNECT. It
// returns the tunnel, or conn itself with an error.
func (s *Server) httpConnect(conn net.Conn, addr string) (net.Conn, error) {
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: http.Header{},
	}
	if s.Username != "" {
		cred := base64.StdEncoding.EncodeToString([]byte(s.Username + ":" + s.Password))
		req.Header.Set("Proxy-Authorization", "Basic "+cred)
	}
	if err := req.Write(conn); err != nil {
		return conn, s.errorf("%w", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return conn, s.errorf("reading its reply: %w", err)
	}
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusProxyAuthRequired && s.Username == "":
		return conn, s.errorf("it wants a username and password")
	case resp.StatusCode == http.StatusProxyAuthRequired:
		return conn, s.errorf("it refused the username or password")
	case resp.StatusCode/100 != 2:
		return conn, s.errorf("CONNECT %s: %s", addr, resp.Status)
	}
	if br.Buffered() > 0 {
		// The far end spoke first, and its bytes are in the reader.
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

// bufferedConn reads what a bufio.Reader holds before reading the
// connection.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
