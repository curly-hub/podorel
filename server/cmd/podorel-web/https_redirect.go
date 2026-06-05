package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const tlsHandshakeRecord = 0x16

type protocolMux struct {
	base         net.Listener
	tlsListener  *connListener
	httpListener *connListener
	sniffTimeout time.Duration
}

func newProtocolMux(base net.Listener, sniffTimeout time.Duration) *protocolMux {
	return &protocolMux{
		base:         base,
		tlsListener:  newConnListener(base.Addr()),
		httpListener: newConnListener(base.Addr()),
		sniffTimeout: sniffTimeout,
	}
}

func (m *protocolMux) TLSListener() net.Listener {
	return m.tlsListener
}

func (m *protocolMux) HTTPListener() net.Listener {
	return m.httpListener
}

func (m *protocolMux) Serve(ctx context.Context) error {
	defer m.tlsListener.Close()
	defer m.httpListener.Close()

	for {
		conn, err := m.base.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return net.ErrClosed
			}
			return err
		}
		go m.route(conn)
	}
}

func (m *protocolMux) route(conn net.Conn) {
	var first [1]byte
	if m.sniffTimeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(m.sniffTimeout))
	}
	n, err := conn.Read(first[:])
	if m.sniffTimeout > 0 {
		_ = conn.SetReadDeadline(time.Time{})
	}
	if err != nil || n == 0 {
		_ = conn.Close()
		return
	}

	wrapped := &prefixedConn{Conn: conn, prefix: first[:n]}
	if first[0] == tlsHandshakeRecord {
		m.tlsListener.deliver(wrapped)
		return
	}
	m.httpListener.deliver(wrapped)
}

type connListener struct {
	addr   net.Addr
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newConnListener(addr net.Addr) *connListener {
	return &connListener{
		addr:   addr,
		conns:  make(chan net.Conn, 128),
		closed: make(chan struct{}),
	}
}

func (l *connListener) Accept() (net.Conn, error) {
	select {
	case <-l.closed:
		return nil, net.ErrClosed
	default:
	}

	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *connListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *connListener) Addr() net.Addr {
	return l.addr
}

func (l *connListener) deliver(conn net.Conn) {
	select {
	case <-l.closed:
		_ = conn.Close()
	case l.conns <- conn:
	}
}

type prefixedConn struct {
	net.Conn
	prefix []byte
}

func (c *prefixedConn) Read(p []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(p, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

func httpsRedirectHandler(publicURL string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, httpsRedirectURL(publicURL, r), http.StatusPermanentRedirect)
	})
}

func httpsRedirectURL(publicURL string, r *http.Request) string {
	path := r.URL.Path
	if path == "" {
		path = "/"
	}

	if base, ok := httpsPublicURLBase(publicURL); ok {
		target := url.URL{
			Scheme:   "https",
			Host:     base.Host,
			Path:     path,
			RawQuery: r.URL.RawQuery,
		}
		return target.String()
	}

	host := r.Host
	if host == "" {
		host = r.URL.Host
	}
	target := url.URL{
		Scheme:   "https",
		Host:     host,
		Path:     path,
		RawQuery: r.URL.RawQuery,
	}
	return target.String()
}

func httpsPublicURLBase(publicURL string) (*url.URL, bool) {
	parsed, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, false
	}
	return parsed, true
}

func isExpectedServerStop(err error) bool {
	return errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed)
}
