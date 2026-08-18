import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import ts from "typescript";
import vm from "node:vm";

const sourcePath = path.resolve("src/services/projectPath.ts");
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

const { formatProjectLocation, parentDirOf, splitPathSegments } = sandbox.exports;

// Windows keeps its backslashes so the label matches File Explorer.
assert.equal(parentDirOf("E:\\claude-workspace\\mindfs"), "E:\\claude-workspace");
assert.equal(formatProjectLocation("E:\\claude-workspace\\mindfs"), "E:\\claude-workspace");
assert.equal(
  formatProjectLocation("C:\\Users\\lenovo\\code\\work\\mindfs"),
  "…\\code\\work",
);

// A project directly on a drive root has no parent worth showing.
assert.equal(parentDirOf("C:\\mindfs"), "C:\\");
assert.equal(formatProjectLocation("C:\\mindfs"), "C:\\");

// POSIX absolute paths keep their leading separator.
assert.equal(parentDirOf("/srv/mindfs"), "/srv");
assert.equal(formatProjectLocation("/srv/mindfs"), "/srv");
assert.equal(formatProjectLocation("/home/user/dev/projects/mindfs"), "…/dev/projects");
assert.equal(parentDirOf("/mindfs"), "/");
assert.equal(formatProjectLocation("/mindfs"), "/");

// UNC prefixes survive -- splitting and rejoining segments would eat them.
assert.equal(parentDirOf("\\\\server\\share\\mindfs"), "\\\\server\\share");
assert.equal(formatProjectLocation("\\\\server\\share\\mindfs"), "\\\\server\\share");

// Trailing separators must not produce an empty last segment.
assert.equal(parentDirOf("/srv/mindfs/"), "/srv");
assert.equal(parentDirOf("E:\\claude-workspace\\mindfs\\"), "E:\\claude-workspace");

// Nothing to show rather than something misleading.
assert.equal(parentDirOf(""), "");
assert.equal(parentDirOf("   "), "");
assert.equal(parentDirOf("mindfs"), "");
assert.equal(formatProjectLocation("mindfs"), "");
assert.equal(formatProjectLocation(undefined), "");
assert.equal(formatProjectLocation(null), "");

// maxSegments is honoured.
assert.equal(formatProjectLocation("/a/b/c/d/mindfs", 1), "…/d");
assert.equal(formatProjectLocation("/a/b/c/d/mindfs", 3), "…/b/c/d");
assert.equal(formatProjectLocation("/a/b/mindfs", 5), "/a/b");

// JSON round-trip rather than deepEqual: arrays built inside the vm sandbox
// carry that realm's Array prototype, which deepStrictEqual rejects even when
// the contents match.
assert.equal(JSON.stringify(splitPathSegments("E:\\a\\\\b\\")), JSON.stringify(["E:", "a", "b"]));
assert.equal(JSON.stringify(splitPathSegments("/a//b/")), JSON.stringify(["a", "b"]));

console.log("project-path: ok");
