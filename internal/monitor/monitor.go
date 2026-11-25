// Package monitor checks whether each app is online and keeps a rolling
// history of the results.
//
// Every interval it checks all targets concurrently (with a cap on how many
// run at once), records status and latency, drops results older than the
// retention window, and notifies subscribers so the page can update live.
package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Status is the state of one check, or of a summary over many checks.
type Status string

const (
	StatusUnknown Status = "unknown" // not checked yet
	StatusUp      Status = "up"      // responded normally
	StatusSlow    Status = "slow"    // responded, but slower than SlowAfter
	StatusDown    Status = "down"    // error, timeout or a 4xx/5xx response
	StatusPartial Status = "partial" // summaries only: some checks failed
)

// Result is one check of one app.
type Result struct {
	Time      time.Time `json:"t"`
	Status    Status    `json:"s"`
	Code      int       `json:"code,omitempty"`
	LatencyMs int64     `json:"ms"`
	Err       string    `json:"err,omitempty"`
}

// Target is an app to check.
type Target struct {
	ID  string
	URL string
}

// Options tune the monitor. Zero values get sensible defaults.
type Options struct {
	Interval    time.Duration // time between check rounds (default 5m)
	Timeout     time.Duration // per-request timeout (default 10s)
	SlowAfter   time.Duration // latency that counts as slow (default 3s)
	Retention   time.Duration // how much history to keep (default 7 days)
	Concurrency int           // max checks in flight (default 8)
	RetryDelay  time.Duration // pause before re-checking a failed app (default 3s)
	StatePath   string        // if set, history is saved here and reloaded on start
	Client      *http.Client
	Now         func() time.Time
}

// Monitor runs the checks and holds the history.
type Monitor struct {
	opts    Options
	targets []Target

	mu      sync.RWMutex
	history map[string][]Result // oldest first
	lastRun time.Time

	subsMu sync.Mutex
	subs   map[chan struct{}]struct{}
}

// New creates a monitor. Call Run to start checking.
func New(targets []Target, opts Options) *Monitor {
	if opts.Interval <= 0 {
		opts.Interval = 5 * time.Minute
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.SlowAfter <= 0 {
		opts.SlowAfter = 3 * time.Second
	}
	if opts.Retention <= 0 {
		opts.Retention = 7 * 24 * time.Hour
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = 3 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: opts.Timeout}
	}
	m := &Monitor{
		opts:    opts,
		targets: targets,
		history: make(map[string][]Result, len(targets)),
		subs:    make(map[chan struct{}]struct{}),
	}
	m.load()
	return m
}

// Interval is the time between check rounds.
func (m *Monitor) Interval() time.Duration { return m.opts.Interval }

// Run checks every target right away, then again every Interval, until ctx
// is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	m.CheckAll(ctx)
	ticker := time.NewTicker(m.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.CheckAll(ctx)
		}
	}
}

// CheckAll runs one round of checks across all targets. Each result is
// recorded and pushed to subscribers as soon as it arrives, so one slow app
// never holds back the others.
func (m *Monitor) CheckAll(ctx context.Context) {
	sem := make(chan struct{}, m.opts.Concurrency)
	var wg sync.WaitGroup
	for _, t := range m.targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res := m.checkWithRetry(ctx, t)
			if ctx.Err() != nil {
				return // shutting down; don't record half-cancelled checks as outages
			}
			m.record(t.ID, res)
			m.notify()
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return
	}

	m.mu.Lock()
	m.lastRun = m.opts.Now()
	m.mu.Unlock()
	m.save()
	m.notify()
}

func (m *Monitor) record(id string, res Result) {
	cutoff := m.opts.Now().Add(-m.opts.Retention)
	m.mu.Lock()
	m.history[id] = prune(append(m.history[id], res), cutoff)
	m.mu.Unlock()
}

// checkWithRetry checks once more after a short pause before reporting an
// app as down, so a momentary network blip doesn't show up as an outage.
func (m *Monitor) checkWithRetry(ctx context.Context, t Target) Result {
	res := m.check(ctx, t)
	if res.Status != StatusDown {
		return res
	}
	select {
	case <-ctx.Done():
		return res
	case <-time.After(m.opts.RetryDelay):
	}
	return m.check(ctx, t)
}

func (m *Monitor) check(ctx context.Context, t Target) Result {
	ctx, cancel := context.WithTimeout(ctx, m.opts.Timeout)
	defer cancel()

	start := m.opts.Now()
	res := Result{Time: start}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		res.Status, res.Err = StatusDown, err.Error()
		return res
	}
	req.Header.Set("User-Agent", "ryanbehrens-portfolio-status/1.0")

	resp, err := m.opts.Client.Do(req)
	if err == nil {
		// Read a little of the body so latency covers the response actually
		// arriving, not just the headers.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		res.Code = resp.StatusCode
	}
	latency := m.opts.Now().Sub(start)
	res.LatencyMs = latency.Milliseconds()
	res.Status = Classify(res.Code, err, latency, m.opts.SlowAfter)
	if err != nil {
		res.Err = describe(err)
	}
	return res
}

// Classify turns a response into a status. Redirects are already followed
// by the HTTP client, so anything below 400 means the app answered.
func Classify(code int, err error, latency, slowAfter time.Duration) Status {
	switch {
	case err != nil, code >= 400, code == 0:
		return StatusDown
	case latency > slowAfter:
		return StatusSlow
	default:
		return StatusUp
	}
}

