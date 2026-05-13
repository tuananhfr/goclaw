package facebook

import (
	"fmt"
	"log/slog"
	"time"
)

// handleMessagingEvent processes a Messenger inbox event.
func (ch *Channel) handleMessagingEvent(entry WebhookEntry, event MessagingEvent) {
	// Feature gate.
	if !ch.config.Features.MessengerAutoReply {
		return
	}

	// Page routing guard (before dedup write).
	if entry.ID != ch.pageID {
		return
	}

	// Track admin (page) replies: when the page itself sends a message,
	// record the recipient's chat ID so the bot skips auto-reply for that
	// conversation during the cooldown window.
	if event.Sender.ID == ch.pageID {
		if event.Recipient.ID != "" {
			eventAt := messagingEventTime(event.Timestamp)
			if ch.isBotEcho(event.Recipient.ID, eventAt) {
				slog.Debug("facebook: bot echo ignored", "chat_id", event.Recipient.ID)
				return
			}
			ch.adminReplied.Store(event.Recipient.ID, eventAt)
			slog.Debug("facebook: admin reply tracked", "chat_id", event.Recipient.ID)
		}
		return
	}

	// Skip delivery/read receipts and other non-content events.
	if event.Message == nil && event.Postback == nil {
		return
	}

	// Dedup by message MID or postback signature (include payload to reduce collision risk).
	var eventKey string
	switch {
	case event.Message != nil:
		eventKey = "msg:" + event.Message.MID
	case event.Postback != nil:
		eventKey = fmt.Sprintf("postback:%s:%d:%s", event.Sender.ID, event.Timestamp, event.Postback.Payload)
	}
	if ch.isDup(eventKey) {
		slog.Debug("facebook: duplicate messaging event skipped", "key", eventKey)
		return
	}

	// Check if admin already replied to this conversation recently.
	senderID := event.Sender.ID
	if ch.adminRepliedRecently(senderID, time.Now()) {
		slog.Info("facebook: skipping auto-reply (admin replied recently)", "chat_id", senderID)
		return
	}

	// Extract text content and media attachment URLs.
	var content string
	var media []string

	var mediaTags string
	if event.Message != nil {
		if event.Message.Text != "" {
			content = event.Message.Text
		}
		for _, att := range event.Message.Attachments {
			if (att.Type == "image" || att.Type == "video" || att.Type == "audio" || att.Type == "file") && att.Payload.URL != "" {
				media = append(media, att.Payload.URL)
				tag := ""
				switch att.Type {
				case "image":
					tag = fmt.Sprintf("<media:image url=%q>", att.Payload.URL)
				case "video":
					tag = "<media:video>"
				case "audio":
					tag = "<media:audio>"
				case "file":
					tag = "<media:document>"
				}
				if tag != "" {
					if mediaTags != "" {
						mediaTags += "\n"
					}
					mediaTags += tag
				}
			}
		}
	} else if event.Postback != nil {
		content = event.Postback.Title
	}

	if content == "" && len(media) == 0 {
		return // Ignore empty messages with no supported media
	}

	// Append media tags so the agent has inline placeholders for the files.
	// This matches Discord's behavior and prevents empty user turns.
	if mediaTags != "" {
		if content != "" {
			content += "\n"
		}
		content += mediaTags
	}

	// Messenger sessions are 1:1: chatID = senderID (channel name scopes the session).
	chatID := senderID

	metadata := map[string]string{
		"fb_mode":     "messenger",
		"message_id":  eventKey,
		"page_id":     ch.pageID,
		"sender_id":   senderID,
		"debounce_ms": "3000", // Facebook splits text+media into separate webhooks; 3s window to merge them
	}
	if ch.config.MessengerOptions.SessionTimeout != "" {
		metadata["session_timeout"] = ch.config.MessengerOptions.SessionTimeout
	}

	// Pass media URLs directly — persistMedia/copyMediaFile handles HTTP downloads.
	// This avoids blocking the webhook handler and allows the debouncer to merge
	// text + media messages from the same sender.
	ch.HandleMessage(senderID, chatID, content, media, metadata, "direct")
}

func messagingEventTime(ts int64) time.Time {
	switch {
	case ts > 1_000_000_000_000:
		return time.UnixMilli(ts)
	case ts > 0:
		return time.Unix(ts, 0)
	default:
		return time.Now()
	}
}
