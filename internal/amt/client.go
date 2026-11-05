package amt

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type ClientConfig struct {
	Listen                            string
	Peers                             []string
	PeerToken, PeerCA, PeerServerName string
	ExitToken, ExitCA, ExitServerName string
	AllowPublicSOCKS                  bool
	StreamPath                        string
}

func RunClient(ctx context.Context, cfg ClientConfig, logger *log.Logger) error {
	if len(cfg.Peers) == 0 {
		return errors.New("at least one peer URL required")
	}
	if len(cfg.PeerToken) < 32 || len(cfg.ExitToken) < 32 {
		return errors.New("peer and exit tokens must be at least 32 characters")
	}
	tCfg, err := trustConfig(cfg.PeerCA, cfg.PeerServerName)
	if err != nil {
		return err
	}
	eCfg, err := trustConfig(cfg.ExitCA, cfg.ExitServerName)
	if err != nil {
		return err
	}
	if eCfg.ServerName == "" {
		return errors.New("--exit-server-name is required")
	}
	// Explicitly refuse an unauthenticated SOCKS5 listener exposed to a network.
	host, _, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return err
	}
	if !cfg.AllowPublicSOCKS && !isLoopbackListen(host) {
		return errors.New("SOCKS5 listener must bind to loopback (unless --allow-public-socks is given)")
	}
	tr := &http.Transport{TLSClientConfig: tCfg, ForceAttemptHTTP2: true, DisableCompression: true, MaxIdleConns: 100, MaxIdleConnsPerHost: 100, IdleConnTimeout: 90 * time.Second, Proxy: nil}
	defer tr.CloseIdleConnections()
	hc := &http.Client{Transport: tr}
	l, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	defer l.Close()
	go func() { <-ctx.Done(); _ = l.Close() }()
	logger.Printf("SOCKS5 listening on %s, peers=%d", l.Addr(), len(cfg.Peers))
	slots := make(chan struct{}, 256)
	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
			go func() { defer func() { <-slots }(); handleSOCKS(c, cfg, hc, eCfg, logger) }()
		default:
			socksReply(c, 1)
			_ = c.Close()
		}
	}
}

func isLoopbackListen(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func handleSOCKS(socks net.Conn, cfg ClientConfig, hc *http.Client, exitTLS *tls.Config, logger *log.Logger) {
	defer socks.Close()
	_ = socks.SetDeadline(time.Now().Add(12 * time.Second))
	target, err := socksDestination(socks)
	if err != nil {
		logger.Printf("SOCKS handshake: %v", err)
		return
	}
	// The data channel is authenticated separately at both peer and exit.
	var lastErr error
	for _, peerURL := range cfg.Peers {
		stream, err := openHTTP2Stream(context.Background(), hc, peerURL, cfg.StreamPath, cfg.PeerToken)
		if err != nil {
			lastErr = err
			continue
		}
		tlsConn := tls.Client(stream, exitTLS.Clone())
		_ = stream.SetDeadline(time.Now().Add(12 * time.Second))
		if err = tlsConn.Handshake(); err == nil {
			err = writeFrame(tlsConn, ConnectRequest{Token: cfg.ExitToken, Address: target})
		}
		if err == nil {
			var reply ConnectResponse
			if err = readFrame(tlsConn, &reply); err == nil && !reply.OK {
				err = fmt.Errorf("exit refused destination: %s", reply.Error)
			}
		}
		if err != nil {
			lastErr = err
			_ = tlsConn.Close()
			continue
		}
		_ = stream.SetDeadline(time.Time{})
		_ = socks.SetDeadline(time.Time{})
		socksReply(socks, 0)
		// TLS CloseWrite preserves response bytes after the request body ends.
		done := make(chan struct{})
		go func() { _, _ = io.Copy(tlsConn, socks); _ = tlsConn.CloseWrite(); close(done) }()
		_, _ = io.Copy(socks, tlsConn)
		_ = tlsConn.Close()
		_ = socks.Close()
		<-done
		return
	}
	logger.Printf("all peers failed: %v", lastErr)
	socksReply(socks, 5)
}
