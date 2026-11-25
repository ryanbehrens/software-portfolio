// Command portfolio serves Ryan Behrens's software portfolio and checks
// whether each listed app is online.
//
// Settings come from environment variables:
//
//	PORT            port to listen on (default 8080)
//	CHECK_INTERVAL  time between uptime checks, e.g. 5m or 30s (default 5m)
//	DATA_DIR        folder to save check history in, so it survives restarts
//	                (default: none, history is kept in memory only)
package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ryanbehrens/software-portfolio/internal/config"
	"github.com/ryanbehrens/software-portfolio/internal/monitor"
	"github.com/ryanbehrens/software-portfolio/internal/server"
)

//go:embed apps.json
var appsJSON []byte

//go:embed all:web
var webFiles embed.FS

func main() {
	apps, err := config.Parse(appsJSON)
	if err != nil {
		log.Fatal(err)
	}

	interval := 5 * time.Minute
	if v := os.Getenv("CHECK_INTERVAL"); v != "" {
		if interval, err = time.ParseDuration(v); err != nil || interval < 10*time.Second {
			log.Fatalf("CHECK_INTERVAL %q: use a duration of at least 10s, like 5m", v)
		}
	}
	var statePath string
	if dir := os.Getenv("DATA_DIR"); dir != "" {
		statePath = filepath.Join(dir, "status-history.json")
	}

	var targets []monitor.Target
	for _, a := range apps {
		if a.Monitored() {
			targets = append(targets, monitor.Target{ID: a.ID, URL: a.URL})
		}
	}
	mon := monitor.New(targets, monitor.Options{Interval: interval, StatePath: statePath})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go mon.Run(ctx)

	static, _ := fs.Sub(webFiles, "web")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           server.New(apps, mon, static).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()

	log.Printf("portfolio: %d apps (%d monitored every %s) on http://localhost:%s",
		len(apps), len(targets), interval, port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
