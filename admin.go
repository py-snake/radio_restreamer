package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

const adminHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Radio Restreamer — Live Stats</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: 'Segoe UI', system-ui, sans-serif;
    background: #0f1117;
    color: #e2e8f0;
    padding: 2rem;
  }
  h1 {
    font-size: 1.5rem;
    font-weight: 600;
    margin-bottom: 0.25rem;
    color: #f8fafc;
  }
  .subtitle {
    color: #94a3b8;
    font-size: 0.85rem;
    margin-bottom: 2rem;
  }
  .stream-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
    gap: 1.25rem;
  }
  .card {
    background: #1e2230;
    border: 1px solid #2d3348;
    border-radius: 12px;
    padding: 1.25rem 1.5rem;
  }
  .card-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 1rem;
  }
  .stream-name {
    font-size: 1.05rem;
    font-weight: 600;
    color: #f1f5f9;
  }
  .stream-url {
    font-size: 0.72rem;
    color: #64748b;
    margin-top: 2px;
    word-break: break-all;
  }
  .badge {
    padding: 3px 10px;
    border-radius: 99px;
    font-size: 0.72rem;
    font-weight: 600;
    white-space: nowrap;
    flex-shrink: 0;
    margin-left: 0.75rem;
  }
  .badge-connected    { background: #14532d; color: #4ade80; }
  .badge-connecting   { background: #1e3a5f; color: #60a5fa; }
  .badge-reconnecting { background: #4c1d13; color: #f97316; }
  .badge-idle         { background: #27272a; color: #a1a1aa; }
  .metrics {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.75rem;
  }
  .metric {
    background: #141824;
    border-radius: 8px;
    padding: 0.75rem 1rem;
  }
  .metric-label {
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: #64748b;
    margin-bottom: 4px;
  }
  .metric-value {
    font-size: 1.3rem;
    font-weight: 700;
    color: #f1f5f9;
    font-variant-numeric: tabular-nums;
  }
  .metric-sub {
    font-size: 0.7rem;
    color: #475569;
    margin-top: 1px;
  }
  .port-tag {
    display: inline-block;
    background: #1e3a5f;
    color: #7dd3fc;
    font-size: 0.7rem;
    padding: 2px 8px;
    border-radius: 4px;
    margin-top: 0.75rem;
  }
  .updated {
    margin-top: 1.5rem;
    font-size: 0.75rem;
    color: #334155;
    text-align: right;
  }
  .no-streams {
    color: #475569;
    font-style: italic;
    margin-top: 1rem;
  }
</style>
</head>
<body>
<h1>Radio Restreamer</h1>
<p class="subtitle">Live statistics &mdash; updates every 2 seconds</p>
<div id="grid" class="stream-grid"></div>
<p class="updated" id="updated"></p>
<script>
function fmtBytes(b) {
  if (b < 1024) return b + ' B';
  if (b < 1048576) return (b/1024).toFixed(1) + ' KB';
  if (b < 1073741824) return (b/1048576).toFixed(2) + ' MB';
  return (b/1073741824).toFixed(3) + ' GB';
}
// Input bandwidth: always display as kbps (kilobits per second, SI)
function fmtInputKbps(bps) {
  return (bps * 8 / 1000).toFixed(1) + ' kbps';
}
// Output bandwidth: kbps below 1 Mbit/s, Mbps above
function fmtOutputBW(bps) {
  const kbps = bps * 8 / 1000;
  if (kbps < 1000) return kbps.toFixed(1) + ' kbps';
  return (kbps / 1000).toFixed(2) + ' Mbps';
}
function badgeClass(status) {
  switch(status) {
    case 'connected':    return 'badge badge-connected';
    case 'connecting':   return 'badge badge-connecting';
    case 'reconnecting': return 'badge badge-reconnecting';
    default:             return 'badge badge-idle';
  }
}
function renderCard(s) {
  return ` + "`" + `
  <div class="card">
    <div class="card-header">
      <div>
        <div class="stream-name">${escHtml(s.name)}</div>
        <div class="stream-url">${escHtml(s.source_url)}</div>
      </div>
      <span class="${badgeClass(s.status)}">${escHtml(s.status)}</span>
    </div>
    <div class="metrics">
      <div class="metric">
        <div class="metric-label">Live Listeners</div>
        <div class="metric-value">${s.listener_count}</div>
        <div class="metric-sub">&nbsp;</div>
      </div>
      <div class="metric">
        <div class="metric-label">Reconnects</div>
        <div class="metric-value">${s.reconnect_count}</div>
        <div class="metric-sub">&nbsp;</div>
      </div>
      <div class="metric">
        <div class="metric-label">Input Bandwidth</div>
        <div class="metric-value">${fmtInputKbps(s.input_bps)}</div>
        <div class="metric-sub">from upstream</div>
      </div>
      <div class="metric">
        <div class="metric-label">Output Bandwidth</div>
        <div class="metric-value">${fmtOutputBW(s.output_bps)}</div>
        <div class="metric-sub">to all listeners</div>
      </div>
      <div class="metric">
        <div class="metric-label">Upstream Total</div>
        <div class="metric-value">${fmtBytes(s.total_bytes_in)}</div>
        <div class="metric-sub">received</div>
      </div>
      <div class="metric">
        <div class="metric-label">Broadcast Total</div>
        <div class="metric-value">${fmtBytes(s.total_bytes_out)}</div>
        <div class="metric-sub">sent to listeners</div>
      </div>
    </div>
    <span class="port-tag">:${s.port}</span>
  </div>` + "`" + `;
}
function escHtml(s) {
  return String(s)
    .replace(/&/g,'&amp;')
    .replace(/</g,'&lt;')
    .replace(/>/g,'&gt;')
    .replace(/"/g,'&quot;');
}
async function refresh() {
  try {
    const r = await fetch('/api/stats');
    if (!r.ok) return;
    const streams = await r.json();
    const grid = document.getElementById('grid');
    if (!streams || streams.length === 0) {
      grid.innerHTML = '<p class="no-streams">No streams configured.</p>';
    } else {
      grid.innerHTML = streams.map(renderCard).join('');
    }
    document.getElementById('updated').textContent =
      'Last updated: ' + new Date().toLocaleTimeString();
  } catch(e) { /* network hiccup, ignore */ }
}
refresh();
setInterval(refresh, 2000);
</script>
</body>
</html>`

// AdminServer serves the live stats web UI and JSON API.
type AdminServer struct {
	port     int
	managers []*StreamManager
}

// NewAdminServer creates an AdminServer for the given managers.
func NewAdminServer(port int, managers []*StreamManager) *AdminServer {
	return &AdminServer{port: port, managers: managers}
}

// Run starts the admin HTTP server and shuts it down when ctx is cancelled.
func (a *AdminServer) Run(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/api/stats", a.handleStats)

	srv := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", a.port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Printf("[admin] shutdown: %v", err)
		}
	}()

	log.Printf("[admin] stats UI available at http://localhost:%d/", a.port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[admin] server error: %v", err)
	}
}

func (a *AdminServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, adminHTML)
}

func (a *AdminServer) handleStats(w http.ResponseWriter, r *http.Request) {
	snapshots := make([]StreamSnapshot, len(a.managers))
	for i, m := range a.managers {
		snapshots[i] = m.Snapshot()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(snapshots)
}
