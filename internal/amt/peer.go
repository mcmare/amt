package amt

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

type PeerConfig struct {
	Listen, Cert, Key, Token, ExitAddress, StreamPath, SiteDir string
}

type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(b []byte) (int, error) {
	n, err := f.w.Write(b)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}

func RunPeer(ctx context.Context, cfg PeerConfig, logger *log.Logger) error {
	if len(cfg.Token) < 32 {
		return errors.New("peer token must be at least 32 characters")
	}
	tc, err := serverTLS(cfg.Cert, cfg.Key)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	if cfg.StreamPath == "" || cfg.StreamPath[0] != '/' || cfg.StreamPath == "/" {
		return errors.New("stream path must start with / and cannot be /")
	}
	if cfg.SiteDir != "" {
		mux.Handle("/", http.FileServer(http.Dir(cfg.SiteDir)))
	}
	mux.HandleFunc(cfg.StreamPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.ProtoMajor != 2 {
			http.NotFound(w, r)
			return
		}
		if !tokenEqual(r.Header.Get("X-AMT-Token"), cfg.Token) {
			http.NotFound(w, r)
			return
		}
		upstream, err := net.DialTimeout("tcp", cfg.ExitAddress, 8*time.Second)
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			logger.Printf("peer exit dial: %v", err)
			return
		}
		defer upstream.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush() // Response headers reach client before upload completes.
		go func() {
			_, _ = io.Copy(upstream, r.Body)
			if tcp, ok := upstream.(*net.TCPConn); ok {
				_ = tcp.CloseWrite()
			}
		}()
		// Closing the exit TCP connection interrupts a pending response read.
		go func() { <-r.Context().Done(); _ = upstream.Close() }()
		_, _ = io.Copy(flushWriter{w}, upstream)
	})
	srv := &http.Server{Addr: cfg.Listen, Handler: mux, TLSConfig: tc, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	logger.Printf("peer HTTPS/HTTP2 listening on %s, fixed exit %s", listener.Addr(), cfg.ExitAddress)
	err = srv.Serve(tls.NewListener(listener, tc))
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
