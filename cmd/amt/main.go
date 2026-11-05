package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mcmare/amt/internal/amt"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := log.New(os.Stderr, "amt: ", log.LstdFlags)
	var err error
	switch os.Args[1] {
	case "exit":
		f := flag.NewFlagSet("exit", flag.ExitOnError)
		cfg := amt.ExitConfig{}
		f.StringVar(&cfg.Listen, "listen", ":9443", "TLS listen address")
		f.StringVar(&cfg.Cert, "cert", "certs/exit.crt", "exit TLS certificate")
		f.StringVar(&cfg.Key, "key", "certs/exit.key", "exit TLS key")
		f.StringVar(&cfg.Token, "token", os.Getenv("EXIT_TOKEN"), "secret exit auth token, or EXIT_TOKEN env")
		f.BoolVar(&cfg.AllowPrivate, "allow-private", false, "allow connecting to loopback/private IPs; local testing only")
		_ = f.Parse(os.Args[2:])
		err = amt.RunExit(ctx, cfg, logger)
	case "peer":
		f := flag.NewFlagSet("peer", flag.ExitOnError)
		cfg := amt.PeerConfig{}
		f.StringVar(&cfg.Listen, "listen", ":8443", "HTTPS listen address")
		f.StringVar(&cfg.Cert, "cert", "certs/peer.crt", "peer TLS certificate")
		f.StringVar(&cfg.Key, "key", "certs/peer.key", "peer TLS key")
		f.StringVar(&cfg.Token, "token", os.Getenv("PEER_TOKEN"), "secret peer auth token, or PEER_TOKEN env")
		f.StringVar(&cfg.ExitAddress, "exit", "127.0.0.1:9443", "fixed exit TLS address")
		f.StringVar(&cfg.StreamPath, "stream-path", "/_edge/stream", "secret HTTPS relay path, same on peer and client")
		f.StringVar(&cfg.SiteDir, "site-dir", "", "optional legitimate website directory to serve over HTTPS")
		_ = f.Parse(os.Args[2:])
		err = amt.RunPeer(ctx, cfg, logger)
	case "client":
		f := flag.NewFlagSet("client", flag.ExitOnError)
		cfg := amt.ClientConfig{}
		var peers string
		f.StringVar(&cfg.Listen, "listen", "127.0.0.1:1080", "local SOCKS5 listen address")
		f.StringVar(&peers, "peers", "https://127.0.0.1:8443", "comma-separated HTTPS peer origins")
		f.StringVar(&cfg.PeerToken, "peer-token", os.Getenv("PEER_TOKEN"), "shared peer token, or PEER_TOKEN env")
		f.StringVar(&cfg.StreamPath, "stream-path", "/_edge/stream", "HTTPS relay path, same on peer and client")
		f.StringVar(&cfg.PeerCA, "peer-ca", "", "peer CA PEM; empty for system trust")
		f.StringVar(&cfg.PeerServerName, "peer-server-name", "", "peer TLS server name; empty uses peer URL host")
		f.StringVar(&cfg.ExitToken, "exit-token", os.Getenv("EXIT_TOKEN"), "shared exit token, or EXIT_TOKEN env")
		f.StringVar(&cfg.ExitCA, "exit-ca", "", "exit CA PEM; empty for system trust")
		f.StringVar(&cfg.ExitServerName, "exit-server-name", "", "expected end-to-end exit certificate DNS name")
		f.BoolVar(&cfg.AllowPublicSOCKS, "allow-public-socks", false, "allow unauthenticated SOCKS5 listener on non-loopback")
		_ = f.Parse(os.Args[2:])
		for _, p := range strings.Split(peers, ",") {
			if strings.TrimSpace(p) != "" {
				cfg.Peers = append(cfg.Peers, strings.TrimSpace(p))
			}
		}
		err = amt.RunClient(ctx, cfg, logger)
	case "version":
		fmt.Println("AMT MVP 0.1 (HTTPS/2 peer relay; end-to-end TLS; SOCKS5)")
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		logger.Fatal(err)
	}
}
func usage() {
	fmt.Fprintln(os.Stderr, "usage: amt {client|peer|exit|version} [flags]\nUse amt <role> --help for options")
}
