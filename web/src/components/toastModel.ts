import type { ErrorSeverity } from "../services/error";

// Fatal errors must stay on screen until the user closes them (R-2.2),
// so they get no expiry; other severities keep the original auto-hide timing.
export function toastExpiresAt(severity: ErrorSeverity, now: number): number | null {
  if (severity === "fatal") return null;
  return now + (severity === "error" ? 5000 : 3000);
}

export function pruneExpiredToasts<T extends { expiresAt: number | null }>(
  toasts: T[],
  now: number,
): T[] {
  return toasts.filter((toast) => toast.expiresAt === null || toast.expiresAt > now);
}
