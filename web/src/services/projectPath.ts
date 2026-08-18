const ELLIPSIS = "…";

/**
 * Windows paths keep their backslashes so the label matches what File Explorer
 * shows and stays copy-pastable. Only fall back to "/" when there is no
 * backslash to preserve.
 */
function separatorOf(path: string): string {
  return path.includes("\\") ? "\\" : "/";
}

function trimTrailingSeparators(path: string): string {
  return path.replace(/[\\/]+$/, "");
}

export function splitPathSegments(path: string): string[] {
  return path.split(/[\\/]+/).filter(Boolean);
}

/**
 * The directory containing rootPath.
 *
 * Works on the original string rather than rebuilding from segments, so path
 * prefixes survive intact -- a UNC share (\\server\share\proj) and a POSIX
 * absolute path (/srv/proj) both lose their leading separators if you split and
 * rejoin.
 *
 * Returns "" when there is no parent to speak of (a bare name, or a project
 * sitting directly at a filesystem root).
 */
export function parentDirOf(rootPath: string | null | undefined): string {
  const raw = trimTrailingSeparators(String(rootPath || "").trim());
  if (!raw) {
    return "";
  }
  const index = Math.max(raw.lastIndexOf("/"), raw.lastIndexOf("\\"));
  if (index < 0) {
    return "";
  }
  if (index === 0) {
    // "/proj" -- the parent is the filesystem root itself.
    return raw.slice(0, 1);
  }
  const parent = trimTrailingSeparators(raw.slice(0, index));
  if (!parent) {
    return raw.slice(0, 1);
  }
  if (/^[A-Za-z]:$/.test(parent)) {
    // "C:" on its own reads like a label; "C:\" reads like a drive.
    return `${parent}${separatorOf(raw)}`;
  }
  return parent;
}

/**
 * Short label for where a project lives, for display next to the project name.
 *
 * Deliberately the *parent* directory: the project's own name is already
 * rendered beside this, so ending the label with that same segment would spend
 * a row on a repeat. Long paths are cut from the left, which is the end that
 * carries the least information.
 *
 * Pair it with the full path in a title/tooltip -- this label is lossy by
 * design.
 */
export function formatProjectLocation(
  rootPath: string | null | undefined,
  maxSegments = 2,
): string {
  const parent = parentDirOf(rootPath);
  if (!parent) {
    return "";
  }
  const segments = splitPathSegments(parent);
  if (segments.length <= maxSegments) {
    return parent;
  }
  const separator = separatorOf(parent);
  return `${ELLIPSIS}${separator}${segments.slice(-maxSegments).join(separator)}`;
}
