import { appPath } from "./base";
import { protectedFetch, protectedJSON } from "./api";

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

// Storage checkup + backup export (R-5.3 / R-5.4). Types mirror
// server/internal/backup/{storage,backup}.go.

export type StorageReport = {
  root_id: string;
  sessions_count: number;
  exchange_bytes: number;
  aux_bytes: number;
  debug_bytes: number;
  db_bytes: number;
  upload_bytes: number;
  orphan_debug_files: string[];
  journal_files: string[];
};

export type CleanupResult = {
  removed_debug_files: string[];
  reclaimed_journals: string[];
  remaining_journals: string[];
};

// backupExportQuery is pure so the vm sandbox test can pin the parameter
// contract with the backend.
export function backupExportQuery(scope: "root" | "all", rootId: string, includeCredentials: boolean): string {
  const parts = [`scope=${scope}`];
  if (scope === "root" && rootId) {
    parts.push(`root=${encodeURIComponent(rootId)}`);
  }
  parts.push(`include_credentials=${includeCredentials ? "1" : "0"}`);
  return `/api/backup/export?${parts.join("&")}`;
}

export async function fetchStorageReport(rootId: string): Promise<StorageReport> {
  return protectedJSON<StorageReport>(appPath(`/api/storage/report?root=${encodeURIComponent(rootId)}`));
}

export async function cleanupStorage(rootId: string): Promise<CleanupResult> {
  return protectedJSON<CleanupResult>(appPath(`/api/storage/cleanup?root=${encodeURIComponent(rootId)}`), {
    method: "POST",
  });
}

// downloadBackup fetches the archive and hands it to the browser's native
// download flow (works in the mobile PWA too).
export async function downloadBackup(scope: "root" | "all", rootId: string, includeCredentials: boolean): Promise<void> {
  const response = await protectedFetch(appPath(backupExportQuery(scope, rootId, includeCredentials)), {
    method: "POST",
  });
  if (!response.ok) {
    throw new Error(`export failed: ${response.status}`);
  }
  const disposition = response.headers.get("Content-Disposition") || "";
  const match = /filename="([^"]+)"/.exec(disposition);
  const filename = match ? match[1] : "mindfs-backup.zip";
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  try {
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = filename;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
  } finally {
    setTimeout(() => URL.revokeObjectURL(url), 10_000);
  }
}
