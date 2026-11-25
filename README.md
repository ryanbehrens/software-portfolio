# Ryan Behrens · Software Portfolio

My portfolio site: every app I've built and shipped, with a live status board showing whether each one is online right now.

It's a small Go service. It serves the page, checks every app on a schedule, and pushes results to open pages in real time.

## How it works

```
apps.json ──► config ──► monitor ──(every 5 min, 8 checks at a time)──► each app's URL
                            │
                            ├─ keeps 7 days of results in memory (saved to disk if DATA_DIR is set)
                            │
               server ◄─────┘
                 ├─ GET /api/apps     all apps with status, uptime and hourly history (JSON)
                 ├─ GET /api/stream   the same data, pushed live over Server-Sent Events
                 ├─ GET /healthz      for the host's health check
                 └─ GET /             the page itself (web/, embedded in the binary)
```

- **Concurrent checks** with a cap on how many run at once (goroutines + a semaphore).
- **One retry** before an app is marked down, so a momentary network blip doesn't show as an outage.
- **Live updates**: each result is pushed to every open page as soon as it arrives; slow listeners never block the checker.
- **Uptime** for the last 24 hours and 7 days, plus 24 hourly bars, computed from the stored results.
- **One binary**: the page, images and app list are embedded with `go:embed`, so deploying is copying one file.
- No dependencies outside Go's standard library.

| Package | What it does |
|---|---|
| `internal/config` | Loads and validates `apps.json` (typos in field names fail loudly) |
| `internal/monitor` | Runs checks, keeps history, computes uptime, notifies subscribers |
| `internal/server` | JSON API, live stream, static files, security headers |
| `web/` | The page: plain HTML, CSS and JavaScript |

## Add a new app

1. Add an entry to `apps.json`:

   ```json
   {
     "id": "my-new-app",
     "name": "My New App",
     "tagline": "One short line",
     "description": "Two sentences in plain English about who it's for and what it does.",
     "category": "Learning",
     "url": "https://my-new-app.vercel.app",
     "source": "https://github.com/ryanbehrens/my-new-app",
     "image": "img/my-new-app.jpg",
     "stack": ["React", "Go"]
   }
   ```

   Optional: `"featured": true` puts it in the big section at the top. For a link that isn't yours to ping (like an app store page), add `"monitor": false` and a `"badge": "On Google Play"`.

2. Take its screenshot:

   ```bash
   ./scripts/screenshot.sh my-new-app https://my-new-app.vercel.app
   ```

3. Run the tests (they check that every image exists), then commit and push. The host redeploys on its own.

   ```bash
   go test ./...
   ```

## Run it locally

```bash
CHECK_INTERVAL=30s go run .
```

Then open http://localhost:8080.

| Setting | Default | What it does |
|---|---|---|
| `PORT` | `8080` | Port to listen on |
| `CHECK_INTERVAL` | `5m` | Time between rounds of checks (minimum 10s) |
| `DATA_DIR` | none | Folder to save history in so it survives restarts |

## Deploy

### Static version (Vercel, free)

The same page also works without the Go server. `scripts/build-static.sh` copies `web/` and `apps.json` into `dist/` and marks the page as static, so it reads `apps.json` directly and hides the live status section. `vercel.json` runs that script, so importing this repo into Vercel needs no extra settings.

### Full version with live status

The checker runs inside the server, so the host must keep it running (no sleeping free tiers).

- **Render**: `render.yaml` is ready. In Render, choose New → Blueprint and pick this repo. It uses the always-on starter plan with a 1 GB disk for history.
- **Anywhere with Docker** (Fly.io, Railway, a VPS): use the `Dockerfile`.

Then point a custom domain at it and use that as the portfolio link on applications.
