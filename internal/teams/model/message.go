package model

import (
	"encoding/json"
	"html"
	"strconv"
	"strings"
	"time"
)

type RemoteMessage struct {
	MessageID        string
	ClientMessageID  string
	SequenceID       string
	SenderID         string
	SenderName       string
	IMDisplayName    string
	TokenDisplayName string
	Timestamp        time.Time
	Body             string
	Reactions        []MessageReaction
}

type MessageReaction struct {
	EmotionKey string
	Users      []MessageReactionUser
}

type MessageReactionUser struct {
	MRI    string
	TimeMS int64
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

func ExtractSenderName(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		return ""
	}
	var obj struct {
		DisplayName string `json:"displayName"`
		Name        string `json:"name"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		if obj.DisplayName != "" {
			return obj.DisplayName
		}
		return obj.Name
	}
	return ""
}

func ExtractReactions(properties json.RawMessage) []MessageReaction {
	if len(properties) == 0 {
		return nil
	}
	var payload struct {
		Emotions []struct {
			Key   string `json:"key"`
			Users []struct {
				MRI  string          `json:"mri"`
				Time json.RawMessage `json:"time"`
			} `json:"users"`
		} `json:"emotions"`
	}
	if err := json.Unmarshal(properties, &payload); err != nil {
		return nil
	}
	if len(payload.Emotions) == 0 {
		return nil
	}

	reactions := make([]MessageReaction, 0, len(payload.Emotions))
	for _, emotion := range payload.Emotions {
		key := strings.TrimSpace(emotion.Key)
		if key == "" || len(emotion.Users) == 0 {
			continue
		}
		users := make([]MessageReactionUser, 0, len(emotion.Users))
		for _, user := range emotion.Users {
			mri := strings.TrimSpace(user.MRI)
			if mri == "" {
				continue
			}
			users = append(users, MessageReactionUser{
				MRI:    mri,
				TimeMS: parseReactionTime(user.Time),
			})
		}
		if len(users) == 0 {
			continue
		}
		reactions = append(reactions, MessageReaction{
			EmotionKey: key,
			Users:      users,
		})
	}
	if len(reactions) == 0 {
		return nil
	}
	return reactions
}

func parseReactionTime(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		str = strings.TrimSpace(str)
		if str == "" {
			return 0
		}
		if val, err := strconv.ParseInt(str, 10, 64); err == nil {
			return val
		}
		if val, err := strconv.ParseFloat(str, 64); err == nil {
			return int64(val)
		}
		return 0
	}
	var num float64
	if err := json.Unmarshal(raw, &num); err == nil {
		return int64(num)
	}
	return 0
}

func NormalizeTeamsUserID(value string) string {
	return strings.TrimSpace(value)
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

func ChooseLastSeenTS(messageTS time.Time, now time.Time) time.Time {
	if !messageTS.IsZero() {
		return messageTS.UTC()
	}
	return now.UTC()
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
