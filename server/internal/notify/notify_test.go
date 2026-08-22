package notify

import (
	"strings"
	"testing"
)

func TestBuildSessionPayloadSessionError(t *testing.T) {
	payload := BuildSessionPayload(SessionNotification{
		Type:         "session.error",
		RootID:       "root1",
		RootTitle:    "Project",
		SessionKey:   "sess1",
		SessionTitle: "My Session",
		Summary:      "agent exploded",
		EventID:      "req-42",
	})

	if payload.Type != "session.error" {
		t.Fatalf("Type = %q, want session.error", payload.Type)
	}
	if payload.Title != "Project · My Session · 出错" {
		t.Fatalf("Title = %q", payload.Title)
	}
	if payload.Body != "agent exploded" {
		t.Fatalf("Body = %q", payload.Body)
	}
	// Like session.done, each error event gets its own tag so consecutive
	// notifications do not silently replace each other.
	if payload.Tag != "mindfs:session.error:root1:sess1:req-42" {
		t.Fatalf("Tag = %q", payload.Tag)
	}
	if !payload.Renotify {
		t.Fatal("Renotify = false, want true")
	}
	if payload.RequireInteraction {
		t.Fatal("RequireInteraction = true, want false")
	}
	if !strings.Contains(payload.URL, "root=root1") || !strings.Contains(payload.URL, "session=sess1") {
		t.Fatalf("URL = %q, want root and session params", payload.URL)
	}
	if payload.Data["type"] != "session.error" || payload.Data["rootId"] != "root1" ||
		payload.Data["sessionKey"] != "sess1" || payload.Data["eventId"] != "req-42" {
		t.Fatalf("Data = %#v", payload.Data)
	}
	if EventID(payload) != "req-42" {
		t.Fatalf("EventID = %q, want req-42", EventID(payload))
	}
}

func TestBuildSessionPayloadKeepsDoneAndAskUserStatus(t *testing.T) {
	done := BuildSessionPayload(SessionNotification{Type: "session.done", RootID: "r", SessionKey: "s"})
	if !strings.HasSuffix(done.Title, "完成") {
		t.Fatalf("done Title = %q, want 完成 suffix", done.Title)
	}
	askUser := BuildSessionPayload(SessionNotification{Type: "session.ask_user", RootID: "r", SessionKey: "s"})
	if !strings.HasSuffix(askUser.Title, "需要输入") {
		t.Fatalf("ask_user Title = %q, want 需要输入 suffix", askUser.Title)
	}
	if !askUser.RequireInteraction {
		t.Fatal("ask_user RequireInteraction = false, want true")
	}
}

// The wording follows the mirrored UI language; unknown or empty falls back
// to Chinese (R-7.1).
func TestBuildPayloadsSpeakBothLanguages(t *testing.T) {
	zh := BuildSessionPayload(SessionNotification{Type: "session.done", RootID: "r", SessionKey: "s"})
	if !strings.HasSuffix(zh.Title, "完成") {
		t.Fatalf("default zh title = %q", zh.Title)
	}
	en := BuildSessionPayload(SessionNotification{Type: "session.done", RootID: "r", SessionKey: "s", Lang: "en-US"})
	if !strings.HasSuffix(en.Title, "Done") {
		t.Fatalf("en done title = %q", en.Title)
	}
	enAsk := BuildSessionPayload(SessionNotification{Type: "session.ask_user", RootID: "r", SessionKey: "s", Lang: "en-US"})
	if !strings.HasSuffix(enAsk.Title, "Input needed") {
		t.Fatalf("en ask title = %q", enAsk.Title)
	}
	enErr := BuildSessionPayload(SessionNotification{Type: "session.error", RootID: "r", SessionKey: "s", Lang: "en-US"})
	if !strings.HasSuffix(enErr.Title, "Failed") {
		t.Fatalf("en error title = %q", enErr.Title)
	}
	enSched := BuildScheduledPayload(ScheduledNotification{RootID: "r", TaskID: "t1", Success: false, Error: "x", Lang: "en-US"})
	if !strings.Contains(enSched.Title, "Scheduled task failed") || !strings.Contains(enSched.Body, "Unnamed task") {
		t.Fatalf("en scheduled = %q / %q", enSched.Title, enSched.Body)
	}
	unknown := BuildSessionPayload(SessionNotification{Type: "session.done", RootID: "r", SessionKey: "s", Lang: "fr-FR"})
	if !strings.HasSuffix(unknown.Title, "完成") {
		t.Fatalf("unknown lang should fall back to zh, got %q", unknown.Title)
	}
}
