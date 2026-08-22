import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import ts from "typescript";
import vm from "node:vm";

const sourcePath = path.resolve("src/services/diagnostics.ts");
const source = fs.readFileSync(sourcePath, "utf8");
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
}).outputText;

const sandbox = {
  exports: {},
  module: { exports: {} },
  require: (name) => {
    if (name === "./base") return { appPath: (p) => p };
    if (name === "./api") return { protectedJSON: async () => ({}) };
    throw new Error("unexpected import: " + name);
  },
};
vm.runInNewContext(compiled, sandbox, { filename: sourcePath });

const { clampLogLines, normalizeLogTail, formatUptime, formatBytes } = sandbox.exports;

// clampLogLines mirrors the backend clamp: default 200, cap 2000.
assert.equal(clampLogLines(50), 50);
assert.equal(clampLogLines(0), 200);
assert.equal(clampLogLines(-5), 200);
assert.equal(clampLogLines(99999), 2000);
assert.equal(clampLogLines(NaN), 200);
assert.equal(clampLogLines(3.7), 3);

// normalizeLogTail hardens partial payloads.
const plain = (value) => JSON.parse(JSON.stringify(value));
assert.deepEqual(plain(normalizeLogTail(null)), { path: "", size_bytes: 0, lines: [], truncated: false });
assert.deepEqual(
  plain(normalizeLogTail({ path: "/x.log", size_bytes: 12, lines: ["a", 42, "b"], truncated: true })),
  { path: "/x.log", size_bytes: 12, lines: ["a", "b"], truncated: true },
);
assert.deepEqual(plain(normalizeLogTail({ lines: "junk" }).lines), []);

// formatUptime picks the right granularity.
assert.equal(formatUptime(42), "42s");
assert.equal(formatUptime(65), "1m 5s");
assert.equal(formatUptime(3700), "1h 1m");
assert.equal(formatUptime(90061), "1d 1h 1m");
assert.equal(formatUptime(-1), "-");

// formatBytes.
assert.equal(formatBytes(512), "512 B");
assert.equal(formatBytes(2048), "2.0 KB");
assert.equal(formatBytes(5 * 1024 * 1024), "5.0 MB");
assert.equal(formatBytes(-1), "-");

// backupExportQuery pins the query contract with the backend handler.
const { backupExportQuery } = sandbox.exports;
assert.equal(backupExportQuery("all", "ignored", true), "/api/backup/export?scope=all&include_credentials=1");
assert.equal(backupExportQuery("all", "", false), "/api/backup/export?scope=all&include_credentials=0");
assert.equal(backupExportQuery("root", "proj a", true), "/api/backup/export?scope=root&root=proj%20a&include_credentials=1");
