// Command piecemeal is a BitTorrent client with a native REST API, a
// qBittorrent-compatible WebAPI and an embedded web UI.
//
// Usage:
//
//	piecemeal                 run the daemon, configured by PIECEMEAL_* variables
//	piecemeal hash-password   read a password on stdin, print its argon2id hash
//	piecemeal openapi         print the native API's OpenAPI document
//	piecemeal healthcheck     check /healthz of the piecemeal on PIECEMEAL_ADDR, for container healthchecks
//	piecemeal version         print the version
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/term"

	"github.com/sebdanielsson/piecemeal/internal/api"
	"github.com/sebdanielsson/piecemeal/internal/auth"
	"github.com/sebdanielsson/piecemeal/internal/config"
	"github.com/sebdanielsson/piecemeal/internal/engine"
	"github.com/sebdanielsson/piecemeal/internal/fetch"
	"github.com/sebdanielsson/piecemeal/internal/logbuf"
	"github.com/sebdanielsson/piecemeal/internal/metrics"
	"github.com/sebdanielsson/piecemeal/internal/natpmp"
	"github.com/sebdanielsson/piecemeal/internal/settings"
	"github.com/sebdanielsson/piecemeal/internal/store"
	"github.com/sebdanielsson/piecemeal/internal/watch"
	"github.com/sebdanielsson/piecemeal/internal/webui"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(realMain())
}

func realMain() int {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "":
	case "version", "-version", "--version":
		fmt.Println(version)
		return 0
	case "hash-password":
		if err := hashPassword(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "openapi":
		if err := writeOpenAPI(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "healthcheck":
		if err := healthcheck(os.Getenv("PIECEMEAL_ADDR")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q; see the package documentation\n", cmd)
		return 2
	}

	cfg, err := config.FromOS()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration:", err)
		return 2
	}
	logs := logbuf.New(2000)
	log := newLogger(cfg, logs)
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, log, logs); err != nil {
		log.Error("piecemeal stopped", "error", err)
		return 1
	}
	return 0
}

func newLogger(cfg config.Config, logs *logbuf.Buffer) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewJSONHandler(os.Stderr, opts)
	if cfg.LogFormat == "text" {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(logs.Handler(h))
}

