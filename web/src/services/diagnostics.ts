import { appPath } from "./base";
import { protectedJSON } from "./api";

// Diagnostics + log tail service (R-4.2 / R-4.3). Types mirror the backend
// payloads in server/internal/api/{diagnostics,logs}.go.

export type DiagnosticsRoot = {
  id: string;
  path: string;
  meta_location: string;
  session_count: number;
};

export type DiagnosticsAgent = {
  name: string;
  protocol: string;
  available: boolean;
  last_probe_at?: string;
};

export type Diagnostics = {
  version: string;
  started_at: string;
  uptime_seconds: number;
  os_arch: string;
  addr: string;
  roots: DiagnosticsRoot[];
  agents: DiagnosticsAgent[];
  webpush?: { enabled?: boolean; subscription_count?: number };
  relay?: { bound?: boolean; connected?: boolean };
  scheduled?: { task_count?: number; next_run_at?: string };
  log?: { path?: string; size_bytes?: number };
};

export type LogTail = {
  path: string;
  size_bytes: number;
  lines: string[];
  truncated: boolean;
};

export const LOG_TAIL_DEFAULT_LINES = 200;
export const LOG_TAIL_MAX_LINES = 2000;

export function clampLogLines(lines: number): number {
  if (!Number.isFinite(lines) || lines < 1) return LOG_TAIL_DEFAULT_LINES;
  return Math.min(Math.floor(lines), LOG_TAIL_MAX_LINES);
}

// normalizeLogTail hardens the payload against missing fields so the panel
// never renders "undefined" rows.
export function normalizeLogTail(payload: Partial<LogTail> | null | undefined): LogTail {
  return {
    path: typeof payload?.path === "string" ? payload.path : "",
    size_bytes: typeof payload?.size_bytes === "number" ? payload.size_bytes : 0,
    lines: Array.isArray(payload?.lines)
      ? payload.lines.filter((line): line is string => typeof line === "string")
      : [],
    truncated: payload?.truncated === true,
  };
}

export function formatUptime(totalSeconds: number): string {
  if (!Number.isFinite(totalSeconds) || totalSeconds < 0) return "-";
  const seconds = Math.floor(totalSeconds);
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h ${minutes}m`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m ${seconds % 60}s`;
  return `${seconds}s`;
}

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "-";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

export async function fetchDiagnostics(): Promise<Diagnostics> {
  return protectedJSON<Diagnostics>(appPath("/api/diagnostics"));
}

export async function fetchLogTail(lines = LOG_TAIL_DEFAULT_LINES): Promise<LogTail> {
  const payload = await protectedJSON<Partial<LogTail>>(
    appPath(`/api/logs?lines=${clampLogLines(lines)}`),
  );
  return normalizeLogTail(payload);
}
