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
