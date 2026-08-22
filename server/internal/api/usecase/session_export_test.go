package usecase

import (
	"strings"
	"testing"
	"time"

	agenttypes "mindfs/server/internal/agent/types"
	"mindfs/server/internal/session"
)

func TestRenderSessionMarkdownMultiTurnWithToolCalls(t *testing.T) {
	ts := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	sess := &session.Session{
		Key:  "sess-1",
		Name: "调查报告",
		Exchanges: []session.Exchange{
			{Seq: 1, Role: "user", Content: "帮我看看这个 bug", Timestamp: ts},
			{Seq: 2, Role: "agent", Agent: "claude", Model: "claude-fable-5", Content: "找到了，是竞态。", Timestamp: ts.Add(time.Minute)},
			{Seq: 3, Role: "user", Content: "修一下", Timestamp: ts.Add(2 * time.Minute)},
		},
		CreatedAt: ts,
	}
	aux := map[int][]session.ExchangeAux{
		2: {
			{ToolCall: &agenttypes.ToolCall{CallID: "c1", Title: "Read main.go", Status: "complete", Kind: agenttypes.ToolKindRead, Locations: []agenttypes.ToolCallLocation{{Path: "main.go"}}}},
			{ToolCall: &agenttypes.ToolCall{CallID: "c2", Title: "go test ./...", Status: "error", Kind: agenttypes.ToolKindExecute}},
			{Thought: "thinking..."},
		},
	}

	md := renderSessionMarkdown(sess, aux)

	if !strings.HasPrefix(md, "# 调查报告\n") {
		t.Fatalf("missing title:\n%s", md)
	}
	if strings.Count(md, "## 用户") != 2 {
		t.Fatalf("user turns = %d, want 2:\n%s", strings.Count(md, "## 用户"), md)
	}
	if !strings.Contains(md, "## Agent（claude · claude-fable-5）") {
		t.Fatalf("agent heading missing:\n%s", md)
	}
	if !strings.Contains(md, "> **工具调用**（2 项）") {
		t.Fatalf("tool call quote missing:\n%s", md)
	}
	if !strings.Contains(md, "> - ✓ Read main.go `main.go`") || !strings.Contains(md, "> - ✗ go test ./...") {
		t.Fatalf("tool call lines wrong:\n%s", md)
	}
	if strings.Contains(md, "thinking...") {
		t.Fatal("thoughts must not leak into the export")
	}
	// Turn order preserved.
	if strings.Index(md, "帮我看看") > strings.Index(md, "找到了") {
		t.Fatal("turn order broken")
	}
}

func TestRenderSessionMarkdownFlagsMissingUploads(t *testing.T) {
	sess := &session.Session{
		Key:  "s",
		Name: "with image",
		Exchanges: []session.Exchange{
			{Seq: 1, Role: "user", Content: "看这张图 ![screenshot](.mindfs/upload/2026-01-01/shot.png)"},
		},
	}
	md := renderSessionMarkdown(sess, nil)
	if !strings.Contains(md, "![screenshot](.mindfs/upload/2026-01-01/shot.png)") {
		t.Fatalf("image reference should stay:\n%s", md)
	}
	if !strings.Contains(md, "⚠ 图片附件未随导出：`.mindfs/upload/2026-01-01/shot.png`") {
		t.Fatalf("missing-attachment note absent:\n%s", md)
	}
}

func TestRenderSessionMarkdownEmptySession(t *testing.T) {
	md := renderSessionMarkdown(&session.Session{Key: "empty", Name: ""}, nil)
	if !strings.Contains(md, "# empty") || !strings.Contains(md, "（会话没有内容）") {
		t.Fatalf("empty session rendering wrong:\n%s", md)
	}
}

func TestExportMarkdownFilenameSanitizes(t *testing.T) {
	got := exportMarkdownFilename(&session.Session{Key: "k", Name: `a/b\c:d*e?"f<g>h|i`})
	if strings.ContainsAny(got, `/\:*?"<>|`) {
		t.Fatalf("filename not sanitized: %q", got)
	}
	if !strings.HasSuffix(got, ".md") {
		t.Fatalf("filename = %q", got)
	}
}
