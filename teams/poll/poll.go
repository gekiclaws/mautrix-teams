package poll

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-teams/teams"
)

const (
	defaultChatsTop    = 50
	defaultMessagesTop = 50
	bodyPreviewLimit   = 120
)

type PolledMessage struct {
	ChatID     string
	MessageID  string
	SenderID   string
	SenderName string
	BodyText   string
	CreatedAt  time.Time
}

type Poller struct {
	GraphClient *teams.GraphClient
	UserID      string
	Cursor      map[string]string
	ChatsTop    int
	MessagesTop int
}

func (p *Poller) RunOnce(ctx context.Context, log zerolog.Logger) error {
	if p.GraphClient == nil {
		return errors.New("poller requires GraphClient")
	}
	if p.UserID == "" {
		return errors.New("poller requires UserID")
	}
	if p.Cursor == nil {
		p.Cursor = make(map[string]string)
	}

	chatsTop := p.ChatsTop
	if chatsTop <= 0 {
		chatsTop = defaultChatsTop
	}
	messagesTop := p.MessagesTop
	if messagesTop <= 0 {
		messagesTop = defaultMessagesTop
	}

	chats, resp, err := p.GraphClient.ListChats(ctx, p.UserID, chatsTop)
	if err != nil {
		return err
	}
	logRateLimitHeaders(log, resp)

	for _, chat := range chats {
		messages, msgResp, err := p.GraphClient.ListChatMessages(ctx, chat.ID, messagesTop)
		if err != nil {
			return err
		}
		logRateLimitHeaders(log, msgResp)

		lastID := p.Cursor[chat.ID]
		polled, cursorFound := collectPolledMessages(chat.ID, messages, lastID)
		if lastID != "" && !cursorFound {
			continue
		}

		for _, message := range polled {
			log.Info().
				Str("chat_id", message.ChatID).
				Str("message_id", message.MessageID).
				Str("sender", senderLabel(message)).
				Time("created_at", message.CreatedAt).
				Str("body", truncateString(message.BodyText, bodyPreviewLimit)).
				Msg("polled teams message")
			p.Cursor[chat.ID] = message.MessageID
		}
	}

	return nil
}

func collectPolledMessages(chatID string, messages []teams.GraphMessage, lastMessageID string) ([]PolledMessage, bool) {
	polled := make([]PolledMessage, 0, len(messages))
	if lastMessageID == "" {
		for _, message := range messages {
			polled = append(polled, normalizeMessage(chatID, message))
		}
		return polled, true
	}

	found := false
	for _, message := range messages {
		if !found {
			if message.ID == lastMessageID {
				found = true
			}
			continue
		}
		polled = append(polled, normalizeMessage(chatID, message))
	}
	return polled, found
}

func normalizeMessage(chatID string, message teams.GraphMessage) PolledMessage {
	senderID := ""
	senderName := ""
	if message.From != nil && message.From.User != nil {
		senderID = message.From.User.ID
		senderName = message.From.User.DisplayName
	}

	body := strings.TrimSpace(message.Body.Content)
	if !strings.EqualFold(message.Body.ContentType, "text") {
		body = stripTags(body)
	}

	return PolledMessage{
		ChatID:     chatID,
		MessageID:  message.ID,
		SenderID:   senderID,
		SenderName: senderName,
		BodyText:   strings.TrimSpace(body),
		CreatedAt:  message.CreatedDateTime,
	}
}

func senderLabel(message PolledMessage) string {
	if message.SenderName != "" {
		return message.SenderName
	}
	if message.SenderID != "" {
		return message.SenderID
	}
	return "unknown"
}

func stripTags(input string) string {
	var builder strings.Builder
	builder.Grow(len(input))
	inTag := false
	for _, ch := range input {
		switch ch {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				builder.WriteRune(ch)
			}
		}
	}
	return builder.String()
}

func truncateString(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func logRateLimitHeaders(log zerolog.Logger, resp teams.GraphResponse) {
	headerKeys := []string{
		"Retry-After",
		"RateLimit-Limit",
		"RateLimit-Remaining",
		"RateLimit-Reset",
		"x-ms-ratelimit-remaining-tenant-reads",
		"x-ms-ratelimit-remaining-tenant-writes",
		"x-ms-ratelimit-remaining-tenant-requests",
		"x-ms-ratelimit-remaining-resource",
	}

	event := log.Info().Str("endpoint", resp.Endpoint)
	hasHeader := false
	for _, key := range headerKeys {
		value := resp.Header.Get(key)
		if value == "" {
			continue
		}
		hasHeader = true
		event.Str(strings.ToLower(key), value)
	}
	if hasHeader {
		event.Msg("graph rate limit headers")
	}
}
