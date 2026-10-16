package onionbravesearch

import (
	"bufio"
	"compress/gzip"
	"compress/zlib"
	"context"
	cryptorand "crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/proxy"
)

const (
	maxIdleConns = 4
	initialBuf   = 64 << 10
)

// transport speaks HTTP/1.1 by hand over a uTLS connection tunneled through
// SOCKS5, so both the TLS ClientHello and the header set/order are under our
// control instead of Go's.
type transport struct {
	proxyAddr string
	insecure  bool
	timeout   time.Duration
	ps        *profileState

	mu       sync.Mutex
	identity string
	idle     []*persistConn
	closed   bool
}

func newTransport(proxyAddr string, insecure bool, ps *profileState, timeout time.Duration) *transport {
	t := &transport{proxyAddr: proxyAddr, insecure: insecure, ps: ps, timeout: timeout}
	t.identity = newIdentity()
	return t
}

func newIdentity() string {
	var b [12]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return fmt.Sprintf("ob-%d", time.Now().UnixNano())
	}
	return "ob-" + hex.EncodeToString(b[:])
}

// rotate moves the following requests onto a fresh Tor circuit and a fresh
// browser fingerprint.
func (t *transport) rotate() {
	t.mu.Lock()
	t.identity = newIdentity()
	idle := t.idle
	t.idle = nil
	t.mu.Unlock()
	for _, pc := range idle {
		pc.conn.Close()
	}
	t.ps.rotate()
}

func (t *transport) closeIdle() {
	t.mu.Lock()
	t.closed = true
	idle := t.idle
	t.idle = nil
	t.mu.Unlock()
	for _, pc := range idle {
		pc.conn.Close()
	}
}

func (t *transport) idleConn() *persistConn {
	t.mu.Lock()
	defer t.mu.Unlock()
	if n := len(t.idle); n > 0 {
		pc := t.idle[n-1]
		t.idle = t.idle[:n-1]
		return pc
	}
	return nil
}

func (t *transport) putIdle(pc *persistConn) {
	pc.conn.SetDeadline(time.Time{})
	t.mu.Lock()
	if t.closed || len(t.idle) >= maxIdleConns {
		t.mu.Unlock()
		pc.conn.Close()
		return
	}
	t.idle = append(t.idle, pc)
	t.mu.Unlock()
}

type persistConn struct {
	conn     net.Conn
	br       *bufio.Reader
	bw       *bufio.Writer
	host     string
	reusable bool
}

// RoundTrip implements http.RoundTripper.
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return nil, fmt.Errorf("onionbravesearch: unsupported method %s", req.Method)
	}
	prof := t.ps.get()

	addr := dialAddr(req.URL)
	pc := t.idleConn()
	var err error
	if pc == nil {
		pc, err = t.dial(req.Context(), addr)
		if err != nil {
			return nil, err
		}
	}
	pc.host = req.URL.Host

	resp, err := t.exchange(req, pc, prof)
	if err == nil {
		return resp, nil
	}

	pc.conn.Close()
	if req.Context().Err() != nil || !retryableConnError(err) {
		return nil, err
	}

	pc, err = t.dial(req.Context(), addr)
	if err != nil {
		return nil, err
	}
	return t.exchange(req, pc, prof)
}

// dialAddr fills in the default port, which req.URL.Host omits for ordinary
// https:// URLs and the SOCKS dialer will not guess.
func dialAddr(u *url.URL) string {
	if u.Port() != "" {
		return u.Host
	}
	switch u.Scheme {
	case "https":
		return net.JoinHostPort(u.Hostname(), "443")
	case "http":
		return net.JoinHostPort(u.Hostname(), "80")
	default:
		return u.Host
	}
}

func (t *transport) exchange(req *http.Request, pc *persistConn, prof *profile) (*http.Response, error) {
	t.setDeadline(req.Context(), pc.conn)
	if err := t.writeRequest(req, pc, prof); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	resp, err := http.ReadResponse(pc.br, req)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	pc.reusable = !resp.Close && resp.ProtoMajor == 1

	body := &connBody{src: resp.Body, t: t, pc: pc}
	resp.Body = body

	if err := decodeBody(resp, body); err != nil {
		body.Close()
		return nil, err
	}
	return resp, nil
}

