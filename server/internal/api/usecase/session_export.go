package usecase

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	agenttypes "mindfs/server/internal/agent/types"
	"mindfs/server/internal/session"
)

// Session-to-Markdown export (R-8.1): turn structure plus tool-call
// summaries as quote blocks, for pasting into note apps. No styling beyond
// plain Markdown.

type ExportSessionMarkdownInput struct {
	RootID string
	Key    string
}

type ExportSessionMarkdownOutput struct {
	Filename string
	Content  string
}

func (s *Service) ExportSessionMarkdown(ctx context.Context, in ExportSessionMarkdownInput) (ExportSessionMarkdownOutput, error) {
	if err := s.ensureRegistry(); err != nil {
		return ExportSessionMarkdownOutput{}, err
	}
	manager, err := s.Registry.GetSessionManager(in.RootID)
	if err != nil {
		return ExportSessionMarkdownOutput{}, err
	}
	sess, err := manager.Get(ctx, strings.TrimSpace(in.Key), 0)
	if err != nil {
		return ExportSessionMarkdownOutput{}, err
	}
	aux, err := manager.GetExchangeAux(ctx, sess.Key, 0)
	if err != nil {
		aux = map[int][]session.ExchangeAux{}
	}
	return ExportSessionMarkdownOutput{
		Filename: exportMarkdownFilename(sess),
		Content:  renderSessionMarkdown(sess, aux),
	}, nil
}

func exportMarkdownFilename(sess *session.Session) string {
	name := strings.TrimSpace(sess.Name)
	if name == "" {
		name = sess.Key
	}
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', '\n', '\r':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	cleaned := strings.TrimSpace(b.String())
	if cleaned == "" {
		cleaned = "session"
	}
	if len([]rune(cleaned)) > 80 {
		cleaned = string([]rune(cleaned)[:80])
	}
	return cleaned + ".md"
}

// uploadRefPattern finds image/attachment references that point into the
// project's .mindfs/upload area; those files do not travel with a single .md
// export, so each gets an explicit missing-attachment note (R-8.1).
var uploadRefPattern = regexp.MustCompile(`!\[[^\]]*\]\(([^)]*upload/[^)]+)\)`)

func renderSessionMarkdown(sess *session.Session, aux map[int][]session.ExchangeAux) string {
	var b strings.Builder
	title := strings.TrimSpace(sess.Name)
	if title == "" {
		title = sess.Key
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "> 导出自 MindFS · 会话 `%s` · 创建于 %s\n\n", sess.Key, sess.CreatedAt.Local().Format("2006-01-02 15:04"))

	if len(sess.Exchanges) == 0 {
		b.WriteString("（会话没有内容）\n")
		return b.String()
	}
	for _, exchange := range sess.Exchanges {
		b.WriteString("---\n\n")
		b.WriteString(exchangeHeading(exchange))
		if toolCalls := auxToolCalls(aux[exchange.Seq]); len(toolCalls) > 0 {
			b.WriteString(renderToolCallQuote(toolCalls))
		}
		content := strings.TrimRight(exchange.Content, "\n")
		if content != "" {
			b.WriteString(annotateMissingUploads(content))
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

func exchangeHeading(exchange session.Exchange) string {
	when := ""
	if !exchange.Timestamp.IsZero() {
		when = " · " + exchange.Timestamp.Local().Format("2006-01-02 15:04")
	}
	if exchange.Role == "user" {
		return fmt.Sprintf("## 用户%s\n\n", when)
	}
	agent := strings.TrimSpace(exchange.Agent)
	model := firstNonBlankString(exchange.ModelDisplayName, exchange.Model)
	label := "Agent"
	switch {
	case agent != "" && model != "":
		label = fmt.Sprintf("Agent（%s · %s）", agent, model)
	case agent != "":
		label = fmt.Sprintf("Agent（%s）", agent)
	}
	return fmt.Sprintf("## %s%s\n\n", label, when)
}

func auxToolCalls(items []session.ExchangeAux) []agenttypes.ToolCall {
	var calls []agenttypes.ToolCall
	for _, item := range items {
		if item.ToolCall != nil {
			calls = append(calls, *item.ToolCall)
		}
	}
	return calls
}

// renderToolCallQuote folds the turn's tool calls into a single quote block:
// one line per call with status, kind/title and first location.
func renderToolCallQuote(calls []agenttypes.ToolCall) string {
	var b strings.Builder
	fmt.Fprintf(&b, "> **工具调用**（%d 项）\n", len(calls))
	for _, call := range calls {
		marker := "•"
		switch strings.ToLower(strings.TrimSpace(call.Status)) {
		case "complete", "completed", "success", "approved":
			marker = "✓"
		case "error", "failed", "rejected", "cancelled":
			marker = "✗"
		}
		label := strings.TrimSpace(call.Title)
		if label == "" {
			label = string(call.Kind)
		}
		label = strings.ReplaceAll(label, "\n", " ")
		if len([]rune(label)) > 120 {
			label = string([]rune(label)[:120]) + "…"
		}
		location := ""
		if len(call.Locations) > 0 && strings.TrimSpace(call.Locations[0].Path) != "" {
			location = " `" + call.Locations[0].Path + "`"
		}
		fmt.Fprintf(&b, "> - %s %s%s\n", marker, label, location)
	}
	b.WriteString("\n")
	return b.String()
}

func annotateMissingUploads(content string) string {
	matches := uploadRefPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return content
	}
	var notes strings.Builder
	seen := map[string]bool{}
	for _, match := range matches {
		path := match[1]
		if seen[path] {
			continue
		}
		seen[path] = true
		fmt.Fprintf(&notes, "\n> ⚠ 图片附件未随导出：`%s`", path)
	}
	return content + notes.String()
}

func firstNonBlankString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
