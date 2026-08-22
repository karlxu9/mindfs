# MindFS notify-script example: forward events to a Windows toast
# notification. Usage: mindfs -notify-script C:\path\to\notify-example.ps1
# Payload JSON arrives on stdin; see docs/notify-script.md for the contract.

$raw = [Console]::In.ReadToEnd()
if ([string]::IsNullOrWhiteSpace($raw)) { exit 0 }
try {
    $payload = $raw | ConvertFrom-Json
} catch {
    exit 1
}

$title = if ($payload.title) { [string]$payload.title } else { "MindFS" }
$body = if ($payload.body) { [string]$payload.body } else { [string]$payload.type }

# Windows Runtime toast API - no third-party modules required.
[void][Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
[void][Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom, ContentType = WindowsRuntime]

$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent(
    [Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$texts = $xml.GetElementsByTagName("text")
[void]$texts.Item(0).AppendChild($xml.CreateTextNode($title))
[void]$texts.Item(1).AppendChild($xml.CreateTextNode($body))

$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
if ($payload.tag) { $toast.Tag = ([string]$payload.tag) -replace "[^a-zA-Z0-9_.-]", "_" }
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("MindFS").Show($toast)
