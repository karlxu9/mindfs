#!/bin/sh
# MindFS notify-script example: forward events to a desktop notification.
# Usage: mindfs -notify-script /path/to/notify-example.sh  (chmod +x first)
# Payload JSON arrives on stdin; see docs/notify-script.md for the contract.

payload=$(cat)
[ -n "$payload" ] || exit 0

json_field() {
    printf '%s' "$payload" | sed -n "s/.*\"$1\":\"\\([^\"]*\\)\".*/\\1/p" | head -n 1
}

title=$(json_field title)
body=$(json_field body)
[ -n "$title" ] || title="MindFS"
[ -n "$body" ] || body=$(json_field type)

if command -v notify-send >/dev/null 2>&1; then
    # Linux desktops
    notify-send "$title" "$body"
elif command -v osascript >/dev/null 2>&1; then
    # macOS
    osascript -e "display notification \"$body\" with title \"$title\""
else
    printf '%s: %s\n' "$title" "$body" >&2
fi
