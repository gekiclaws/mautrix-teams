package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

const defaultMessagesURL = "https://msgapi.teams.live.com/v1/users/ME/conversations"
const defaultSendMessagesURL = "https://teams.live.com/api/chatsvc/consumer/v1/users/ME/conversations"

var ErrMissingToken = errors.New("consumer client missing skypetoken")

type MessagesError struct {
	Status      int
	BodySnippet string
}

func (e MessagesError) Error() string {
	return "messages request failed"
}

type SendMessageError struct {
	Status      int
	BodySnippet string
}

func (e SendMessageError) Error() string {
	return "send message request failed"
}

type remoteMessage struct {
	ID                     string          `json:"id"`
	ClientMessageID        string          `json:"clientmessageid"`
	SequenceID             json.RawMessage `json:"sequenceId"`
	OriginalArrivalTime    string          `json:"originalarrivaltime"`
	From                   json.RawMessage `json:"from"`
	IMDisplayName          string          `json:"imdisplayname"`
	FromDisplayNameInToken string          `json:"fromDisplayNameInToken"`
	Content                json.RawMessage `json:"content"`
	Properties             json.RawMessage `json:"properties"`
}

func (c *Client) ListMessages(ctx context.Context, conversationID string, sinceSequence string) ([]model.RemoteMessage, error) {
	if c == nil || c.HTTP == nil {
		return nil, ErrMissingHTTPClient
	}
	if c.Token == "" {
		return nil, ErrMissingToken
	}
	if conversationID == "" {
		return nil, errors.New("missing conversation id")
	}

	var payload struct {
		Messages []remoteMessage `json:"messages"`
	}
	baseURL := c.MessagesURL
	if baseURL == "" {
		baseURL = defaultMessagesURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	messagesURL := fmt.Sprintf("%s/%s/messages", baseURL, url.PathEscape(conversationID))
	if err := c.fetchJSON(ctx, messagesURL, &payload); err != nil {
		return nil, err
	}

	result := make([]model.RemoteMessage, 0, len(payload.Messages))
	seen := make(map[string]struct{}, len(payload.Messages))
	for _, msg := range payload.Messages {
		msgID := strings.TrimSpace(msg.ID)
		if msgID != "" {
			if _, ok := seen[msgID]; ok {
				continue
			}
			seen[msgID] = struct{}{}
		}

		sequenceID, err := normalizeSequenceID(msg.SequenceID)
		if err != nil {
			return nil, err
		}
		senderID := model.NormalizeTeamsUserID(model.ExtractSenderID(msg.From))
		if senderID == "" && c.Log != nil {
			c.Log.Debug().
				Str("message_id", msg.ID).
				Msg("teams message missing sender id")
		}
		content := model.ExtractContent(msg.Content)
		result = append(result, model.RemoteMessage{
			MessageID:        msg.ID,
			ClientMessageID:  msg.ClientMessageID,
			SequenceID:       sequenceID,
			SenderID:         senderID,
			IMDisplayName:    msg.IMDisplayName,
			TokenDisplayName: msg.FromDisplayNameInToken,
			Timestamp:        model.ParseTimestamp(msg.OriginalArrivalTime),
			Body:             content.Body,
			FormattedBody:    content.FormattedBody,
			GIFs:             content.GIFs,
			PropertiesFiles:  model.ExtractFilesProperty(msg.Properties),
			Reactions:        model.ExtractReactions(msg.Properties),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return model.CompareSequenceID(result[i].SequenceID, result[j].SequenceID) < 0
	})

	return result, nil
}

var sendMessageCounter uint64

func GenerateClientMessageID() string {
	now := uint64(time.Now().UTC().UnixNano())
	for {
		prev := atomic.LoadUint64(&sendMessageCounter)
		if now <= prev {
			now = prev + 1
		}
		if atomic.CompareAndSwapUint64(&sendMessageCounter, prev, now) {
			return strconv.FormatUint(now, 10)
		}
	}
}

func (c *Client) SendMessage(ctx context.Context, threadID string, text string, fromUserID string) (string, error) {
	clientMessageID := GenerateClientMessageID()
	_, err := c.SendMessageWithID(ctx, threadID, text, fromUserID, clientMessageID)
	return clientMessageID, err
}

func (c *Client) SendGIF(ctx context.Context, threadID string, gifURL string, title string, fromUserID string) (string, error) {
	clientMessageID := GenerateClientMessageID()
	_, err := c.SendGIFWithID(ctx, threadID, gifURL, title, fromUserID, clientMessageID)
	return clientMessageID, err
}

func (c *Client) SendMessageWithID(ctx context.Context, threadID string, text string, fromUserID string, clientMessageID string) (int, error) {
	return c.sendHTMLMessageWithID(ctx, threadID, formatHTMLContent(text), fromUserID, clientMessageID)
}

func (c *Client) SendGIFWithID(ctx context.Context, threadID string, gifURL string, title string, fromUserID string, clientMessageID string) (int, error) {
	return c.sendHTMLMessageWithID(ctx, threadID, formatGIFContent(gifURL, title), fromUserID, clientMessageID)
}

func (c *Client) sendHTMLMessageWithID(ctx context.Context, threadID string, htmlContent string, fromUserID string, clientMessageID string) (int, error) {
	return c.sendRichTextMessageWithID(ctx, threadID, htmlContent, "", fromUserID, clientMessageID, false)
}

func (c *Client) SendAttachmentMessageWithID(ctx context.Context, threadID string, htmlContent string, filesProperty string, fromUserID string, clientMessageID string) (int, error) {
	if strings.TrimSpace(filesProperty) == "" {
		return 0, errors.New("missing files property")
	}
	return c.sendRichTextMessageWithID(ctx, threadID, htmlContent, filesProperty, fromUserID, clientMessageID, true)
}

func (c *Client) sendRichTextMessageWithID(ctx context.Context, threadID string, htmlContent string, filesProperty string, fromUserID string, clientMessageID string, allowEmptyContent bool) (int, error) {
	if c == nil || c.HTTP == nil {
		return 0, ErrMissingHTTPClient
	}
	if c.Token == "" {
		return 0, ErrMissingToken
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return 0, errors.New("missing thread id")
	}
	if !allowEmptyContent && strings.TrimSpace(htmlContent) == "" {
		return 0, errors.New("missing message content")
	}
	if strings.TrimSpace(fromUserID) == "" {
		return 0, errors.New("missing from user id")
	}
	if clientMessageID == "" {
		return 0, errors.New("missing client message id")
	}

	if !strings.Contains(threadID, "@thread.v2") && c.Log != nil {
		c.Log.Warn().
			Str("thread_id", threadID).
			Msg("teams thread id missing @thread.v2")
	}

	baseURL := c.SendMessagesURL
	if baseURL == "" {
		baseURL = defaultSendMessagesURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	messagesURL := fmt.Sprintf("%s/%s/messages", baseURL, url.PathEscape(threadID))

	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload := map[string]interface{}{
		"type":                "Message",
		"conversationid":      threadID,
		"content":             htmlContent,
		"messagetype":         "RichText/Html",
		"contenttype":         "Text",
		"clientmessageid":     clientMessageID,
		"composetime":         now,
		"originalarrivaltime": now,
		"from":                fromUserID,
		"fromUserId":          fromUserID,
	}
	if strings.TrimSpace(filesProperty) != "" {
		payload["properties"] = map[string]string{
			"files": filesProperty,
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, messagesURL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.Header.Set("authentication", "skypetoken="+c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	c.debugRequest("teams send message request", messagesURL, req)

	executor := c.Executor
	if executor == nil {
		executor = &TeamsRequestExecutor{
			HTTP:        c.HTTP,
			Log:         zerolog.Nop(),
			MaxRetries:  4,
			BaseBackoff: 500 * time.Millisecond,
			MaxBackoff:  10 * time.Second,
		}
		c.Executor = executor
	}
	if executor.HTTP == nil {
		executor.HTTP = c.HTTP
	}
	if c.Log != nil {
		executor.Log = *c.Log
	}

	ctx = WithRequestMeta(ctx, RequestMeta{
		ThreadID:        threadID,
		ClientMessageID: clientMessageID,
	})
	resp, err := executor.Do(ctx, req, classifyTeamsSendResponse)
	if err != nil {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}
		return statusCode, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func formatHTMLContent(text string) string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	escaped := html.EscapeString(normalized)
	escaped = strings.ReplaceAll(escaped, "\n", "<br>")
	return "<p>" + escaped + "</p>"
}

func formatGIFContent(gifURL string, title string) string {
	gifURL = strings.TrimSpace(gifURL)
	label := strings.TrimSpace(title)
	if label == "" {
		label = "GIF"
	}
	fullLabel := label + " (GIF Image)"
	return `<p>&nbsp;</p><readonly title="` + html.EscapeString(fullLabel) + `" itemtype="http://schema.skype.com/Giphy" contenteditable="false" aria-label="` + html.EscapeString(fullLabel) + `"><img style="height:auto;margin-top:4px;max-width:100%;" alt="` + html.EscapeString(fullLabel) + `" height="250" width="350" src="` + html.EscapeString(gifURL) + `" itemtype="http://schema.skype.com/Giphy"></readonly><p>&nbsp;</p>`
}

func classifyTeamsSendResponse(resp *http.Response) error {
	if resp == nil {
		return errors.New("missing response")
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return RetryableError{
			Status:     resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return RetryableError{Status: resp.StatusCode}
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	return SendMessageError{
		Status:      resp.StatusCode,
		BodySnippet: string(snippet),
	}
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if duration := time.Until(at); duration > 0 {
			return duration
		}
	}
	return 0
}

func normalizeSequenceID(value json.RawMessage) (string, error) {
	if len(value) == 0 {
		return "", nil
	}
	var asString string
	if err := json.Unmarshal(value, &asString); err == nil {
		return asString, nil
	}
	var asNumber json.Number
	if err := json.Unmarshal(value, &asNumber); err == nil {
		return asNumber.String(), nil
	}
	return "", errors.New("invalid sequenceId")
}

func (c *Client) fetchJSON(ctx context.Context, endpoint string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("authentication", "skypetoken="+c.Token)
	req.Header.Set("Accept", "application/json")
	c.debugRequest("teams messages request", endpoint, req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if resp.StatusCode == http.StatusTooManyRequests {
			return RetryableError{
				Status:     resp.StatusCode,
				RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			}
		}
		if resp.StatusCode >= http.StatusInternalServerError {
			return RetryableError{Status: resp.StatusCode}
		}
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return MessagesError{
			Status:      resp.StatusCode,
			BodySnippet: string(snippet),
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func (c *Client) debugRequest(message string, endpoint string, req *http.Request) {
	if c == nil || c.Log == nil {
		return
	}
	headers := map[string][]string{}
	for key, values := range req.Header {
		if strings.EqualFold(key, "authentication") || strings.EqualFold(key, "authorization") {
			headers[key] = []string{"REDACTED"}
		} else {
			headers[key] = values
		}
	}
	c.Log.Debug().
		Str("url", endpoint).
		Interface("headers", headers).
		Msg(message)
}
