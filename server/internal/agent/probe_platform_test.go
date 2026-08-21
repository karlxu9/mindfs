package agent

import (
	"reflect"
	"testing"
)

func TestBackgroundRuntimeProbeDisabledOnWindows(t *testing.T) {
	// Windows 只放行 ACP 协议，claude-sdk / codex-sdk 由 SDK 内部 spawn 进程，
	// MindFS 拿不到 *exec.Cmd，无法注入 CREATE_NO_WINDOW。
	if !shouldRunBackgroundRuntimeProbe("windows", ProtocolACP) {
		t.Fatalf("Windows should still background-probe ACP agents (CREATE_NO_WINDOW is injected)")
	}
	if shouldRunBackgroundRuntimeProbe("windows", ProtocolClaudeSDK) {
		t.Fatalf("Windows must not background-probe claude-sdk (SDK spawns its own process)")
	}
	if shouldRunBackgroundRuntimeProbe("windows", ProtocolCodexSDK) {
		t.Fatalf("Windows must not background-probe codex-sdk (SDK spawns its own process)")
	}
	// 非 Windows 平台一律放行，无论协议。
	if !shouldRunBackgroundRuntimeProbe("darwin", ProtocolClaudeSDK) {
		t.Fatalf("darwin should keep background runtime probes enabled")
	}
	if !shouldRunBackgroundRuntimeProbe("linux", ProtocolCodexSDK) {
		t.Fatalf("linux should keep background runtime probes enabled")
	}
}

func TestFilterBackgroundProbeDefsOnWindows(t *testing.T) {
	defs := []Definition{
		{Name: "gemini", Protocol: ProtocolACP},
		{Name: "claude", Protocol: ProtocolClaudeSDK},
		{Name: "codex", Protocol: ProtocolCodexSDK},
		// protocol 为空：claude/codex 回退到 SDK，其余回退到 ACP。
		{Name: "claude", Protocol: ""},
		{Name: "qwen", Protocol: ""},
	}

	got := filterBackgroundProbeDefs("windows", defs)
	if len(got) != 2 {
		t.Fatalf("windows filter kept %d defs, want 2: %v", len(got), got)
	}
	if got[0].Name != "gemini" || got[0].Protocol != ProtocolACP {
		t.Fatalf("windows filter should keep the explicit ACP agent first, got %+v", got[0])
	}
	if got[1].Name != "qwen" || got[1].Protocol != "" {
		t.Fatalf("windows filter should keep the empty-protocol non-claude/codex agent (falls back to ACP), got %+v", got[1])
	}
}

func TestFilterBackgroundProbeDefsOnUnixKeepsAll(t *testing.T) {
	defs := []Definition{
		{Name: "claude", Protocol: ProtocolClaudeSDK},
		{Name: "codex", Protocol: ProtocolCodexSDK},
		{Name: "gemini", Protocol: ProtocolACP},
	}
	got := filterBackgroundProbeDefs("linux", defs)
	if !reflect.DeepEqual(got, defs) {
		t.Fatalf("non-windows filter should return the input unchanged, got %v", got)
	}
	if len(got) != len(defs) {
		t.Fatalf("non-windows filter kept %d defs, want %d", len(got), len(defs))
	}
}