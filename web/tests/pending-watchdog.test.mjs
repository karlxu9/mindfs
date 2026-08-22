import assert from "node:assert/strict";
import {
  createPendingWatchdog,
  findStuckSessions,
  parsePendingCacheKeys,
} from "../src/services/pendingWatchdog.ts";

// --- parsePendingCacheKeys ---
assert.deepEqual(
  parsePendingCacheKeys(["root-1::sess-a", "bad-key", "::x", "r::"], { "root-1::sess-a": 111 }),
  [{ rootId: "root-1", sessionKey: "sess-a", startedAt: 111 }],
);

// --- findStuckSessions ---
const local = [
  { rootId: "r", sessionKey: "active" },
  { rootId: "r", sessionKey: "stuck", startedAt: 0 },
  { rootId: "r", sessionKey: "fresh", startedAt: 99_000 },
];
const replying = [{ rootId: "r", sessionKey: "active" }];
const stuck = findStuckSessions(local, replying, 100_000, 15_000);
assert.deepEqual(stuck.map((s) => s.sessionKey), ["stuck"]);
// entries without startedAt are eligible immediately
assert.equal(
  findStuckSessions([{ rootId: "r", sessionKey: "no-ts" }], [], 100_000, 15_000).length,
  1,
);

// --- createPendingWatchdog with fake timers ---
function makeHarness({ replying = [], failFetch = false } = {}) {
  const state = {
    pending: [],
    resolved: [],
    timers: [],
    fetches: 0,
  };
  const watchdog = createPendingWatchdog({
    intervalMs: 10,
    graceMs: 0,
    now: () => 1_000_000,
    listLocalPending: () => [...state.pending],
    fetchReplying: async () => {
      state.fetches++;
      if (failFetch) throw new Error("network down");
      return replying;
    },
    resolveStuck: (item) => {
      state.resolved.push(item.sessionKey);
      state.pending = state.pending.filter((p) => p.sessionKey !== item.sessionKey);
    },
    setTimer: (fn) => {
      state.timers.push(fn);
      return state.timers.length;
    },
    clearTimer: () => {},
  });
  return { watchdog, state, runTimer: async () => {
    const fn = state.timers.shift();
    assert.ok(fn, "expected a scheduled timer");
    fn();
    await new Promise((resolve) => setImmediate(resolve));
  } };
}

// no pending -> poke is a no-op, nothing scheduled
{
  const { watchdog, state } = makeHarness();
  watchdog.poke();
  assert.equal(watchdog.isRunning(), false);
  assert.equal(state.timers.length, 0);
}

// stuck session gets resolved, watchdog stops once nothing is pending
{
  const { watchdog, state, runTimer } = makeHarness();
  state.pending = [{ rootId: "r", sessionKey: "s1", startedAt: 0 }];
  watchdog.poke();
  assert.equal(watchdog.isRunning(), true);
  watchdog.poke(); // idempotent while running
  assert.equal(state.timers.length, 1);
  await runTimer();
  assert.deepEqual(state.resolved, ["s1"]);
  assert.equal(watchdog.isRunning(), false);
  assert.equal(state.timers.length, 0);
}

// session the server still reports keeps its pending state (no false clear)
{
  const { watchdog, state, runTimer } = makeHarness({
    replying: [{ rootId: "r", sessionKey: "s1" }],
  });
  state.pending = [{ rootId: "r", sessionKey: "s1", startedAt: 0 }];
  watchdog.poke();
  await runTimer();
  assert.deepEqual(state.resolved, []);
  assert.equal(watchdog.isRunning(), true, "keeps polling while pending remains");
  assert.equal(state.timers.length, 1);
  watchdog.stop();
}

// fetch failure: nothing cleared, next tick scheduled
{
  const { watchdog, state, runTimer } = makeHarness({ failFetch: true });
  state.pending = [{ rootId: "r", sessionKey: "s1", startedAt: 0 }];
  watchdog.poke();
  await runTimer();
  assert.deepEqual(state.resolved, []);
  assert.equal(state.timers.length, 1);
  watchdog.stop();
}

// race with a real done: local list emptied during fetch -> no resolveStuck
{
  const state = { resolved: [], timers: [], pendingEmpty: false };
  let pending = [{ rootId: "r", sessionKey: "s1", startedAt: 0 }];
  const watchdog = createPendingWatchdog({
    intervalMs: 10,
    graceMs: 0,
    now: () => 1_000_000,
    listLocalPending: () => [...pending],
    fetchReplying: async () => {
      pending = []; // done event lands while the request is in flight
      return [];
    },
    resolveStuck: (item) => state.resolved.push(item.sessionKey),
    setTimer: (fn) => { state.timers.push(fn); return 1; },
    clearTimer: () => {},
  });
  watchdog.poke();
  const fn = state.timers.shift();
  fn();
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(state.resolved, [], "must not clear a session done already handled");
  assert.equal(watchdog.isRunning(), false);
}
