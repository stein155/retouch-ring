// Command retouch-ring is the Ring-chime plugin for ReTouch. ReTouch installs,
// verifies and supervises it as a child process, launching it with:
//
//	--speaker-host 127.0.0.1:8090   the speaker's local API
//	--config-dir   <dir>            where config.json + chimes live
//	--listen       127.0.0.1:<port> the loopback address ReTouch reverse-proxies
//	--host-url     http://...:8000  ReTouch's own base URL (for callbacks)
//
// It serves a small settings API (see package plugin) that ReTouch renders, and runs
// the Ring agent (package ring) once the user has logged in. It can also run
// standalone (its flags default sensibly) for testing without ReTouch.
package main

import (
	"context"
	"embed"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/stein155/retouch-ring/plugin"
)

//go:embed assets/shine.pcm assets/doorbell.pcm
var chimes embed.FS

// version is stamped at build time via -ldflags "-X main.version=<tag>" (see the
// release workflow); "dev" for local/standalone builds.
var version = "dev"

func main() {
	speaker := flagOr("--speaker-host", "127.0.0.1:8090")
	cfgDir := flagOr("--config-dir", "/mnt/nv/retouch/plugins/ring")
	listen := flagOr("--listen", "127.0.0.1:9101")
	_ = flagOr("--host-url", "") // reserved: ReTouch base URL for future callbacks

	logger := log.New(os.Stderr, "ring: ", log.LstdFlags)
	logger.Printf("retouch-ring %s starting", version)

	// Keep RSS small — a second Go runtime shares the speaker's ~120 MB with ReTouch.
	runtime.GOMAXPROCS(1)
	debug.SetGCPercent(20)
	debug.SetMemoryLimit(24 << 20)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ReTouch hands us a pipe as stdin and holds the write end for its own lifetime;
	// the kernel closes it when ReTouch dies — however it dies (self-update os.Exit,
	// crash, kill -9). Exiting on EOF means we never linger as an orphan next to a
	// relaunched ReTouch (two agents would invalidate each other's rotating Ring
	// token). Standalone runs have a terminal or /dev/null as stdin, not a pipe, so
	// the watchdog stays off there.
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeNamedPipe != 0 {
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			logger.Printf("host closed stdin; shutting down")
			stop()
			time.Sleep(3 * time.Second) // grace for the HTTP server + agent teardown
			os.Exit(0)
		}()
	}

	p, err := plugin.New(ctx, cfgDir, speaker, chimes, logger)
	if err != nil {
		logger.Fatalf("start: %v", err)
	}

	srv := &http.Server{Addr: listen, Handler: p.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sh)
	}()
	logger.Printf("listening on %s (speaker %s, config %s)", listen, speaker, cfgDir)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("listen: %v", err)
	}
}

// flagOr reads a "--name value" pair from os.Args (the plugin contract passes flags
// in that form) and returns def when the flag is absent. A tiny hand-rolled parser
// avoids the flag package's exit-on-unknown behaviour, so unknown host flags are
// simply ignored rather than crashing the plugin.
func flagOr(name, def string) string {
	for i := 0; i < len(os.Args)-1; i++ {
		if os.Args[i] == name {
			return os.Args[i+1]
		}
	}
	return def
}
