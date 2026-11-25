// Package server serves the portfolio page and its JSON API.
package server

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/ryanbehrens/software-portfolio/internal/config"
	"github.com/ryanbehrens/software-portfolio/internal/monitor"
)

// Server ties together the app list, the monitor and the static site.
type Server struct {
	apps   []config.App
	mon    *monitor.Monitor
	static fs.FS
}

// New creates a server.
func New(apps []config.App, mon *monitor.Monitor, static fs.FS) *Server {
	return &Server{apps: apps, mon: mon, static: static}
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/apps", s.handleApps)
	mux.HandleFunc("GET /api/stream", s.handleStream)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.Handle("GET /", staticCache(http.FileServerFS(s.static)))
	return securityHeaders(mux)
}

type appView struct {
	config.App
	Monitored bool             `json:"monitored"`
	Health    *monitor.Summary `json:"health,omitempty"`
}

type overall struct {
	Monitored int      `json:"monitored"`
	Online    int      `json:"online"`
	Uptime7d  *float64 `json:"uptime7d"`
}

// Payload is the body of /api/apps and of each live update.
type Payload struct {
	Apps            []appView  `json:"apps"`
	Overall         overall    `json:"overall"`
	LastRun         *time.Time `json:"lastRun,omitempty"`
	IntervalSeconds int        `json:"intervalSeconds"`
}

func (s *Server) payload() Payload {
	p := Payload{
		Apps:            make([]appView, 0, len(s.apps)),
		IntervalSeconds: int(s.mon.Interval().Seconds()),
	}
	if t := s.mon.LastRun(); !t.IsZero() {
		p.LastRun = &t
	}
	var sum float64
	var withData int
	for _, a := range s.apps {
		v := appView{App: a, Monitored: a.Monitored()}
		v.Monitor = nil // internal setting; "monitored" says the same thing
		if v.Monitored {
			h := s.mon.Summary(a.ID)
			v.Health = &h
			p.Overall.Monitored++
			if h.Status == monitor.StatusUp || h.Status == monitor.StatusSlow {
				p.Overall.Online++
			}
			if h.Uptime7d != nil {
				sum += *h.Uptime7d
				withData++
			}
		}
		p.Apps = append(p.Apps, v)
	}
	if withData > 0 {
		avg := float64(int(sum/float64(withData)*100+0.5)) / 100
		p.Overall.Uptime7d = &avg
	}
	return p
}

func (s *Server) handleApps(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	if err := json.NewEncoder(w).Encode(s.payload()); err != nil {
		log.Printf("encode /api/apps: %v", err)
	}
}

// handleStream pushes a fresh payload over Server-Sent Events after every
// round of checks, so open pages update without polling.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // keep proxies from buffering events

	updates, stop := s.mon.Subscribe()
	defer stop()

	send := func() bool {
		data, err := json.Marshal(s.payload())
		if err != nil {
			return false
		}
		if _, err := w.Write([]byte("event: status\ndata: ")); err != nil {
			return false
		}
		w.Write(data)
		w.Write([]byte("\n\n"))
		flusher.Flush()
		return true
	}
	if !send() {
		return
	}

	// A comment line every 25s keeps idle connections from being dropped.
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-updates:
			if !send() {
				return
			}
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func staticCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) > 5 && r.URL.Path[:5] == "/img/" {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}
