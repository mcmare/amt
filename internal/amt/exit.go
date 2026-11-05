package amt

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"strings"
	"time"
)

type ExitConfig struct {
	Listen, Cert, Key, Token string
	AllowPrivate             bool
}

func RunExit(ctx context.Context, cfg ExitConfig, logger *log.Logger) error {
	if len(cfg.Token) < 32 {
		return errors.New("exit token must be at least 32 characters")
	}
	tc, err := serverTLS(cfg.Cert, cfg.Key)
	if err != nil {
		return err
	}
	// The exit node speaks raw TLS, not HTTP/2.
	tc.NextProtos = nil
	l, err := tls.Listen("tcp", cfg.Listen, tc)
	if err != nil {
		return err
	}
	defer l.Close()
	go func() { <-ctx.Done(); _ = l.Close() }()
	logger.Printf("exit listening on %s", l.Addr())
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
			go func() { defer func() { <-slots }(); handleExit(c, cfg, logger) }()
		default:
			_ = c.Close()
		}
	}
}

func handleExit(conn net.Conn, cfg ExitConfig, logger *log.Logger) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(12 * time.Second))
	var req ConnectRequest
	if err := readFrame(conn, &req); err != nil {
		logger.Printf("exit handshake error: %v", err)
		return
	}
	if !tokenEqual(req.Token, cfg.Token) {
		_ = writeFrame(conn, ConnectResponse{Error: "authentication failed"})
		return
	}
	target, err := dialAllowed(req.Address, cfg.AllowPrivate)
	if err != nil {
		_ = writeFrame(conn, ConnectResponse{Error: "destination unavailable or prohibited"})
		logger.Printf("exit connect rejected (destination redacted)")
		return
	}
	defer target.Close()
	if err := writeFrame(conn, ConnectResponse{OK: true}); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	bidirectional(conn, target)
}

func dialAllowed(address string, allowPrivate bool) (net.Conn, error) {
	host, port, err := validateAddress(address)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("dns: %w", err)
	}
	var failure error
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip.IP)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		if !allowPrivate && !isAllowedPublic(addr) {
			continue
		}
		c, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(addr.String(), port))
		if err == nil {
			return c, nil
		}
		failure = err
	}
	if failure == nil {
		failure = errors.New("no permitted destination IP addresses")
	}
	return nil, failure
}

func isAllowedPublic(ip netip.Addr) bool {
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	// Carrier-grade NAT, network benchmark ranges and IPv4 documentation ranges.
	forbidden := []string{"100.64.0.0/10", "198.18.0.0/15", "192.0.0.0/24", "192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32"}
	for _, s := range forbidden {
		p := netip.MustParsePrefix(s)
		if p.Contains(ip) {
			return false
		}
	}
	// IPv6 local-use and transition ranges must not reach internal systems.
	if ip.Is6() && (strings.HasPrefix(ip.String(), "fe80:") || strings.HasPrefix(ip.String(), "fc") || strings.HasPrefix(ip.String(), "fd")) {
		return false
	}
	return true
}
