import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { pruneExpiredToasts, toastExpiresAt } from "../src/components/toastModel.ts";

const now = 1_000_000;

// Fatal toasts never expire; the rest keep their original auto-hide timing.
assert.equal(toastExpiresAt("fatal", now), null);
assert.equal(toastExpiresAt("error", now), now + 5000);
assert.equal(toastExpiresAt("warning", now), now + 3000);
assert.equal(toastExpiresAt("info", now), now + 3000);

const toasts = [
  { id: "fatal", expiresAt: toastExpiresAt("fatal", now) },
  { id: "error", expiresAt: toastExpiresAt("error", now) },
  { id: "info", expiresAt: toastExpiresAt("info", now) },
];

assert.deepEqual(
  pruneExpiredToasts(toasts, now + 2999).map((t) => t.id),
  ["fatal", "error", "info"],
);
assert.deepEqual(
  pruneExpiredToasts(toasts, now + 5001).map((t) => t.id),
  ["fatal"],
);
assert.deepEqual(
  pruneExpiredToasts(toasts, now + 365 * 24 * 3600 * 1000).map((t) => t.id),
  ["fatal"],
);

// ToastContainer must not drop fatal errors and must route expiry through the model.
const toastSource = readFileSync(
  new URL("../src/components/Toast.tsx", import.meta.url),
  "utf8",
);
assert.doesNotMatch(
  toastSource,
  /severity === "fatal"\) return/,
  "Toast must not silently drop fatal errors",
);
assert.match(toastSource, /toastExpiresAt\(/);
assert.match(toastSource, /pruneExpiredToasts\(/);

// App.tsx wires both error boundaries (R-2.1).
const appSource = readFileSync(new URL("../src/App.tsx", import.meta.url), "utf8");
assert.match(appSource, /<MainViewErrorBoundary>/);
assert.match(appSource, /<DrawerPanelErrorBoundary>/);
