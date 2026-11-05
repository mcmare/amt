package amt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type http2Stream struct {
	body      io.ReadCloser
	sender    *io.PipeWriter
	cancel    context.CancelFunc
	remote    string
	closeOnce sync.Once
	timerMu   sync.Mutex
	timer     *time.Timer
}

func (s *http2Stream) Read(p []byte) (int, error)  { return s.body.Read(p) }
func (s *http2Stream) Write(p []byte) (int, error) { return s.sender.Write(p) }
func (s *http2Stream) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		_ = s.sender.Close()
		_ = s.body.Close()
		s.timerMu.Lock()
		if s.timer != nil {
			s.timer.Stop()
		}
		s.timerMu.Unlock()
	})
	return nil
}

type streamAddr struct{ value string }

func (a streamAddr) Network() string        { return "h2" }
func (a streamAddr) String() string         { return a.value }
func (s *http2Stream) LocalAddr() net.Addr  { return streamAddr{"local"} }
func (s *http2Stream) RemoteAddr() net.Addr { return streamAddr{s.remote} }

// h2 has no socket-level deadlines. For this MVP any deadline expiration closes
// the whole stream, rather than merely interrupting one direction.
func (s *http2Stream) SetDeadline(t time.Time) error {
	s.timerMu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if !t.IsZero() {
		d := time.Until(t)
		if d <= 0 {
			s.timerMu.Unlock()
			_ = s.Close()
			return nil
		}
		s.timer = time.AfterFunc(d, func() { _ = s.Close() })
	}
	s.timerMu.Unlock()
	return nil
}
func (s *http2Stream) SetReadDeadline(t time.Time) error  { return s.SetDeadline(t) }
func (s *http2Stream) SetWriteDeadline(t time.Time) error { return s.SetDeadline(t) }

func openHTTP2Stream(ctx context.Context, hc *http.Client, peerURL, streamPath, token string) (net.Conn, error) {
	base, err := url.Parse(peerURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.Path != "" || base.RawQuery != "" {
		return nil, errors.New("peer must be an HTTPS origin such as https://peer.example:443")
	}
	// A single HTTP/2 request/response pair transports an independent
	// bidirectional byte stream. It is not a protocol-invisibility guarantee.
	bodyReader, bodyWriter := io.Pipe()
	sessionCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(sessionCtx, http.MethodPost, strings.TrimSuffix(peerURL, "/")+streamPath, bodyReader)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("X-AMT-Token", token)
	req.Header.Set("Content-Type", "application/octet-stream")
	// Time-limit just the establishment phase; do not time-limit a live tunnel.
	openTimeout := time.AfterFunc(12*time.Second, cancel)
	resp, err := hc.Do(req)
	openTimeout.Stop()
	if err != nil {
		cancel()
		_ = bodyWriter.Close()
		_ = bodyReader.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		_ = bodyWriter.Close()
		return nil, fmt.Errorf("peer returned HTTP %d", resp.StatusCode)
	}
	if resp.ProtoMajor != 2 {
		_ = resp.Body.Close()
		cancel()
		_ = bodyWriter.Close()
		return nil, fmt.Errorf("peer used HTTP/%d, HTTP/2 required", resp.ProtoMajor)
	}
	return &http2Stream{body: resp.Body, sender: bodyWriter, cancel: cancel, remote: base.Host}, nil
}
