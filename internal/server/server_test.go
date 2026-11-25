package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ryanbehrens/software-portfolio/internal/config"
	"github.com/ryanbehrens/software-portfolio/internal/monitor"
)

func setup(t *testing.T) (*httptest.Server, *monitor.Monitor) {
	t.Helper()
	app := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(app.Close)

	off := false
	apps := []config.App{
		{ID: "live", Name: "Live App", URL: app.URL},
		{ID: "store", Name: "Store Link", URL: "https://play.google.com/x", Monitor: &off},
	}
	mon := monitor.New([]monitor.Target{{ID: "live", URL: app.URL}}, monitor.Options{})
	static := fstest.MapFS{"index.html": {Data: []byte("<h1>portfolio</h1>")}}
	srv := httptest.NewServer(New(apps, mon, static).Handler())
	t.Cleanup(srv.Close)
	return srv, mon
}

func TestAppsEndpoint(t *testing.T) {
	srv, mon := setup(t)
	mon.CheckAll(context.Background())

	resp, err := http.Get(srv.URL + "/api/apps")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var p Payload
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}

	if len(p.Apps) != 2 {
		t.Fatalf("want 2 apps, got %d", len(p.Apps))
	}
	live, store := p.Apps[0], p.Apps[1]
	if !live.Monitored || live.Health == nil || live.Health.Status != monitor.StatusUp {
		t.Errorf("live app should be monitored and up, got %+v", live)
	}
	if store.Monitored || store.Health != nil {
		t.Errorf("store link should not be monitored, got %+v", store)
	}
	if p.Overall.Monitored != 1 || p.Overall.Online != 1 || *p.Overall.Uptime7d != 100 {
		t.Errorf("overall: got %+v", p.Overall)
	}
	if p.LastRun == nil {
		t.Error("lastRun should be set after checks ran")
	}
}

func TestStreamPushesUpdates(t *testing.T) {
	srv, mon := setup(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type: got %q", ct)
	}

	events := make(chan Payload)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var p Payload
				if json.Unmarshal([]byte(data), &p) == nil {
					events <- p
				}
			}
		}
	}()

	first := <-events
	if first.Apps[0].Health.Status != monitor.StatusUnknown {
		t.Errorf("before any checks the app should be unknown, got %s", first.Apps[0].Health.Status)
	}
	mon.CheckAll(context.Background())
	select {
	case next := <-events:
		if next.Apps[0].Health.Status != monitor.StatusUp {
			t.Errorf("after a check the stream should report up, got %s", next.Apps[0].Health.Status)
		}
	case <-ctx.Done():
		t.Fatal("no update arrived after a round of checks")
	}
}

func TestStaticPageAndHeaders(t *testing.T) {
	srv, _ := setup(t)
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /: status %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("security headers should be set")
	}
}
