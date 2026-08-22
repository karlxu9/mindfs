package notify

import (
	"fmt"
	"net/url"
	"strings"
)

const BodyMaxRunes = 600

type Payload struct {
	Type               string         `json:"type"`
	Title              string         `json:"title"`
	Body               string         `json:"body,omitempty"`
	Tag                string         `json:"tag,omitempty"`
	URL                string         `json:"url,omitempty"`
	Icon               string         `json:"icon,omitempty"`
	Badge              string         `json:"badge,omitempty"`
	Renotify           bool           `json:"renotify,omitempty"`
	RequireInteraction bool           `json:"requireInteraction,omitempty"`
	Data               map[string]any `json:"data,omitempty"`
}

type SessionNotification struct {
	Type         string
	RootID       string
	RootTitle    string
	SessionKey   string
	SessionTitle string
	Summary      string
	EventID      string
	// Lang selects the wording ("zh-CN" default, "en-US"); the server has no
	// i18n framework, so the word table lives in this package (R-7.1).
	Lang string
}

type ScheduledNotification struct {
	RootID     string
	RootTitle  string
	TaskID     string
	TaskName   string
	SessionKey string
	Summary    string
	Error      string
	Success    bool
	EventID    string
	Lang       string
}

type wordKey string

const (
	wordDone           wordKey = "done"
	wordAskUser        wordKey = "ask_user"
	wordError          wordKey = "error"
	wordSession        wordKey = "session"
	wordScheduledDone  wordKey = "scheduled_done"
	wordScheduledFail  wordKey = "scheduled_failed"
	wordUnnamedTask    wordKey = "unnamed_task"
)

var notifyWords = map[string]map[wordKey]string{
	"zh-CN": {
		wordDone:          "完成",
		wordAskUser:       "需要输入",
		wordError:         "出错",
		wordSession:       "会话",
		wordScheduledDone: "定时任务完成",
		wordScheduledFail: "定时任务失败",
		wordUnnamedTask:   "未命名任务",
	},
	"en-US": {
		wordDone:          "Done",
		wordAskUser:       "Input needed",
		wordError:         "Failed",
		wordSession:       "Session",
		wordScheduledDone: "Scheduled task done",
		wordScheduledFail: "Scheduled task failed",
		wordUnnamedTask:   "Unnamed task",
	},
}

func word(lang string, key wordKey) string {
	table, ok := notifyWords[strings.TrimSpace(lang)]
	if !ok {
		table = notifyWords["zh-CN"]
	}
	return table[key]
}

func BuildSessionPayload(in SessionNotification) Payload {
	kind := strings.TrimSpace(in.Type)
	if kind == "" {
		kind = "session.done"
	}
	status := word(in.Lang, wordDone)
	switch kind {
	case "session.ask_user":
		status = word(in.Lang, wordAskUser)
	case "session.error":
		status = word(in.Lang, wordError)
	}
	root := firstNonEmpty(in.RootTitle, in.RootID, "MindFS")
	sessionTitle := firstNonEmpty(in.SessionTitle, word(in.Lang, wordSession))
	title := fmt.Sprintf("%s · %s · %s", root, sessionTitle, status)
	body := truncateRunes(strings.TrimSpace(in.Summary), BodyMaxRunes)
	tag := fmt.Sprintf("mindfs:%s:%s:%s", kind, in.RootID, in.SessionKey)
	eventID := firstNonEmpty(in.EventID, tag)
	if kind == "session.done" || kind == "session.error" {
		tag = fmt.Sprintf("mindfs:%s:%s:%s:%s", kind, in.RootID, in.SessionKey, eventID)
	}
	return Payload{
		Type:               kind,
		Title:              title,
		Body:               body,
		Tag:                tag,
		URL:                sessionURL(in.RootID, in.SessionKey),
		Icon:               "./pwa-192.png",
		Badge:              "./pwa-192.png",
		Renotify:           kind == "session.ask_user" || kind == "session.done" || kind == "session.error",
		RequireInteraction: kind == "session.ask_user",
		Data: map[string]any{
			"type":       kind,
			"rootId":     in.RootID,
			"sessionKey": in.SessionKey,
			"eventId":    eventID,
		},
	}
}

func BuildScheduledPayload(in ScheduledNotification) Payload {
	root := firstNonEmpty(in.RootTitle, in.RootID, "MindFS")
	status := word(in.Lang, wordScheduledDone)
	kind := "scheduled.done"
	body := strings.TrimSpace(in.Summary)
	renotify := false
	if !in.Success {
		status = word(in.Lang, wordScheduledFail)
		kind = "scheduled.failed"
		body = strings.TrimSpace(in.Error)
		renotify = true
	}
	taskName := firstNonEmpty(in.TaskName, word(in.Lang, wordUnnamedTask))
	if body == "" {
		body = taskName
	} else {
		body = taskName + ": " + body
	}
	tag := fmt.Sprintf("mindfs:%s:%s:%s", kind, in.RootID, in.TaskID)
	return Payload{
		Type:               kind,
		Title:              fmt.Sprintf("%s · %s", root, status),
		Body:               truncateRunes(body, BodyMaxRunes),
		Tag:                tag,
		URL:                sessionURL(in.RootID, in.SessionKey),
		Icon:               "./pwa-192.png",
		Badge:              "./pwa-192.png",
		Renotify:           renotify,
		RequireInteraction: !in.Success,
		Data: map[string]any{
			"type":       kind,
			"rootId":     in.RootID,
			"sessionKey": in.SessionKey,
			"taskId":     in.TaskID,
			"eventId":    firstNonEmpty(in.EventID, tag),
		},
	}
}

func EventID(payload Payload) string {
	if payload.Data != nil {
		if value, ok := payload.Data["eventId"].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return strings.TrimSpace(payload.Tag)
}

func sessionURL(rootID, sessionKey string) string {
	params := make([]string, 0, 2)
	if strings.TrimSpace(rootID) != "" {
		params = append(params, "root="+url.QueryEscape(rootID))
	}
	if strings.TrimSpace(sessionKey) != "" {
		params = append(params, "session="+url.QueryEscape(sessionKey))
	}
	if len(params) == 0 {
		return "./"
	}
	return "./?" + strings.Join(params, "&")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func truncateRunes(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return "..." + string(runes[len(runes)-max:])
}
