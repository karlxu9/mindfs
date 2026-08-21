// Client-side cache for /api/candidates lookups.
//
// Kept free of imports so the node test harness can transpile and run it in a
// bare vm sandbox, the same way projectPath.ts is tested.
//
// The contract with the server (candidates.go) that this module leans on:
// - matching is case-insensitive substring over the candidate name;
// - file/skill results are sorted prefix-match-first, then shorter name, then
//   lexicographic; command/prompt results are recency-ordered;
// - every result list is truncated to 10 items.
//
// The truncation is why a cached prefix result can only ever be an optimistic
// preview: the full match set for a longer query may include names that were
// cut from the prefix's top-10. peekCandidates therefore reports exact=true
// only for a fresh same-query hit -- that is the only case where skipping the
// network request is sound. A prefix-derived list must still be revalidated by
// the caller.

export const CANDIDATE_FETCH_DEBOUNCE_MS = 130;

export type CachedCandidate = {
  type: string;
  name: string;
  description?: string;
};

export type CandidateCacheParams = {
  rootId: string;
  type: string;
  query: string;
  agent?: string;
};

type CacheEntry = {
  query: string;
  items: CachedCandidate[];
  at: number;
};

// How long an exact hit may skip the network. Files appear and disappear while
// agents work, so this errs short; prefix previews are always revalidated and
// can use older entries.
const EXACT_FRESH_MS = 15_000;
const MAX_ENTRIES = 80;
const SERVER_RESULT_CAP = 10;

const store = new Map<string, CacheEntry>();

let now: () => number = () => Date.now();

// Test seam; production never calls this.
export function _setCandidateCacheClock(clock: () => number): void {
  now = clock;
}

// NUL cannot appear in root ids, candidate types, agent names, or typed
// queries, so it is a collision-proof separator where a space would not be.
const SEP = "\u0000";

function bucketKey(params: CandidateCacheParams): string {
  // The server only reads agent for skill lookups, so keying other types by
  // agent would just split identical results into separate entries.
  const agent = params.type === "skill" ? (params.agent || "").trim() : "";
  return `${params.rootId}${SEP}${params.type}${SEP}${agent}`;
}

function entryKey(params: CandidateCacheParams): string {
  return `${bucketKey(params)}${SEP}${params.query}`;
}

function matchesCandidateName(name: string, normalizedQuery: string): boolean {
  if (!normalizedQuery) {
    return true;
  }
  return name.toLowerCase().includes(normalizedQuery);
}

// Mirrors the server's comparator for name-sorted candidate types, so the
// optimistic preview does not visibly reshuffle when the real response lands.
function sortLikeServer(items: CachedCandidate[], normalizedQuery: string): CachedCandidate[] {
  return [...items].sort((a, b) => {
    if (normalizedQuery) {
      const aPrefix = a.name.toLowerCase().startsWith(normalizedQuery);
      const bPrefix = b.name.toLowerCase().startsWith(normalizedQuery);
      if (aPrefix !== bPrefix) {
        return aPrefix ? -1 : 1;
      }
    }
    if (a.name.length !== b.name.length) {
      return a.name.length - b.name.length;
    }
    return a.name < b.name ? -1 : a.name > b.name ? 1 : 0;
  });
}

export function storeCandidates<T extends CachedCandidate>(params: CandidateCacheParams, items: T[]): void {
  const key = entryKey(params);
  // Re-inserting moves the entry to the tail of the Map's insertion order,
  // which is what eviction below treats as most recently used.
  store.delete(key);
  store.set(key, { query: params.query, items: items.slice(), at: now() });
  while (store.size > MAX_ENTRIES) {
    const oldest = store.keys().next().value;
    if (oldest === undefined) {
      break;
    }
    store.delete(oldest);
  }
}

export type CandidatePeek<T extends CachedCandidate = CachedCandidate> = {
  items: T[];
  // True only for a fresh same-query hit; the caller may then skip the fetch.
  // False means the items are a preview derived from a shorter query and the
  // caller must still fetch to reconcile.
  exact: boolean;
};

// The generic is honest because storeCandidates only ever puts the caller's
// own items in: what comes out is what the same caller put in.
export function peekCandidates<T extends CachedCandidate>(params: CandidateCacheParams): CandidatePeek<T> | null {
  const exactEntry = store.get(entryKey(params));
  if (exactEntry && now() - exactEntry.at <= EXACT_FRESH_MS) {
    return { items: exactEntry.items.slice() as T[], exact: true };
  }

  const bucket = bucketKey(params) + SEP;
  let best: CacheEntry | null = null;
  for (const [key, entry] of store) {
    if (!key.startsWith(bucket)) {
      continue;
    }
    if (!params.query.startsWith(entry.query)) {
      continue;
    }
    if (!best || entry.query.length > best.query.length) {
      best = entry;
    }
  }
  if (!best) {
    return null;
  }
  const normalizedQuery = params.query.trim().toLowerCase();
  const filtered = best.items.filter((item) => matchesCandidateName(item.name, normalizedQuery));
  if (filtered.length === 0) {
    // Showing a confidently empty list would be wrong more often than helpful:
    // the match may live in the part the server truncated away.
    return null;
  }
  const ordered =
    params.type === "file" || params.type === "skill"
      ? sortLikeServer(filtered, normalizedQuery)
      : filtered;
  return { items: ordered.slice(0, SERVER_RESULT_CAP) as T[], exact: false };
}

export function invalidateCandidates(type?: string): void {
  if (type === undefined) {
    store.clear();
    return;
  }
  for (const key of [...store.keys()]) {
    if (key.split(SEP)[1] === type) {
      store.delete(key);
    }
  }
}
