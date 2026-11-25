package monitor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	slow := 3 * time.Second
	tests := []struct {
		name    string
		code    int
		err     error
		latency time.Duration
		want    Status
	}{
		{"ok", 200, nil, 100 * time.Millisecond, StatusUp},
		{"redirect target ok", 204, nil, time.Second, StatusUp},
		{"slow", 200, nil, 4 * time.Second, StatusSlow},
		{"not found", 404, nil, 50 * time.Millisecond, StatusDown},
		{"server error", 503, nil, 50 * time.Millisecond, StatusDown},
		{"network error", 0, errors.New("dial tcp: refused"), 0, StatusDown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.code, tt.err, tt.latency, slow); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCheckAllAgainstRealServers(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer up.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()
	hang := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	defer close(hang)

	m := New([]Target{
		{ID: "up", URL: up.URL},
		{ID: "broken", URL: broken.URL},
		{ID: "hangs", URL: slow.URL},
	}, Options{Timeout: 200 * time.Millisecond, RetryDelay: time.Millisecond})

	m.CheckAll(context.Background())

	if s := m.Summary("up"); s.Status != StatusUp || s.Checks != 1 || *s.Uptime24h != 100 {
		t.Errorf("up: got %+v", s)
	}
	if s := m.Summary("broken"); s.Status != StatusDown || *s.Uptime24h != 0 {
		t.Errorf("broken: got %+v", s)
	}
	if s := m.Summary("hangs"); s.Status != StatusDown {
		t.Errorf("a server that never answers should count as down, got %s", s.Status)
	}
	if m.LastRun().IsZero() {
		t.Error("LastRun should be set after a round of checks")
	}
}

func TestOneFailedCheckIsRetried(t *testing.T) {
	var calls atomic.Int32
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway) // a blip on the first try
		}
	}))
	defer flaky.Close()

	m := New([]Target{{ID: "flaky", URL: flaky.URL}}, Options{RetryDelay: time.Millisecond})
	m.CheckAll(context.Background())

	if s := m.Summary("flaky"); s.Status != StatusUp || s.Checks != 1 {
		t.Errorf("a blip that recovers on retry should record one up check, got %s with %d checks", s.Status, s.Checks)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("want exactly one retry (2 requests), got %d", n)
	}
}

func TestConcurrencyIsCapped(t *testing.T) {
	var inFlight, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		inFlight.Add(-1)
	}))
	defer srv.Close()

	var targets []Target
	for i := range 10 {
		targets = append(targets, Target{ID: string(rune('a' + i)), URL: srv.URL})
	}
	New(targets, Options{Concurrency: 3}).CheckAll(context.Background())

	if p := peak.Load(); p > 3 {
		t.Errorf("at most 3 checks should run at once, saw %d", p)
	}
}

func TestSummarizeUptimeAndHourlyBars(t *testing.T) {
	now := time.Date(2026, 11, 24, 15, 30, 0, 0, time.UTC)
	at := func(ago time.Duration, s Status, ms int64) Result {
		return Result{Time: now.Add(-ago), Status: s, LatencyMs: ms}
	}
	h := []Result{
		at(3*24*time.Hour, StatusDown, 0),            // counts toward 7 days only
		at(2*time.Hour, StatusUp, 100),               // 13:30 bucket
		at(2*time.Hour-5*time.Minute, StatusDown, 0), // same bucket: partial
		at(10*time.Minute, StatusSlow, 4000),         // slow still counts as up
		at(time.Minute, StatusUp, 200),
	}
	s := summarize(h, now)

	if s.Status != StatusUp || s.LatencyMs != 200 {
		t.Errorf("current status should come from the latest check, got %s %dms", s.Status, s.LatencyMs)
	}
	if *s.Uptime24h != 75 {
		t.Errorf("24h uptime: got %v, want 75 (3 of 4 checks)", *s.Uptime24h)
	}
	if *s.Uptime7d != 60 {
		t.Errorf("7d uptime: got %v, want 60 (3 of 5 checks)", *s.Uptime7d)
	}
	if s.AvgLatency24h != (100+4000+200)/3 {
		t.Errorf("average latency should cover successful checks only, got %d", s.AvgLatency24h)
	}
	if len(s.Hours) != 24 {
		t.Fatalf("want 24 hourly bars, got %d", len(s.Hours))
	}
	last := s.Hours[23]
	if !last.Start.Equal(time.Date(2026, 11, 24, 15, 0, 0, 0, time.UTC)) || last.Status != StatusUp {
		t.Errorf("last bar should be the current hour and up, got %+v", last)
	}
	if s.Hours[21].Status != StatusPartial {
		t.Errorf("13:00 bar mixes up and down checks, want partial, got %s", s.Hours[21].Status)
	}
	if s.Hours[0].Status != StatusUnknown {
		t.Errorf("hours with no checks should be unknown, got %s", s.Hours[0].Status)
	}
}

func TestSummaryWithNoChecks(t *testing.T) {
	s := summarize(nil, time.Now())
	if s.Status != StatusUnknown || s.Uptime24h != nil || s.LastChecked != nil {
		t.Errorf("an unchecked app should report unknown with no uptime, got %+v", s)
	}
}

func TestOldResultsArePruned(t *testing.T) {
	now := time.Now()
	h := []Result{{Time: now.Add(-8 * 24 * time.Hour)}, {Time: now.Add(-time.Hour)}}
	if got := prune(h, now.Add(-7*24*time.Hour)); len(got) != 1 {
		t.Errorf("want 1 result kept, got %d", len(got))
	}
}

func TestHistorySurvivesRestart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "state.json")
	targets := []Target{{ID: "app", URL: srv.URL}}

	New(targets, Options{StatePath: path}).CheckAll(context.Background())
	restarted := New(targets, Options{StatePath: path})

	if s := restarted.Summary("app"); s.Checks != 1 || s.Status != StatusUp {
		t.Errorf("history should be reloaded from disk, got %+v", s)
	}
}

func TestSubscribersAreNotified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	m := New([]Target{{ID: "app", URL: srv.URL}}, Options{})
	updates, stop := m.Subscribe()
	defer stop()

	m.CheckAll(context.Background())
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("subscriber was not notified after a round of checks")
	}
}