func run(ctx context.Context, cfg config.Config, log *slog.Logger, logs *logbuf.Buffer) error {
	log.Info("piecemeal starting", "version", version, "addr", cfg.Addr, "dataDir", cfg.DataDir, "downloadDir", cfg.DownloadDir)
	for _, d := range []string{cfg.DataDir, cfg.DownloadDir, cfg.WatchDir} {
		if d == "" {
			continue
		}
		if err := os.MkdirAll(d, 0o750); err != nil {
			return err
		}
	}
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Error("closing database", "error", err)
		}
	}()

	sm, err := settings.Load(ctx, st, settings.Defaults(cfg.DownloadDir), os.Environ())
	if err != nil {
		return err
	}

	// With NAT-PMP, map the port first so the client starts listening on
	// it, then follow the gateway when it moves the mapping.
	var eng atomic.Pointer[engine.Engine]
	var mapper *natpmp.Mapper
	ecfg := engine.Config{
		DataDir: cfg.DataDir, AllowedRoots: cfg.AllowedRoots, Mmap: cfg.Mmap, DisableIPv6: cfg.DisableIPv6,
		ListenHost: cfg.ListenHost,
	}
	if cfg.NATPMPGateway != nil {
		mapper = natpmp.New(cfg.NATPMPGateway, log.With("component", "natpmp"))
		first := make(chan int, 1)
		mapper.OnPort = func(port int) {
			if e := eng.Load(); e != nil {
				if err := e.SetPort(port); err != nil {
					log.Error("moving to the forwarded port", "port", port, "error", err)
				}
				return
			}
			select {
			case first <- port:
			default:
			}
		}
		go mapper.Run(ctx)
		ecfg.PortForwarded = true
		select {
		case ecfg.InitialPort = <-first:
		case <-time.After(30 * time.Second):
			log.Warn("no NAT-PMP mapping yet; starting on a random port until there is one")
		case <-ctx.Done():
			return nil
		}
	}

	e, err := engine.New(ctx, ecfg, st, sm, log.With("component", "engine"))
	if err != nil {
		return err
	}
	eng.Store(e)
	defer func() {
		if err := e.Close(); err != nil {
			log.Error("closing engine", "error", err)
		}
	}()
	if mapper != nil {
		if p := mapper.Status().Port; p != 0 && p != e.Port() {
			if err := e.SetPort(p); err != nil {
				log.Error("moving to the forwarded port", "port", p, "error", err)
			}
		}
	}

	if cfg.WatchDir != "" {
		w := &watch.Watcher{
			Dir: cfg.WatchDir, Interval: 5 * time.Second, Settle: 3 * time.Second, Adder: e,
			Log: log.With("component", "watch"),
		}
		go w.Run(ctx)
	}

	authSvc, err := auth.New(ctx, auth.Config{
		AdminUsername: cfg.AdminUsername, AdminPasswordHash: cfg.AdminPasswordHash, LocalLogin: cfg.LocalLogin,
		SecureCookies: cfg.SecureCookies, TrustedProxies: cfg.TrustedProxies,
	}, st, log.With("component", "auth"))
	if err != nil {
		return err
	}
	go pruneSessions(ctx, authSvc, log)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if mapper != nil && !mapper.Ready() {
			http.Error(w, "waiting for a NAT-PMP port mapping\n", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "ok\n")
	})
	if cfg.Metrics != "off" {
		reg := prometheus.NewRegistry()
		reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}), metrics.NewCollector(e))
		h := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
		if cfg.Metrics == "auth" {
			h = requireAuth(authSvc, h)
		}
		mux.Handle("GET /metrics", h)
	}
	nativeAPI := api.New(mux, api.Deps{
		Version: version, StartedAt: time.Now(), Engine: e, Settings: sm, Auth: authSvc,
		Fetcher: fetch.New(cfg.FetchAllowedNetworks), Logs: logs, NATPMP: mapper, Log: log.With("component", "api"),
		System: api.SystemInfo{
			DataDir: cfg.DataDir, DownloadDir: cfg.DownloadDir, WatchDir: cfg.WatchDir, AllowedRoots: cfg.AllowedRoots,
		},
	})
	mux.Handle("GET /", webui.Handler())

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No ReadTimeout or WriteTimeout: either would cut the event stream
		// (net/http cancels the request context at the read deadline). Huma
		// bounds request bodies itself.
	}
	srv.RegisterOnShutdown(nativeAPI.Close)
	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- fmt.Errorf("http server: %w", err)
		}
		close(errc)
	}()
	log.Info("piecemeal ready", "addr", cfg.Addr, "peerPort", e.Port())

	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-errc:
		return err
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("http shutdown", "error", err)
	}
	return nil
}

// requireAuth lets through sessions and API keys with the full or read-only
// scope.
func requireAuth(a *auth.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := a.Authenticate(r, auth.CookieWeb)
		if err != nil || p.Scope == auth.ScopeQBittorrent {
			w.Header().Set("WWW-Authenticate", `Bearer realm="piecemeal"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func pruneSessions(ctx context.Context, a *auth.Service, log *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if err := a.PruneSessions(ctx); err != nil && ctx.Err() == nil {
			log.Warn("pruning sessions", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func hashPassword(in *os.File, out io.Writer) error {
	var pw string
	if term.IsTerminal(int(in.Fd())) {
		fmt.Fprint(os.Stderr, "Password: ")
		b, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		pw = string(b)
	} else {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		pw = strings.TrimRight(line, "\r\n")
	}
	if pw == "" {
		return errors.New("empty password")
	}
	h, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, h)
	return err
}

// healthcheck asks the local piecemeal listening on addr (":8080" when
// empty) for /healthz. Images built on distroless have no curl or wget.
func healthcheck(addr string) error {
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("PIECEMEAL_ADDR %q: %w", addr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz") //nolint:gosec // the local server's own address
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/healthz: %s", resp.Status)
	}
	return nil
}

func writeOpenAPI(w io.Writer) error {
	a := api.New(http.NewServeMux(), api.Deps{Version: "dev"})
	b, err := json.MarshalIndent(a.OpenAPI(), "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}
