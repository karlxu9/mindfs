import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import ts from "typescript";
import vm from "node:vm";

const sourcePath = path.resolve("src/services/candidateCache.ts");
const source = fs.readFileSync(sourcePath, "utf8");
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.CommonJS,
    target: ts.ScriptTarget.ES2020,
  },
}).outputText;

const sandbox = {
  exports: {},
  module: { exports: {} },
  Date,
};
vm.runInNewContext(compiled, sandbox, { filename: sourcePath });

const {
  CANDIDATE_FETCH_DEBOUNCE_MS,
  storeCandidates,
  peekCandidates,
  invalidateCandidates,
  _setCandidateCacheClock,
} = sandbox.exports;

// Arrays cross the vm realm boundary, so compare by JSON rather than deep
// equality (the realm's Array prototype differs).
const names = (peek) => JSON.stringify(peek.items.map((item) => item.name));

let clock = 1_000_000;
_setCandidateCacheClock(() => clock);

const file = (name) => ({ type: "file", name });
const bucket = { rootId: "r1", type: "file" };

// --- exact hit: fresh same-query lookups skip the network ---
invalidateCandidates();
storeCandidates({ ...bucket, query: "re" }, [file("readme.md"), file("src/render.ts")]);
let peek = peekCandidates({ ...bucket, query: "re" });
assert.equal(peek.exact, true);
assert.equal(names(peek), JSON.stringify(["readme.md", "src/render.ts"]));

// A stale exact entry may only serve as a preview: files change while agents
// work, so the caller has to refetch.
clock += 16_000;
peek = peekCandidates({ ...bucket, query: "re" });
assert.equal(peek.exact, false);

// --- prefix derivation: longer queries filter the cached shorter query ---
invalidateCandidates();
storeCandidates({ ...bucket, query: "re" }, [
  file("readme.md"),
  file("src/render.ts"),
  file("core.go"),
]);
peek = peekCandidates({ ...bucket, query: "rea" });
assert.equal(peek.exact, false);
assert.equal(names(peek), JSON.stringify(["readme.md"]));

// Substring semantics mirror the server: "nd" matches mid-name.
peek = peekCandidates({ ...bucket, query: "rend" });
assert.equal(names(peek), JSON.stringify(["src/render.ts"]));

// The longest cached prefix wins over a shorter one.
storeCandidates({ ...bucket, query: "" }, [file("only-from-empty.ts")]);
peek = peekCandidates({ ...bucket, query: "rea" });
assert.equal(names(peek), JSON.stringify(["readme.md"]));

// A filtered-to-empty preview is withheld: the true match may have been cut by
// the server's truncation, so claiming "no results" would lie.
assert.equal(peekCandidates({ ...bucket, query: "zzz" }), null);

// --- file results re-sort like the server: prefix first, shorter first ---
invalidateCandidates();
storeCandidates({ ...bucket, query: "" }, [
  file("main_test.go"),
  file("main.go"),
  file("domain.go"),
]);
peek = peekCandidates({ ...bucket, query: "main" });
assert.equal(
  names(peek),
  JSON.stringify(["main.go", "main_test.go", "domain.go"]),
);

// --- command results keep their recency order ---
invalidateCandidates();
const cmd = (name) => ({ type: "command", name });
storeCandidates({ rootId: "r1", type: "command", query: "" }, [
  cmd("go test ./..."),
  cmd("go build"),
]);
peek = peekCandidates({ rootId: "r1", type: "command", query: "go" });
assert.equal(names(peek), JSON.stringify(["go test ./...", "go build"]));

// --- buckets are isolated ---
invalidateCandidates();
storeCandidates({ rootId: "r1", type: "file", query: "a" }, [file("a.ts")]);
assert.equal(peekCandidates({ rootId: "r2", type: "file", query: "a" }), null);
assert.equal(peekCandidates({ rootId: "r1", type: "prompt", query: "a" }), null);

// Space-containing root ids must not leak across buckets through the key.
storeCandidates({ rootId: "my project", type: "file", query: "x" }, [file("x.ts")]);
assert.equal(peekCandidates({ rootId: "my", type: "file", query: "x" }), null);

// Skill buckets are additionally split by agent.
storeCandidates({ rootId: "r1", type: "skill", query: "d", agent: "claude" }, [
  { type: "skill", name: "deploy" },
]);
assert.equal(
  peekCandidates({ rootId: "r1", type: "skill", query: "d", agent: "codex" }),
  null,
);
assert.equal(
  peekCandidates({ rootId: "r1", type: "skill", query: "d", agent: "claude" }).exact,
  true,
);

// --- typed invalidation only clears its own type ---
invalidateCandidates();
storeCandidates({ rootId: "r1", type: "prompt", query: "" }, [
  { type: "prompt", name: "fix the bug" },
]);
storeCandidates({ rootId: "r1", type: "file", query: "" }, [file("a.ts")]);
invalidateCandidates("prompt");
assert.equal(peekCandidates({ rootId: "r1", type: "prompt", query: "" }), null);
assert.equal(peekCandidates({ rootId: "r1", type: "file", query: "" }).exact, true);

// --- eviction keeps the store bounded and prefers recent entries ---
invalidateCandidates();
for (let i = 0; i < 100; i++) {
  storeCandidates({ rootId: "r1", type: "file", query: `q${i}` }, [file(`f${i}`)]);
}
assert.equal(peekCandidates({ rootId: "r1", type: "file", query: "q0" }), null);
assert.equal(
  peekCandidates({ rootId: "r1", type: "file", query: "q99" }).exact,
  true,
);

// The debounce constant is the single source both editors import.
assert.ok(
  CANDIDATE_FETCH_DEBOUNCE_MS >= 100 && CANDIDATE_FETCH_DEBOUNCE_MS <= 200,
  `debounce ${CANDIDATE_FETCH_DEBOUNCE_MS} out of the intended range`,
);

console.log("candidate-cache tests passed");
