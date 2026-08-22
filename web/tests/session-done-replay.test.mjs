import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import ts from "typescript";
import vm from "node:vm";

const sourcePath = path.resolve("src/services/session.ts");
const source = fs.readFileSync(sourcePath, "utf8");
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
}).outputText;

const e2eeStub = { required: false, decode: async (raw) => JSON.parse(raw) };
const sandbox = {
  exports: {},
  module: { exports: {} },
  WebSocket: { OPEN: 1, CONNECTING: 0 },
  console,
  setTimeout,
  clearTimeout,
  setInterval,
  clearInterval,
  Date,
  Math,
  JSON,
  require: (name) => {
    if (name === "./base") {
      return { appURL: (p) => p, wsURL: (p) => p };
    }
    if (name === "./api") {
      return {
        protectedFetch: async () => { throw new Error("no network in test"); },
        protectedJSON: async () => { throw new Error("no network in test"); },
      };
    }
    if (name === "./e2ee") {
      return {
        e2eeService: {
          setClientId: () => {},
          isRequired: () => e2eeStub.required,
          decodeWSMessage: (raw) => e2eeStub.decode(raw),
        },
      };
    }
    throw new Error("unexpected import: " + name);
  },
};
vm.runInNewContext(compiled, sandbox, { filename: sourcePath });

const service = sandbox.exports.sessionService;
assert.ok(service, "sessionService missing");

const deliver = (type, sessionKey, payload = {}, msg = {}) =>
  service.emitDecrypted(type, sessionKey, { session_key: sessionKey, ...payload }, msg);

// --- Gap A (bugfix B-3): done queued while no subscriber, replayed in order ---
{
  const key = "sess-replay";
  deliver("session.stream", key, { event: { type: "message_chunk", data: { text: "hi" } } });
  deliver("session.stream", key, { event: { type: "message_done" } });
  deliver("session.done", key, { root_id: "r1" });

  const seen = [];
  const unsubscribe = service.subscribe(key, {
    onStream: (event) => seen.push(`stream:${event.type}`),
    onDone: () => seen.push("done"),
    onError: (message) => seen.push(`error:${message}`),
  });
  assert.deepEqual(seen, ["stream:message_chunk", "stream:message_done", "done"],
    "queued events must replay in order and finish with done");
  unsubscribe();

  // queue is drained: a fresh subscriber replays nothing
  const again = [];
  service.subscribe(key, { onStream: () => again.push("stream"), onDone: () => again.push("done") })();
  assert.deepEqual(again, []);
}

// --- errors queue for replay too ---
{
  const key = "sess-error";
  deliver("session.stream", key, { event: { type: "message_chunk", data: { text: "x" } } });
  deliver("session.error", key, {}, { error: { message: "boom" } });
  const seen = [];
  service.subscribe(key, {
    onStream: () => seen.push("stream"),
    onDone: () => seen.push("done"),
    onError: (message) => seen.push(`error:${message}`),
  })();
  assert.deepEqual(seen, ["stream", "error:boom"]);
}

// --- live subscriber still gets events directly (no behavior change) ---
{
  const key = "sess-live";
  const seen = [];
  const unsubscribe = service.subscribe(key, {
    onStream: (event) => seen.push(`stream:${event.type}`),
    onDone: () => seen.push("done"),
  });
  deliver("session.stream", key, { event: { type: "message_chunk", data: {} } });
  deliver("session.done", key, {});
  assert.deepEqual(seen, ["stream:message_chunk", "done"]);
  unsubscribe();
}

// --- done clears the active-stream flag even without subscribers ---
{
  const key = "sess-flag";
  deliver("session.stream", key, { event: { type: "message_chunk", data: {} } });
  assert.equal(service.isSessionStreaming(key), true);
  deliver("session.done", key, {});
  assert.equal(service.isSessionStreaming(key), false);
}

// --- resolvePendingLocally (bugfix B-2): full synthetic done, replay:true ---
{
  const key = "sess-watchdog";
  // stale queued stream events must not resurrect "generating"
  deliver("session.stream", key, { event: { type: "message_chunk", data: {} } });

  const globalEvents = [];
  const unsubscribeGlobal = service.subscribeEvents((event) => globalEvents.push(event));
  service.resolvePendingLocally("r1", key);
  unsubscribeGlobal();

  const done = globalEvents.find((event) => event.type === "session.done");
  assert.ok(done, "global listeners must see the synthetic done");
  assert.equal(done.payload.replay, true, "synthetic done must be silent (replay:true)");
  assert.equal(done.payload.session_key, key);
  assert.equal(service.isSessionStreaming(key), false);

  // Stale stream backlog is dropped; the synthetic done itself queues, so a
  // later subscriber settles on the terminal state instead of "generating".
  const seen = [];
  service.subscribe(key, { onStream: () => seen.push("stream"), onDone: () => seen.push("done") })();
  assert.deepEqual(seen, ["done"], "subscriber must settle on done, with stale streams dropped");

  // idempotent
  service.resolvePendingLocally("r1", key);
}

// --- markSessionReady queues while offline, flush resends (bugfix B-4) ---
{
  assert.ok(!service.ws, "test assumes no live socket");
  await service.markSessionReady("r1", "sess-ready-a");
  await service.markSessionReady("r1", "sess-ready-b");
  await service.markSessionReady("r1", "sess-ready-a"); // dedupes by key
  assert.equal(service.pendingReadySessions.size, 2, "unsent readies must queue");

  // Socket comes back: flushing sends every queued ready.
  const frames = [];
  service.ws = { readyState: 1, send: (data) => frames.push(JSON.parse(data)) };
  service.flushPendingReadySessions();
  await new Promise((resolve) => setImmediate(resolve));
  const readyKeys = frames
    .filter((frame) => frame.type === "session.ready")
    .map((frame) => frame.payload.session_key)
    .sort();
  assert.deepEqual(readyKeys, ["sess-ready-a", "sess-ready-b"]);
  assert.equal(service.pendingReadySessions.size, 0, "queue must drain after flush");

  // While connected, a ready sends directly and does not queue.
  await service.markSessionReady("r1", "sess-ready-c");
  assert.equal(service.pendingReadySessions.size, 0);
  assert.ok(frames.some((frame) => frame.payload?.session_key === "sess-ready-c"));
  service.ws = null;
}

// --- E2EE decrypt completion order must not reorder handling (bugfix B-7) ---
{
  e2eeStub.required = true;
  // First frame (stream) decrypts slowly; second frame (done) instantly.
  e2eeStub.decode = async (raw) => {
    const parsed = JSON.parse(raw);
    if (parsed.type === "session.stream") {
      await new Promise((resolve) => setTimeout(resolve, 40));
    }
    return parsed;
  };

  const key = "sess-serial";
  const order = [];
  const unsubscribeGlobal = service.subscribeEvents((event) => {
    if (event.sessionKey === key) order.push(event.type);
  });

  service.enqueueIncomingMessage(JSON.stringify({
    type: "session.stream",
    payload: { session_key: key, event: { type: "message_chunk", data: {} } },
  }));
  service.enqueueIncomingMessage(JSON.stringify({
    type: "session.done",
    payload: { session_key: key, root_id: "r1" },
  }));
  await service.messagePipeline;
  unsubscribeGlobal();

  assert.deepEqual(order, ["session.stream", "session.done"],
    "handling must follow arrival order even when the first frame decrypts last");
  assert.equal(service.isSessionStreaming(key), false,
    "done must win: the slow stream event may not re-pin the streaming flag");
  e2eeStub.required = false;
}
