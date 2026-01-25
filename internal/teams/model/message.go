package model

import (
	"encoding/json"
	"html"
	"strconv"
	"strings"
	"time"
)

type RemoteMessage struct {
	MessageID  string
	SequenceID string
	SenderID   string
	Timestamp  time.Time
	Body       string
}

func ExtractBody(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var plain string
	if err := json.Unmarshal(content, &plain); err == nil {
		return html.UnescapeString(plain)
	}
	var obj struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &obj); err == nil {
		return html.UnescapeString(obj.Text)
	}
	return ""
}

func ExtractSenderID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		if idx := strings.LastIndex(plain, "/"); idx >= 0 && idx+1 < len(plain) {
			return plain[idx+1:]
		}
		return plain
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.ID
	}
	return ""
}

func ParseTimestamp(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return ts
	}
	ts, err = time.Parse(time.RFC3339, value)
	if err == nil {
		return ts
	}
	return time.Time{}
}

func CompareSequenceID(a, b string) int {
	aNum, aErr := strconv.ParseUint(a, 10, 64)
	bNum, bErr := strconv.ParseUint(b, 10, 64)
	if aErr == nil && bErr == nil {
		switch {
		case aNum < bNum:
			return -1
		case aNum > bNum:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(a, b)
}
