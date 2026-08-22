import React, { useCallback, useEffect, useState } from "react";
import { useI18n } from "../i18n";
import {
  fetchDiagnostics,
  fetchLogTail,
  formatBytes,
  formatUptime,
  type Diagnostics,
  type LogTail,
} from "../services/diagnostics";

type DiagnosticsPanelProps = {
  onClose: () => void;
};

const sectionTitleStyle: React.CSSProperties = {
  fontSize: "12px",
  fontWeight: 700,
  color: "var(--text-secondary)",
  textTransform: "uppercase",
  letterSpacing: "0.04em",
  margin: "18px 0 8px",
};

const rowStyle: React.CSSProperties = {
  display: "flex",
  justifyContent: "space-between",
  gap: "12px",
  padding: "3px 0",
  fontSize: "13px",
};

const labelStyle: React.CSSProperties = {
  color: "var(--text-secondary)",
  flexShrink: 0,
};

const valueStyle: React.CSSProperties = {
  color: "var(--text-primary)",
  textAlign: "right",
  overflowWrap: "anywhere",
  minWidth: 0,
};

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div style={rowStyle}>
      <span style={labelStyle}>{label}</span>
      <span style={valueStyle}>{value}</span>
    </div>
  );
}

export function DiagnosticsPanel({ onClose }: DiagnosticsPanelProps): React.ReactElement {
  const { t } = useI18n();
  const [diagnostics, setDiagnostics] = useState<Diagnostics | null>(null);
  const [logTail, setLogTail] = useState<LogTail | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [diag, logs] = await Promise.all([
        fetchDiagnostics(),
        fetchLogTail().catch(() => null),
      ]);
      setDiagnostics(diag);
      setLogTail(logs);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("diagnostics.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return (
    <div
      style={{
        position: "fixed",
        inset: 0,
        background: "rgba(15, 23, 42, 0.45)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: "16px",
        zIndex: 1600,
      }}
      onClick={onClose}
    >
      <div
        onClick={(event) => event.stopPropagation()}
        style={{
          width: "min(760px, 100%)",
          maxHeight: "min(86vh, 900px)",
          display: "flex",
          flexDirection: "column",
          background: "var(--content-bg, #fff)",
          color: "var(--text-primary)",
          borderRadius: "16px",
          boxShadow: "0 24px 64px rgba(15, 23, 42, 0.3)",
          overflow: "hidden",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: "10px",
            padding: "14px 18px",
            borderBottom: "1px solid var(--border-color)",
          }}
        >
          <span style={{ fontSize: "15px", fontWeight: 700, flex: 1 }}>{t("diagnostics.title")}</span>
          <button
            type="button"
            onClick={() => void refresh()}
            disabled={loading}
            style={{
              border: "1px solid var(--border-color)",
              background: "transparent",
              color: "var(--text-primary)",
              borderRadius: "8px",
              padding: "5px 12px",
              fontSize: "12px",
              cursor: loading ? "default" : "pointer",
              opacity: loading ? 0.6 : 1,
            }}
          >
            {loading ? t("diagnostics.loading") : t("diagnostics.refresh")}
          </button>
          <button
            type="button"
            onClick={onClose}
            aria-label={t("diagnostics.close")}
            style={{
              border: "none",
              background: "transparent",
              color: "var(--text-secondary)",
              fontSize: "16px",
              cursor: "pointer",
              padding: "4px 6px",
            }}
          >
            ✕
          </button>
        </div>

        <div style={{ overflowY: "auto", padding: "4px 18px 18px", minHeight: 0 }}>
          {error ? (
            <div style={{ color: "#ef4444", fontSize: "13px", padding: "12px 0" }}>{error}</div>
          ) : null}

          <div style={sectionTitleStyle}>{t("diagnostics.overview")}</div>
          {diagnostics ? (
            <div>
              <InfoRow label={t("diagnostics.version")} value={diagnostics.version || "-"} />
              <InfoRow label={t("diagnostics.uptime")} value={formatUptime(diagnostics.uptime_seconds)} />
              <InfoRow label={t("diagnostics.system")} value={diagnostics.os_arch || "-"} />
              <InfoRow label={t("diagnostics.addr")} value={diagnostics.addr || "-"} />
              <InfoRow
                label={t("diagnostics.webpush")}
                value={
                  diagnostics.webpush?.enabled
                    ? t("diagnostics.webpushEnabled", { count: diagnostics.webpush?.subscription_count ?? 0 })
                    : t("diagnostics.disabled")
                }
              />
              <InfoRow
                label={t("diagnostics.relay")}
                value={
                  diagnostics.relay?.bound
                    ? diagnostics.relay?.connected
                      ? t("diagnostics.relayConnected")
                      : t("diagnostics.relayBoundOffline")
                    : t("diagnostics.relayUnbound")
                }
              />
              <InfoRow
                label={t("diagnostics.scheduled")}
                value={
                  (diagnostics.scheduled?.task_count ?? 0) > 0
                    ? t("diagnostics.scheduledSummary", {
                        count: diagnostics.scheduled?.task_count ?? 0,
                        next: diagnostics.scheduled?.next_run_at
                          ? new Date(diagnostics.scheduled.next_run_at).toLocaleString()
                          : "-",
                      })
                    : t("diagnostics.scheduledNone")
                }
              />
              {diagnostics.roots.length > 0 ? (
                <>
                  <div style={{ ...sectionTitleStyle, margin: "12px 0 6px" }}>{t("diagnostics.roots")}</div>
                  {diagnostics.roots.map((root) => (
                    <InfoRow
                      key={root.id}
                      label={root.id}
                      value={`${root.meta_location} · ${
                        root.session_count >= 0
                          ? t("diagnostics.sessionCount", { count: root.session_count })
                          : "-"
                      }`}
                    />
                  ))}
                </>
              ) : null}
              {diagnostics.agents.length > 0 ? (
                <>
                  <div style={{ ...sectionTitleStyle, margin: "12px 0 6px" }}>{t("diagnostics.agents")}</div>
                  {diagnostics.agents.map((agent) => (
                    <InfoRow
                      key={agent.name}
                      label={`${agent.name} (${agent.protocol})`}
                      value={agent.available ? t("diagnostics.agentAvailable") : t("diagnostics.agentUnavailable")}
                    />
                  ))}
                </>
              ) : null}
            </div>
          ) : (
            <div style={{ color: "var(--text-secondary)", fontSize: "13px" }}>
              {loading ? t("diagnostics.loading") : "-"}
            </div>
          )}

          <div style={sectionTitleStyle}>{t("diagnostics.logs")}</div>
          {logTail ? (
            <div>
              <div style={{ fontSize: "11px", color: "var(--text-secondary)", marginBottom: "6px", overflowWrap: "anywhere" }}>
                {logTail.path} · {formatBytes(logTail.size_bytes)}
                {logTail.truncated ? ` · ${t("diagnostics.logTruncated")}` : ""}
              </div>
              <pre
                style={{
                  margin: 0,
                  padding: "10px 12px",
                  background: "var(--sidebar-bg, rgba(15, 23, 42, 0.04))",
                  borderRadius: "10px",
                  fontSize: "11px",
                  lineHeight: 1.5,
                  fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace",
                  overflowX: "auto",
                  whiteSpace: "pre",
                  maxHeight: "320px",
                  overflowY: "auto",
                }}
              >
                {logTail.lines.length > 0 ? logTail.lines.join("\n") : t("diagnostics.logEmpty")}
              </pre>
            </div>
          ) : (
            <div style={{ color: "var(--text-secondary)", fontSize: "13px" }}>{t("diagnostics.logUnavailable")}</div>
          )}

          <div style={sectionTitleStyle}>{t("diagnostics.storage")}</div>
          <div style={{ color: "var(--text-secondary)", fontSize: "13px" }}>
            {t("diagnostics.storagePlaceholder")}
          </div>
        </div>
      </div>
    </div>
  );
}