func describe(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return err.Error()
}

// prune drops results older than cutoff. History is oldest first.
func prune(h []Result, cutoff time.Time) []Result {
	i := 0
	for i < len(h) && h[i].Time.Before(cutoff) {
		i++
	}
	if i == 0 {
		return h
	}
	return append([]Result(nil), h[i:]...)
}

// Hour is one hourly bar on the status page.
type Hour struct {
	Start  time.Time `json:"start"`
	Status Status    `json:"status"`
	Uptime *float64  `json:"uptime,omitempty"`
}

// Summary is what the page shows for one app.
type Summary struct {
	Status        Status     `json:"status"`
	LastChecked   *time.Time `json:"lastChecked,omitempty"`
	LatencyMs     int64      `json:"latencyMs"`
	AvgLatency24h int64      `json:"avgLatency24h"`
	Uptime24h     *float64   `json:"uptime24h"`
	Uptime7d      *float64   `json:"uptime7d"`
	Checks        int        `json:"checks"`
	Hours         []Hour     `json:"hours"` // last 24 hours, oldest first
}

// LastRun is when the most recent round of checks finished.
func (m *Monitor) LastRun() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastRun
}

// Summary describes one app's recent health.
func (m *Monitor) Summary(id string) Summary {
	m.mu.RLock()
	h := m.history[id]
	m.mu.RUnlock()
	return summarize(h, m.opts.Now())
}

func summarize(h []Result, now time.Time) Summary {
	s := Summary{Status: StatusUnknown, Checks: len(h), Hours: make([]Hour, 24)}

	// Hourly buckets, aligned to the clock so bars don't shift every check.
	end := now.Truncate(time.Hour).Add(time.Hour)
	first := end.Add(-24 * time.Hour)
	var up, total [24]int
	for i := range s.Hours {
		s.Hours[i] = Hour{Start: first.Add(time.Duration(i) * time.Hour), Status: StatusUnknown}
	}

	day, week := now.Add(-24*time.Hour), now.Add(-7*24*time.Hour)
	var up24, n24, up7, n7 int
	var latSum int64
	for _, r := range h {
		ok := r.Status == StatusUp || r.Status == StatusSlow
		if !r.Time.Before(week) {
			n7++
			if ok {
				up7++
			}
		}
		if !r.Time.Before(day) {
			n24++
			if ok {
				up24++
				latSum += r.LatencyMs
			}
		}
		if b := int(r.Time.Sub(first) / time.Hour); b >= 0 && b < 24 {
			total[b]++
			if ok {
				up[b]++
			}
		}
	}

	for i := range s.Hours {
		if total[i] == 0 {
			continue
		}
		p := pct(up[i], total[i])
		s.Hours[i].Uptime = &p
		switch up[i] {
		case total[i]:
			s.Hours[i].Status = StatusUp
		case 0:
			s.Hours[i].Status = StatusDown
		default:
			s.Hours[i].Status = StatusPartial
		}
	}

	if n24 > 0 {
		p := pct(up24, n24)
		s.Uptime24h = &p
	}
	if n7 > 0 {
		p := pct(up7, n7)
		s.Uptime7d = &p
	}
	if up24 > 0 {
		s.AvgLatency24h = latSum / int64(up24)
	}
	if len(h) > 0 {
		last := h[len(h)-1]
		s.Status = last.Status
		s.LatencyMs = last.LatencyMs
		t := last.Time
		s.LastChecked = &t
	}
	return s
}

func pct(n, d int) float64 {
	// Round to two decimals so 2015/2016 shows as 99.95, not 99.950396...
	return float64(int(float64(n)/float64(d)*10000+0.5)) / 100
}

// Subscribe returns a channel that receives a signal after every round of
// checks, and a function to stop listening. Slow listeners miss signals
// rather than blocking the monitor.
func (m *Monitor) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	m.subsMu.Lock()
	m.subs[ch] = struct{}{}
	m.subsMu.Unlock()
	return ch, func() {
		m.subsMu.Lock()
		delete(m.subs, ch)
		m.subsMu.Unlock()
	}
}

func (m *Monitor) notify() {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	for ch := range m.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// --- persistence ---------------------------------------------------------

type state struct {
	LastRun time.Time           `json:"lastRun"`
	History map[string][]Result `json:"history"`
}

func (m *Monitor) load() {
	if m.opts.StatePath == "" {
		return
	}
	data, err := os.ReadFile(m.opts.StatePath)
	if err != nil {
		return
	}
	var st state
	if json.Unmarshal(data, &st) != nil {
		return
	}
	cutoff := m.opts.Now().Add(-m.opts.Retention)
	for _, t := range m.targets { // ignore apps that were removed from apps.json
		if h, ok := st.History[t.ID]; ok {
			m.history[t.ID] = prune(h, cutoff)
		}
	}
	m.lastRun = st.LastRun
}

func (m *Monitor) save() {
	if m.opts.StatePath == "" {
		return
	}
	m.mu.RLock()
	data, err := json.Marshal(state{LastRun: m.lastRun, History: m.history})
	m.mu.RUnlock()
	if err != nil {
		return
	}
	// Write to a temp file and rename, so a crash never leaves half a file.
	tmp := m.opts.StatePath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(m.opts.StatePath), 0o755); err != nil {
		return
	}
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, m.opts.StatePath)
	}
}
