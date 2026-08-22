// Pending-state watchdog (bugfix B-2). The frontend "replying" flags are
// in-memory mirrors whose only reset used to be the WS session.done event;
// when that one message is lost the UI stays stuck forever. While any local
// pending session exists, this watchdog periodically reconciles against the
// server's replying-sessions list (the source of truth) and force-clears
// sessions the server no longer considers in flight. With no pending
// sessions it is fully stopped — zero standing overhead.
//
// Pure module with injected dependencies so the vm sandbox tests can drive
// it without a DOM or network.

export type PendingSessionRef = {
  rootId: string;
  sessionKey: string;
  // startedAt (epoch ms) guards against clearing a message that was sent an
  // instant ago and has not reached the server yet.
  startedAt?: number;
};

export type PendingWatchdogDeps = {
  intervalMs?: number;
  graceMs?: number;
  listLocalPending: () => PendingSessionRef[];
  fetchReplying: () => Promise<PendingSessionRef[]>;
  resolveStuck: (session: PendingSessionRef) => void;
  now?: () => number;
  setTimer?: (fn: () => void, ms: number) => unknown;
  clearTimer?: (id: unknown) => void;
};

export const PENDING_WATCHDOG_INTERVAL_MS = 10_000;
export const PENDING_WATCHDOG_GRACE_MS = 15_000;

// parsePendingCacheKeys turns App-level cache keys ("<rootId>::<sessionKey>")
// back into session refs; malformed keys are dropped.
export function parsePendingCacheKeys(
  keys: string[],
  startedAtByKey?: Record<string, number>,
): PendingSessionRef[] {
  const out: PendingSessionRef[] = [];
  for (const key of keys) {
    const splitAt = key.indexOf("::");
    if (splitAt <= 0 || splitAt + 2 >= key.length) continue;
    out.push({
      rootId: key.slice(0, splitAt),
      sessionKey: key.slice(splitAt + 2),
      startedAt: startedAtByKey?.[key],
    });
  }
  return out;
}

// findStuckSessions returns the local pending sessions the server does not
// consider replying. Entries younger than graceMs are kept pending: a just
// sent message may not have registered server-side yet, and the next tick
// will re-check it.
export function findStuckSessions(
  local: PendingSessionRef[],
  replying: PendingSessionRef[],
  now: number,
  graceMs: number,
): PendingSessionRef[] {
  const active = new Set(
    replying.map((item) => `${item.rootId}::${item.sessionKey}`),
  );
  return local.filter((item) => {
    if (active.has(`${item.rootId}::${item.sessionKey}`)) return false;
    if (typeof item.startedAt === "number" && now - item.startedAt < graceMs) {
      return false;
    }
    return true;
  });
}

export type PendingWatchdog = {
  // poke starts the watchdog if local pending sessions exist; no-op while
  // already running. Call after every place that creates a pending mirror.
  poke: () => void;
  stop: () => void;
  isRunning: () => boolean;
};

export function createPendingWatchdog(deps: PendingWatchdogDeps): PendingWatchdog {
  const intervalMs = deps.intervalMs ?? PENDING_WATCHDOG_INTERVAL_MS;
  const graceMs = deps.graceMs ?? PENDING_WATCHDOG_GRACE_MS;
  const now = deps.now ?? (() => Date.now());
  const setTimer = deps.setTimer ?? ((fn: () => void, ms: number) => setTimeout(fn, ms));
  const clearTimer = deps.clearTimer ?? ((id: unknown) => clearTimeout(id as ReturnType<typeof setTimeout>));

  let timer: unknown = null;
  let running = false;

  const schedule = () => {
    timer = setTimer(() => {
      timer = null;
      void tick();
    }, intervalMs);
  };

  const tick = async () => {
    if (!running) return;
    if (deps.listLocalPending().length === 0) {
      running = false;
      return;
    }
    try {
      const replying = await deps.fetchReplying();
      // Re-read the local list after the fetch: sessions whose done event
      // arrived meanwhile are gone already, shrinking the race window. The
      // server list stays the source of truth; resolveStuck is idempotent.
      const stuck = findStuckSessions(deps.listLocalPending(), replying, now(), graceMs);
      for (const item of stuck) {
        deps.resolveStuck(item);
      }
    } catch {
      // Network hiccup: keep the schedule, reconcile on the next tick.
    }
    if (!running || deps.listLocalPending().length === 0) {
      running = false;
      return;
    }
    schedule();
  };

  return {
    poke() {
      if (running) return;
      if (deps.listLocalPending().length === 0) return;
      running = true;
      schedule();
    },
    stop() {
      running = false;
      if (timer != null) {
        clearTimer(timer);
        timer = null;
      }
    },
    isRunning: () => running,
  };
}