func (t *transport) writeRequest(req *http.Request, pc *persistConn, prof *profile) error {
	bw := pc.bw
	bw.Reset(pc.conn)

	uri := req.URL.RequestURI()
	if uri == "" {
		uri = "/"
	}
	if _, err := fmt.Fprintf(bw, "%s %s HTTP/1.1\r\n", req.Method, uri); err != nil {
		return err
	}
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	if err := writeHeader(bw, "Host", strings.TrimSuffix(host, ":443")); err != nil {
		return err
	}

	seen := map[string]bool{"Host": true}
	for _, name := range prof.order {
		key := textproto.CanonicalMIMEHeaderKey(name)
		seen[key] = true
		for _, v := range req.Header.Values(key) {
			if err := writeHeader(bw, name, v); err != nil {
				return err
			}
		}
	}

	var rest []string
	for k := range req.Header {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	rand.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	for _, k := range rest {
		for _, v := range req.Header.Values(k) {
			if err := writeHeader(bw, k, v); err != nil {
				return err
			}
		}
	}

	if _, err := bw.WriteString("\r\n"); err != nil {
		return err
	}
	return bw.Flush()
}

func writeHeader(bw *bufio.Writer, name, value string) error {
	if _, err := bw.WriteString(name); err != nil {
		return err
	}
	if _, err := bw.WriteString(": "); err != nil {
		return err
	}
	if _, err := bw.WriteString(value); err != nil {
		return err
	}
	_, err := bw.WriteString("\r\n")
	return err
}

func (t *transport) dial(ctx context.Context, host string) (*persistConn, error) {
	t.mu.Lock()
	identity := t.identity
	t.mu.Unlock()

	var (
		raw net.Conn
		err error
	)
	if t.proxyAddr == "" {
		d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		raw, err = d.DialContext(ctx, "tcp", host)
	} else {
		pd, derr := proxy.SOCKS5("tcp", t.proxyAddr, &proxy.Auth{User: identity, Password: "x"}, &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		})
		if derr != nil {
			return nil, fmt.Errorf("socks5 dialer: %w", derr)
		}
		cd, ok := pd.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("socks5 dialer: no context support")
		}
		raw, err = cd.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}

	prof := t.ps.get()
	serverName := host
	if h, _, splitErr := net.SplitHostPort(host); splitErr == nil {
		serverName = h
	}

	cfg := &utls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: t.insecure,
		MinVersion:         tls.VersionTLS12,
	}
	uconn := utls.UClient(raw, cfg, prof.hello)
	if err := uconn.BuildHandshakeState(); err != nil {
		raw.Close()
		return nil, fmt.Errorf("tls hello: %w", err)
	}
	for _, ext := range uconn.Extensions {
		if alpn, ok := ext.(*utls.ALPNExtension); ok {
			alpn.AlpnProtocols = []string{"http/1.1"}
		}
	}
	if err := uconn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, fmt.Errorf("tls handshake: %w", err)
	}

	return &persistConn{
		conn: uconn,
		br:   bufio.NewReaderSize(uconn, initialBuf),
		bw:   bufio.NewWriterSize(uconn, 4096),
		host: host,
	}, nil
}

func (t *transport) setDeadline(ctx context.Context, conn net.Conn) {
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
		return
	}
	if t.timeout > 0 {
		conn.SetDeadline(time.Now().Add(t.timeout))
	}
}

// connBody hands the connection back to the pool once the response is drained.
type connBody struct {
	src    io.ReadCloser
	t      *transport
	pc     *persistConn
	eof    bool
	closed bool
}

func (b *connBody) Read(p []byte) (int, error) {
	n, err := b.src.Read(p)
	if err == io.EOF {
		b.eof = true
	}
	return n, err
}

func (b *connBody) Close() error {
	if b.closed {
		return nil
	}
	b.closed = true
	err := b.src.Close()
	pc := b.pc
	b.pc = nil
	if pc != nil {
		if b.eof && pc.reusable {
			b.t.putIdle(pc)
		} else {
			pc.conn.Close()
		}
	}
	return err
}

// decodeBody transparently decompresses and makes sure Close still walks down
// to the pooled connection.
func decodeBody(resp *http.Response, underlying *connBody) error {
	enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	if enc == "" || enc == "identity" {
		return nil
	}

	var (
		reader io.Reader
		closer io.Closer
	)
	switch enc {
	case "gzip", "x-gzip":
		zr, err := gzip.NewReader(underlying)
		if err != nil {
			return fmt.Errorf("gzip: %w", err)
		}
		reader, closer = zr, zr
	case "deflate":
		zr, err := zlib.NewReader(underlying)
		if err != nil {
			return fmt.Errorf("deflate: %w", err)
		}
		reader, closer = zr, zr
	case "br":
		reader = brotli.NewReader(underlying)
	case "zstd":
		zr, err := zstd.NewReader(underlying)
		if err != nil {
			return fmt.Errorf("zstd: %w", err)
		}
		rc := zr.IOReadCloser()
		reader, closer = rc, rc
	default:
		return nil
	}

	resp.Body = &decodeReadCloser{r: reader, dec: closer, raw: underlying}
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	resp.Uncompressed = true
	return nil
}

type decodeReadCloser struct {
	r   io.Reader
	dec io.Closer
	raw io.Closer
}

func (d *decodeReadCloser) Read(p []byte) (int, error) { return d.r.Read(p) }

func (d *decodeReadCloser) Close() error {
	if d.dec != nil {
		d.dec.Close()
	}
	return d.raw.Close()
}

func retryableConnError(err error) bool {
	if err == nil {
		return false
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe")
}
