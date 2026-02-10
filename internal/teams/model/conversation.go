package model

import "strings"

type ThreadProperties struct {
	OriginalThreadID  string `json:"originalThreadId"`
	ProductThreadType string `json:"productThreadType"`
	CreatedAt         string `json:"createdat"`
	IsCreator         bool   `json:"isCreator"`
	Topic             string `json:"topic"`
	ThreadTopic       string `json:"threadTopic"`
	Title             string `json:"title"`
	DisplayName       string `json:"displayName"`
	Name              string `json:"name"`
}

type ConversationMember struct {
	ID            string `json:"id"`
	MRI           string `json:"mri"`
	DisplayName   string `json:"displayName"`
	Name          string `json:"name"`
	IsSelf        bool   `json:"isSelf"`
	IsCurrentUser bool   `json:"isCurrentUser"`
}

type ConversationProperties struct {
	Topic       string `json:"topic"`
	ThreadTopic string `json:"threadTopic"`
	Title       string `json:"title"`
	DisplayName string `json:"displayName"`
	Name        string `json:"name"`
}

type RemoteConversation struct {
	ID               string                 `json:"id"`
	ThreadProperties ThreadProperties       `json:"threadProperties"`
	Topic            string                 `json:"topic"`
	Title            string                 `json:"title"`
	DisplayName      string                 `json:"displayName"`
	Name             string                 `json:"name"`
	Properties       ConversationProperties `json:"properties"`
	Members          []ConversationMember   `json:"members"`
	Participants     []ConversationMember   `json:"participants"`
	Consumers        []ConversationMember   `json:"consumers"`
}

type Thread struct {
	ID             string
	ConversationID string
	Type           string
	CreatedAtRaw   string
	IsCreator      bool
	IsOneToOne     bool
	RoomName       string
}

func (c RemoteConversation) Normalize() (Thread, bool) {
	return c.NormalizeForSelf("")
}

func (c RemoteConversation) NormalizeForSelf(selfUserID string) (Thread, bool) {
	id := strings.TrimSpace(c.ThreadProperties.OriginalThreadID)
	if id == "" {
		return Thread{}, false
	}
	threadType := strings.TrimSpace(c.ThreadProperties.ProductThreadType)
	conversationID := strings.TrimSpace(c.ID)
	isOneToOne := threadType == "OneToOneChat" || c.isLikelyOneToOne(strings.TrimSpace(selfUserID))
	return Thread{
		ID:             id,
		ConversationID: conversationID,
		Type:           threadType,
		CreatedAtRaw:   c.ThreadProperties.CreatedAt,
		IsCreator:      c.ThreadProperties.IsCreator,
		IsOneToOne:     isOneToOne,
		RoomName:       c.resolveRoomName(isOneToOne, strings.TrimSpace(selfUserID)),
	}, true
}

func (c RemoteConversation) resolveRoomName(isOneToOne bool, selfUserID string) string {
	if isOneToOne {
		if dmName := c.resolveDMName(selfUserID); dmName != "" {
			return dmName
		}
		return "Chat"
	}
	if name := c.resolveThreadName(); name != "" {
		return name
	}
	return "Chat"
}

func (c RemoteConversation) resolveDMName(selfUserID string) string {
	for _, list := range [][]ConversationMember{c.Members, c.Participants, c.Consumers} {
		for _, member := range list {
			id := strings.TrimSpace(member.ID)
			if id == "" {
				id = strings.TrimSpace(member.MRI)
			}
			if member.IsSelf || member.IsCurrentUser {
				continue
			}
			if selfUserID != "" && id != "" && strings.EqualFold(id, selfUserID) {
				continue
			}
			if isLikelyTeamsBotID(id) {
				continue
			}
			if name := strings.TrimSpace(member.DisplayName); name != "" {
				return name
			}
			if name := strings.TrimSpace(member.Name); name != "" {
				return name
			}
		}
	}
	return ""
}

func (c RemoteConversation) resolveThreadName() string {
	for _, candidate := range []string{
		c.ThreadProperties.Topic,
		c.ThreadProperties.ThreadTopic,
		c.ThreadProperties.Title,
		c.ThreadProperties.DisplayName,
		c.ThreadProperties.Name,
		c.Properties.Topic,
		c.Properties.ThreadTopic,
		c.Properties.Title,
		c.Properties.DisplayName,
		c.Properties.Name,
		c.Topic,
		c.Title,
		c.DisplayName,
		c.Name,
	} {
		if name := strings.TrimSpace(candidate); name != "" {
			return name
		}
	}
	return ""
}

func (c RemoteConversation) isLikelyOneToOne(selfUserID string) bool {
	if c.resolveThreadName() != "" {
		return false
	}
	others := make(map[string]struct{})
	for _, list := range [][]ConversationMember{c.Members, c.Participants, c.Consumers} {
		for _, member := range list {
			rawID := strings.TrimSpace(member.ID)
			if rawID == "" {
				rawID = strings.TrimSpace(member.MRI)
			}
			if rawID == "" {
				continue
			}
			idKey := strings.ToLower(rawID)
			if member.IsSelf || member.IsCurrentUser {
				continue
			}
			if selfUserID != "" && strings.EqualFold(rawID, selfUserID) {
				continue
			}
			if isLikelyTeamsBotID(rawID) {
				continue
			}
			others[idKey] = struct{}{}
			if len(others) > 1 {
				return false
			}
		}
	}
	return len(others) == 1
}

func isLikelyTeamsBotID(raw string) bool {
	id := strings.TrimSpace(strings.ToLower(raw))
	if id == "" {
		return false
	}
	return strings.HasPrefix(id, "28:") || strings.Contains(id, "teamsbot")
}
