package api

import (
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

type diagnosticsRoot struct {
	ID           string `json:"id"`
	Path         string `json:"path"`
	MetaLocation string `json:"meta_location"`
	SessionCount int    `json:"session_count"`
}

type diagnosticsAgent struct {
	Name        string `json:"name"`
	Protocol    string `json:"protocol"`
	Available   bool   `json:"available"`
	LastProbeAt string `json:"last_probe_at,omitempty"`
}

type diagnosticsResponse struct {
	Version       string             `json:"version"`
	StartedAt     string             `json:"started_at"`
	UptimeSeconds int64              `json:"uptime_seconds"`
	OSArch        string             `json:"os_arch"`
	Addr          string             `json:"addr"`
	Roots         []diagnosticsRoot  `json:"roots"`
	Agents        []diagnosticsAgent `json:"agents"`
	WebPush       map[string]any     `json:"webpush"`
	Relay         map[string]any     `json:"relay"`
	Scheduled     map[string]any     `json:"scheduled"`
	Log           map[string]any     `json:"log"`
}

// handleDiagnostics aggregates in-memory service state (R-4.3). It performs
// no directory scans; the only I/O are the per-root session-count queries and
// one stat on the log file, well within the <500ms budget (N-4).
func (h *HTTPHandler) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	resp := diagnosticsResponse{
		Version: h.Version,
		OSArch:  runtime.GOOS + "/" + runtime.GOARCH,
		Addr:    h.Addr,
		Roots:   []diagnosticsRoot{},
		Agents:  []diagnosticsAgent{},
	}
	if !h.StartedAt.IsZero() {
		resp.StartedAt = h.StartedAt.UTC().Format(time.RFC3339)
		resp.UptimeSeconds = int64(now.Sub(h.StartedAt).Seconds())
	}

	app := h.AppContext
	if app != nil {
		for _, root := range app.ListRoots() {
			entry := diagnosticsRoot{
				ID:           root.ID,
				Path:         root.RootPath,
				MetaLocation: root.EffectiveMetaLocation(),
				SessionCount: -1,
			}
			if manager, err := app.GetSessionManager(root.ID); err == nil {
				if metas, err := manager.ListMetas(r.Context()); err == nil {
					entry.SessionCount = len(metas)
				}
			}
			resp.Roots = append(resp.Roots, entry)
		}
		if prober := app.GetProber(); prober != nil {
			for _, status := range prober.GetAllStatuses() {
				agent := diagnosticsAgent{
					Name:      status.Name,
					Protocol:  string(status.Protocol),
					Available: status.Available,
				}
				if !status.LastProbe.IsZero() {
					agent.LastProbeAt = status.LastProbe.UTC().Format(time.RFC3339)
				}
				resp.Agents = append(resp.Agents, agent)
			}
		}
		if app.WebPush != nil {
			status := app.WebPush.Status()
			resp.WebPush = map[string]any{
				"enabled":            status["enabled"],
				"subscription_count": status["subscription_count"],
			}
		}
		if app.Relay != nil {
			resp.Relay = map[string]any{
				"bound":     app.Relay.Status().Bound,
				"connected": app.Relay.Running(),
			}
		}
		if app.Scheduled != nil {
			summary := app.Scheduled.Summary()
			scheduled := map[string]any{"task_count": summary.TaskCount}
			if summary.NextRunAt != nil {
				scheduled["next_run_at"] = summary.NextRunAt.UTC().Format(time.RFC3339)
			}
			resp.Scheduled = scheduled
		}
	}

	if path := strings.TrimSpace(h.LogPath); path != "" {
		logInfo := map[string]any{"path": path}
		if info, err := os.Stat(path); err == nil {
			logInfo["size_bytes"] = info.Size()
		}
		resp.Log = logInfo
	}

	respondJSON(w, http.StatusOK, resp)
}
