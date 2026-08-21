import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import ts from "typescript";
import vm from "node:vm";

const sourcePath = path.resolve("src/services/sessionAgentGroups.ts");
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
};
vm.runInNewContext(compiled, sandbox, { filename: sourcePath });

const { buildAgentGroups, normalizeAgentGroup, agentGroupLabel } = sandbox.exports;

// Arrays cross the vm realm boundary, so compare by JSON rather than deep
// equality (the realm's Array prototype differs).
const shape = (groups) =>
  JSON.stringify(groups.map((g) => [g.agent, g.topLevelCount, g.rows.join("+")]));

const top = (row, agent) => ({ row, agent, isTopLevel: true });
const child = (row) => ({ row, agent: null, isTopLevel: false });

// Sessions of the same agent merge into one group even when interleaved, and
// group order follows first appearance (the list is recency-sorted upstream).
let groups = buildAgentGroups([
  top("c1", "claude"),
  top("x1", "codex"),
  top("c2", "claude"),
]);
assert.equal(shape(groups), JSON.stringify([["claude", 2, "c1+c2"], ["codex", 1, "x1"]]));

// Child rows and toggle rows stay attached to their parent's group, even when
// the child was produced by a different-agent subprocess (agent is not read
// for non-top-level rows).
groups = buildAgentGroups([
  top("c1", "claude"),
  child("c1-child"),
  child("c1-toggle"),
  top("x1", "codex"),
  child("x1-child"),
]);
assert.equal(
  shape(groups),
  JSON.stringify([["claude", 1, "c1+c1-child+c1-toggle"], ["codex", 1, "x1+x1-child"]]),
);

// Sessions without an agent (plugin shells, command sessions) group under "",
// not silently dropped.
groups = buildAgentGroups([
  top("sh1", ""),
  top("c1", "claude"),
  top("sh2", undefined),
]);
assert.equal(shape(groups), JSON.stringify([["", 2, "sh1+sh2"], ["claude", 1, "c1"]]));

// Agent names normalize: case and whitespace differences are one group.
groups = buildAgentGroups([
  top("a", "Claude"),
  top("b", " claude "),
]);
assert.equal(shape(groups), JSON.stringify([["claude", 2, "a+b"]]));

// A stray structural row before any top-level session lands in the unnamed
// group rather than disappearing.
groups = buildAgentGroups([child("stray"), top("c1", "claude")]);
assert.equal(shape(groups), JSON.stringify([["", 0, "stray"], ["claude", 1, "c1"]]));

// topLevelCount counts sessions, not rows.
groups = buildAgentGroups([top("c1", "claude"), child("k1"), child("k2")]);
assert.equal(groups[0].topLevelCount, 1);
assert.equal(groups[0].rows.length, 3);

// Empty input, empty output.
assert.equal(buildAgentGroups([]).length, 0);

// --- normalization + labels ---
assert.equal(normalizeAgentGroup(" Claude "), "claude");
assert.equal(normalizeAgentGroup(undefined), "");
assert.equal(normalizeAgentGroup(null), "");

assert.equal(agentGroupLabel("claude"), "Claude");
assert.equal(agentGroupLabel("codebuddy"), "CodeBuddy");
assert.equal(agentGroupLabel("dsh"), "DeepSeek Harness");
assert.equal(agentGroupLabel("omp"), "OMP");
// Unknown agents get capitalized rather than shown raw.
assert.equal(agentGroupLabel("myagent"), "Myagent");
// The unnamed group returns "" so the component can substitute its own
// translated label.
assert.equal(agentGroupLabel(""), "");
assert.equal(agentGroupLabel("   "), "");

console.log("session-agent-groups tests passed");
