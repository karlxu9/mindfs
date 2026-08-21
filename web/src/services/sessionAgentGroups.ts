// Grouping of the session list by agent.
//
// Kept free of imports so the node test harness can transpile and run it in a
// bare vm sandbox, the same way projectPath.ts is tested.
//
// The session list is not a flat list of sessions: child sessions and their
// expand/collapse toggle rows follow their top-level parent. Grouping therefore
// walks the already-built row list in order and attributes every row to the
// agent of the most recent top-level session, so children always stay under
// their parent regardless of which agent produced them.

export type AgentGroupRowInput<T> = {
  row: T;
  // Agent of the session this row belongs to; null for structural rows
  // (child toggles) which inherit the current group.
  agent: string | null;
  // True when this row starts a new top-level session, i.e. a new attribution
  // point for the rows that follow it.
  isTopLevel: boolean;
};

export type AgentGroup<T> = {
  // Normalized group key; "" collects sessions with no agent (plugin shells,
  // command sessions) so they do not vanish when grouping is on.
  agent: string;
  topLevelCount: number;
  rows: T[];
};

export function normalizeAgentGroup(agent?: string | null): string {
  return String(agent || "").trim().toLowerCase();
}

// Groups keep the order in which their first session appears. The list is
// already sorted pinned-first then most-recent-first, so the group with the
// most recent activity naturally lands on top without a second sort pass.
export function buildAgentGroups<T>(inputs: AgentGroupRowInput<T>[]): AgentGroup<T>[] {
  const groups: AgentGroup<T>[] = [];
  const byAgent = new Map<string, AgentGroup<T>>();
  let current: AgentGroup<T> | null = null;
  for (const input of inputs) {
    if (input.isTopLevel) {
      const key = normalizeAgentGroup(input.agent);
      current = byAgent.get(key) || null;
      if (!current) {
        current = { agent: key, topLevelCount: 0, rows: [] };
        byAgent.set(key, current);
        groups.push(current);
      }
      current.topLevelCount++;
    }
    if (!current) {
      // A structural row before any top-level session should not happen, but
      // dropping it silently would hide a real session row too. Put strays in
      // the unnamed group.
      current = byAgent.get("") || null;
      if (!current) {
        current = { agent: "", topLevelCount: 0, rows: [] };
        byAgent.set("", current);
        groups.push(current);
      }
    }
    current.rows.push(input.row);
  }
  return groups;
}

// Display casing for known agents, matching the icon alt names in
// AgentIcon.tsx. Unknown agents get their first letter capitalized rather than
// being shown raw-lowercase.
const AGENT_LABELS: Record<string, string> = {
  augment: "Augment",
  claude: "Claude",
  cline: "Cline",
  codebuddy: "CodeBuddy",
  codex: "Codex",
  copilot: "Copilot",
  cursor: "Cursor",
  dsh: "DeepSeek Harness",
  gemini: "Gemini",
  grok: "Grok",
  hermes: "Hermes",
  kimi: "Kimi",
  kiro: "Kiro",
  omp: "OMP",
  openclaw: "OpenClaw",
  opencode: "OpenCode",
  pi: "Pi",
  qoder: "Qoder",
  qwen: "Qwen",
  reasonix: "Reasonix",
};

// Returns "" for the unnamed group; the component substitutes its own
// translated "other" label.
export function agentGroupLabel(agent: string): string {
  const key = normalizeAgentGroup(agent);
  if (!key) {
    return "";
  }
  const known = AGENT_LABELS[key];
  if (known) {
    return known;
  }
  return key.charAt(0).toUpperCase() + key.slice(1);
}
